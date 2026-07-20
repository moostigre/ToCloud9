# Layering

Layer placement is owned by the servers registry. The gateway supplies the
player's realm, map and group ID and follows the selected gameserver using the
existing worldserver redirect flow.

The registry stores both per-map configuration and group bindings in Redis.
Consequently, registry replicas share the same state and any replica can serve
a placement request. There is no layer coordinator service and no in-memory
player assignment cache.

The registry also owns portal placement. The gateway sends the area-trigger ID,
realm, character and group to `SelectGameServerForAreaTrigger`. The registry
resolves the destination map from a versioned Redis catalog and atomically binds
the group and character to an eligible gameserver. The gateway
redirects first and replays the original area-trigger packet only after the
destination gameserver has accepted the player. Therefore only that gameserver
executes the portal and creates or reuses the native AzerothCore instance.

This pre-routing order prevents two cores from independently creating temporary
instances and avoids coupling the gateway to the AzerothCore world schema. No
AzerothCore patch or layer-specific game-core logic is required. The registry
chooses the owning core; AzerothCore continues to own instance IDs, maps,
lockouts, resets and saves.

## Configuration

Configure fixed layer counts at registry startup:

```yaml
servers_registry:
  layering:
    maps:
      - "1:2"
      - "531:3"
```

The equivalent environment variable is `LAYER_MAPS=1:2,531:3`. Startup values
are written to Redis. The `UpdateMapLayerConfiguration` gRPC method can replace
them at runtime and triggers map redistribution.

Every gameserver that hosts a layer registers a stable, non-zero `LAYER_ID`.
For a configured map, the registry assigns one compatible gameserver for every
layer ID from 1 through the configured count. Layer IDs must be unique among
gameservers capable of hosting the same map.

Dungeon and raid maps must not be listed in `LAYER_MAPS`. They use the separate
instance pool described below.

### Instance server pool

The registry imports the authoritative instance-map list from
`instance_template`. `INSTANCE_POOL_REPLICAS` controls how many compatible
gameserver processes are assigned each dungeon and raid map:

```yaml
servers_registry:
  instancePool:
    replicas: 2
```

This creates capacity, not instance layers. A core may host many different
native AzerothCore instances, but each individual instance route has exactly
one owning core. Outdoor `LAYER_ID` is ignored for instance selection and a
forced `.layer switch` is rejected while inside an instance.

To dedicate gameserver pods to instances, configure their
`Cluster.AvailableMaps` with the imported dungeon/raid map IDs. The registry
prefers these explicitly scoped cores over all-map cores when filling the
instance pool.

### Portal catalog

At registry startup, `areatrigger_teleport` and `instance_template` are imported
from the AzerothCore world database into a versioned Redis catalog. Configure
the database and catalog version:

```yaml
servers-registry:
  worldDB: "user:password@tcp(mysql:3306)/acore_world"
  areaTriggerCatalogVersion: "acore-world-2026-07"
  areaTriggerCatalogImportEnabled: true
```

Equivalent environment variables are `WORLD_DB_CONNECTION`,
`AREA_TRIGGER_CATALOG_VERSION`, and
`AREA_TRIGGER_CATALOG_IMPORT_ENABLED`. Change the catalog version when the
deployed world data changes. Import is idempotent and publishes through an
atomic Redis rename, so multiple registry replicas can start concurrently
without exposing a partially populated catalog.

When an external deployment job owns the import, set
`AREA_TRIGGER_CATALOG_IMPORT_ENABLED=false` after populating the versioned
catalog. An unknown trigger is forwarded normally because most area triggers
are not teleports. A Redis or placement failure is handled differently: the
gateway does not forward the portal packet, preventing split placement and
allowing the player to retry safely.

## Placement

For an ungrouped player, the registry returns the compatible layer gameserver
with the fewest active connections. For a grouped player it atomically creates
or reads this Redis binding:

```text
(realm ID, group ID, map ID) -> gameserver ID
```

When the bound gameserver is no longer available for the map, an atomic
compare-and-set moves the binding to the least-loaded available gameserver.
Group creation explicitly binds the leader's current gameserver before members
request placement. Active outdoor-layer bindings refresh a 24-hour Redis expiry
so abandoned group/map entries are eventually removed.

Instance placement uses related Redis bindings:

```text
(realm ID, group ID, instance map ID) -> gameserver ID
(realm ID, character GUID, instance map ID) -> gameserver ID
```

