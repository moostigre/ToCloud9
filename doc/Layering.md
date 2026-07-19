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
the group (or the character when solo) to an eligible gameserver. The gateway
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

### Portal catalog

At registry startup, `areatrigger_teleport` is imported from the AzerothCore
world database into a Redis hash. Configure the database and catalog version:

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
request placement. Active bindings refresh a 24-hour Redis expiry so abandoned
group/map entries are eventually removed.

Portal placement uses a related Redis binding:

```text
(realm ID, group ID, destination map ID) -> gameserver ID
```

For a solo player, character GUID replaces group ID. This keeps repeated solo
entries on the same instance-owning core even if load changes. The binding is a
core affinity, not an instance-ID allocation: the selected AzerothCore creates
and manages its native instance ID when it processes the replayed trigger.

Population is intentionally approximate and uses the gameserver's existing
active-connection metric. The minimal implementation does not track individual
players in the registry.

## Redis availability and registry scaling

Registry processes are stateless and can be horizontally replicated. A single
Redis pod is suitable for development but is a production single point of
failure. Configure either Redis/Valkey Sentinel (one writable primary with
replicas and quorum failover) or Redis Cluster (sharded primaries with
replicas). Do not point registry replicas at independent, uncoordinated Redis
servers because they can produce conflicting placements.

`REDIS_URL` retains standalone compatibility. For HA deployments use:

```text
REDIS_ADDRESSES=redis-0:6379,redis-1:6379,redis-2:6379
REDIS_MASTER_NAME=mymaster  # set for Sentinel; omit for Redis Cluster
REDIS_USERNAME=...
REDIS_PASSWORD=...
REDIS_DB=0
```

Placement keys use Redis hash tags so each atomic compare-and-set remains in a
single Redis Cluster slot. During Redis failover placement is temporarily
unavailable and fails closed; existing players and instances continue running.

## Test commands

- `.layer` displays the current map configuration and approximate populations.
- `.layer switch <number>` redirects the current character to a selected layer.

These commands use the normal redirect path. There is no visibility cache,
seamless-transition state machine, movement preservation, or special layer
recovery logic.
