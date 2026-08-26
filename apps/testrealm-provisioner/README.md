# Test realm provisioner

This service collects deterministic fixture requirements for an open,
non-draft AzerothCore pull request and requests a temporary ToCloud9 realm.
There is no LLM, chat endpoint, API key, prompt processing, or generated
infrastructure input.

The browser form accepts an optional testing character, up to 40 item IDs with
counts, and quest IDs marked either active or completed. The backend applies
the same strict bounds independently. A realm-only request is also supported.

## Trust boundary

- The PR and its current head SHA are revalidated with GitHub at admission.
- A server-owned catalogue must map that exact SHA to an immutable image digest
  under `ALLOWED_IMAGE_PREFIX`.
- The public service sends only a fixed signed JSON command to a separately
  deployed HTTPS realm operator.
- The operator must independently validate the command and owns all database,
  Kubernetes, readiness, account, fixture, and realmlist mutations.
- Missing operator configuration fails closed. There is no simulation mode.
- At most five provisioning/running realms are admitted. Up to 25 distinct PR
  image builds may continue independently; completed builds wait until online
  realm capacity and a retained realm slot are available.

The operator must not report success until the realm is visible in the auth
database and its AzerothCore worker is ready. Its provision response is:

```json
{"realm_name":"PR 123 test","address":"163.172.51.144","port":32760,"realmlist_id":2}
```

Provision commands request the fixed test credentials `admin` / `admin`.

## Lifecycle

- Poll the realm gateway's live connection count through the operator, refresh
  activity while any player is connected, and suspend after 30 minutes with no
  connected players. Monitoring failures keep the realm running.
- Delete data and the unreferenced PR image 48 hours after going offline.
- A one-time management token authorizes inspect, reactivate, and delete. Only
  its SHA-256 digest is persisted.

## Required configuration

```text
ACTIVITY_HMAC_SECRET
IMAGE_CATALOG_FILE=/data/config/images.json
REALM_OPERATOR_URL=https://operator-origin
REALM_OPERATOR_SECRET
```

State and temporary files live under `/data`. The container root filesystem is
read-only. The service reports unavailable until the operator is configured.

## Trusted image catalogue

Only trusted CI may write the catalogue. Never use `pull_request_target` to
execute untrusted PR code with deployment or registry credentials.

```json
{"123":{"SHA":"full-head-sha","Image":"allowed/repo@sha256:64-hex"}}
```

## Verification

```bash
go test ./apps/testrealm-provisioner
go test -race ./apps/testrealm-provisioner
```
