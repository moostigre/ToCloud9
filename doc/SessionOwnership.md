# Cluster-wide session ownership

Gateways enforce one active session per account globally and one active session
per character within its realm. The implementation is gateway-owned because
only the gateway has both the client socket and a cluster-wide view of
authentication.

## State and takeover

Redis stores token-fenced owner records. An owner contains the gateway ID and a
cryptographically random session token. A normal logout deletes an owner only
when its token still matches, so cleanup from an older connection cannot delete
a newer connection's ownership.

A takeover acquires a leased per-owner claim, atomically verifies the current
owner and appends an eviction event to the previous gateway's Redis Stream. The
previous owner remains authoritative until its teardown is acknowledged; only
then does a compare-and-set commit the new owner. A failed or timed-out claimant
therefore cannot hide the predecessor from a later login. All keys involved in
these transactions share a global Redis Cluster hash tag; character keys also
contain their realm ID.

Account ownership is established after cryptographic authentication but before
the gateway sends `SMSG_AUTH_RESPONSE`. The previous client's game and world
session teardown must complete before its eviction is acknowledged. Failed
acknowledgement writes are retried while the gateway remains healthy.

NATS sends the same eviction as a low-latency fast path. Redis Streams are the
durable path: an eviction is still consumed if NATS delivery is interrupted.
Duplicate deliveries are deduplicated by eviction ID.

## Failure behaviour

- A gateway crash closes its client sockets. Stale owner records are harmless:
  the next claim replaces them using a new fencing token.
- Each gateway writes one expiring liveness heartbeat. A claimant waits for an
  eviction acknowledgement while the previous gateway is considered live. A
  gateway that cannot refresh its heartbeat disconnects all local sessions
  before the heartbeat expires, preventing split-brain sessions during a
  Redis/NATS partition.
- A temporary Redis outage does not disconnect existing players because there
  are no per-session renewals, unless it approaches the configured liveness
  TTL. New claims fail closed until ownership can be established again.
- A temporary NATS outage falls back to the durable Redis eviction stream.
- Redis carries an ownership-generation key. If Redis loses its state, existing
  gateways detect the generation change and terminate; a recovery fence delays
  new claims for one liveness TTL so old and new sessions cannot overlap.

## Load model

Idle players generate no Redis traffic. Redis receives one generation check and
one heartbeat write per gateway every one-third of
`gatewayLivenessTTLSeconds`, plus operations for login, character selection,
takeover and logout. Load therefore follows gateway count and session
transitions rather than the number of connected players.

Eviction work is bounded to 32 concurrent workers per gateway, and each Redis
Stream is approximately trimmed to 4096 events.

## Configuration

```yaml
gateway:
  redisUrl: redis://redis:6379/0
  redisCluster: false
  gatewayLivenessTTLSeconds: 15
```

The liveness TTL must be at least 10 seconds. Redis and NATS high availability
are deployment concerns and can be provided independently of gateway scaling.
Set `redisCluster` to `true` when `redisUrl` points to a Redis Cluster bootstrap
node. Additional bootstrap addresses can be supplied with repeated `addr` query
parameters supported by go-redis's cluster URL parser. For Helm deployments,
set `gateway.redisUrl`, or store the full URL in `gateway.redisExistingSecret`
under `gateway.redisExistingSecretKey`; this allows `redis.enabled=false` with
an external standalone or clustered Redis deployment.