The group key is canonical while grouped. Every grouped entry also refreshes
the character key to the same core, so leaving or disbanding the group does not
move an existing instance to another process. Group creation binds the leader's
current instance core before members are routed. Repeated entries therefore
return to the process holding the instance's in-memory creature state even if
load changes.

Instance affinities do not use a fixed expiry: expiring a raid affinity while
its native lock is still valid can route the next entry to the wrong core. A
successful native reset explicitly removes them. If an owner disappears,
normal selection atomically replaces the stale binding.

These bindings are core affinity rather than instance-ID allocation: the
selected AzerothCore creates, reuses and persists its native instance ID when it
processes the replayed trigger. If the owner core disappears, the registry may
replace the stale affinity; database-persisted instance state survives, while
ordinary unsaved in-memory creature state cannot survive a process failure.

### Native reset handoff

The gateway intercepts the client's native reset request only long enough to
deliver it to every gameserver that owns one of the party's canonical group
instance affinities. It snapshots party membership, verifies that the requester
is the leader, groups the map affinities by owning core, and visits those cores
in a deterministic sequence. Each owner receives
AzerothCore's unmodified reset opcode through the player's authenticated world
session. The gateway waits for AzerothCore's success or failure packet before
continuing and returns the player to the original outdoor gameserver at the end.

Only a successful native reset invokes `FinalizeInstanceReset`. That idempotent
registry operation deletes the map's group affinity and every snapshotted party
member's character affinity. The next portal entry performs a fresh placement
and AzerothCore creates a fresh native instance. Failed resets retain all
affinities. A non-responsive owner aborts the handoff after a bounded timeout
and returns the player outdoors, so a dead gameserver cannot strand the client.
The player can safely repeat the idempotent operation after recovery.

This is a routing handoff, not a second instance lifecycle system. The registry
never allocates an instance ID and the gateway never edits an AzerothCore lock
or save. Instance-pool cores must accept the temporary authenticated handoff on
the player's outdoor map. Operationally dedicated instance cores should use
`LAYER_ID=0` and advertise the required outdoor control maps as well as their
instance maps; configured outdoor layers remain assigned to non-zero layer
cores.

Population is intentionally approximate and uses the gameserver's existing
active-connection metric. The minimal implementation does not track individual
players in the registry.

## Redis availability and registry scaling

Registry processes are stateless and can be horizontally replicated. The Helm
chart runs three registry replicas by default, spreads them across nodes when
possible, and protects them during voluntary disruptions. The chart-managed
standalone Redis remains a development default and is a production single point
of failure. Production deployments must configure either Redis/Valkey Sentinel
(one writable primary with replicas and quorum failover) or Redis Cluster
(sharded primaries with replicas). Do not point registry replicas at independent,
uncoordinated Redis servers because they can produce conflicting placements.

`REDIS_URL` retains standalone compatibility. For HA deployments use:

```text
REDIS_ADDRESSES=redis-0:6379,redis-1:6379,redis-2:6379
REDIS_MASTER_NAME=mymaster  # set for Sentinel; omit for Redis Cluster
REDIS_USERNAME=...
REDIS_PASSWORD=...
REDIS_DB=0
```

Placement keys use Redis hash tags so each atomic compare-and-set remains in a
single Redis Cluster slot. Registry scans visit every cluster primary and bulk
reads use slot-aware pipelines rather than cross-slot `MGET`. Party reset
cleanup is idempotent: if failover interrupts a multi-key cleanup, retrying
safely converges every member key. During Redis failover new placement and reset
routing are temporarily unavailable and fail closed; existing players and
instances continue running.

The reset sequence is transient per connected game session and creates no
cluster-wide coordinator. If that gateway dies, its client reconnects through
the normal recovery path and can safely repeat the reset against shared Redis
state. Multiple gateways and registry replicas can operate concurrently without
session affinity between services.

## Test commands

- `.layer` displays every configured outdoor map and its layer availability,
  followed by unique gameserver connection totals and every instance-pool core
  with its supported-map count and group/raid placement count. The connection metric is core-wide and is never
  presented as a per-map population. Gameserver IDs and addresses are included
  only when gateway server-detail messages are enabled.
- `.layer switch <number>` redirects the current character to a selected layer.

These commands use the normal redirect path. There is no visibility cache,
seamless-transition state machine, movement preservation, or special layer
recovery logic.
