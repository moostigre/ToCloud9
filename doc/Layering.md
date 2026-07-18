# Layering

Layer placement is owned by the servers registry. The gateway supplies the
player's realm, map and group ID and follows the selected gameserver using the
existing worldserver redirect flow.

The registry stores both per-map configuration and group bindings in Redis.
Consequently, registry replicas share the same state and any replica can serve
a placement request. There is no layer coordinator service and no in-memory
player assignment cache.

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

Population is intentionally approximate and uses the gameserver's existing
active-connection metric. The minimal implementation does not track individual
players in the registry.

## Test commands

- `.layer` displays the current map configuration and approximate populations.
- `.layer switch <number>` redirects the current character to a selected layer.

These commands use the normal redirect path. There is no visibility cache,
seamless-transition state machine, movement preservation, or special layer
recovery logic.
