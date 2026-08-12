# ADR-0012: Retain recoverable Client Keys in the private local configuration

## Status

Accepted

## Decision date

2026-08-10

## Recorded date

2026-08-10

## Context

vibe-proxy originally retained only a bcrypt hash after generating a data-plane
Client Key. That is a suitable default for a hosted credential service, but it
makes a local gateway unnecessarily difficult to operate: the owner cannot copy
an existing key to another Agent or machine and must instead delete or rotate a
working credential.

The default product is local-first. Its control plane is loopback-only, desktop
access is protected by a management session, CLI access uses the Admin bearer
token, and the local configuration is written with owner-only permissions. A
routine list response should still avoid spraying credentials into browser and
network state, but a deliberate authenticated reveal is appropriate for this
trust model.

## Decision

Client Keys created through the control plane retain two values:

- `key_hash`, a bcrypt hash that remains the sole authentication source of truth;
- `raw_key`, a recoverable copy stored in the private local YAML configuration.

The following invariants apply:

1. Client-key list responses expose only name, prefix, policy, status, and a
   `recoverable` boolean.
2. The complete value is returned only by the explicit authenticated
   `GET /admin/client-keys/{name}` endpoint or by the existing authenticated raw
   configuration editor.
3. Create and reveal responses use `Cache-Control: no-store`.
4. Runtime snapshots, telemetry, metrics, logs, and error messages never contain
   the complete key.
5. Existing hash-only keys remain valid and are reported as non-recoverable. No
   migration pretends to reverse bcrypt; users must rotate or recreate those
   keys if they need later viewing.
6. Updates to status, model policy, or rate limit preserve `raw_key`, while key
   deletion removes both stored forms.
7. Rotation replaces the hash, recoverable value, and prefix atomically while
   preserving status and policy. The previous credential becomes invalid.

## Consequences

### Positive

- Local users can copy a Client Key again without invalidating active Agents.
- Routine list and observability paths remain credential-free.
- Authentication behavior and bcrypt comparison remain unchanged.
- The optional field preserves compatibility with existing configuration files.

### Negative

- Anyone who can read the local config file can use its recoverable Client Keys.
- Configuration backups now contain usable data-plane credentials.
- A future hosted or multi-tenant deployment must replace this storage policy
  with an encrypted secret backend or an explicit non-recoverable mode.

### Neutral

- Provider credentials keep their existing secret-reference behavior and are not
  affected by this decision.
- Management passwords remain one-way Argon2id hashes and cannot be revealed.

## Alternatives considered

### Keep one-time display and add rotation only

Rejected for the local default because rotation disrupts every Agent currently
using the key and does not solve the common copy-to-another-client workflow.

### Return complete keys from the collection endpoint

Rejected because routine page loads and refetches do not need every credential.
An explicit per-key reveal keeps the sensitive operation intentional and
auditable at the API boundary.

### Require a separately managed encryption master key

Rejected for the default local experience because it introduces another secret
that users must provision and recover. This remains an appropriate future mode
for hosted deployments.

## Security and operational considerations

- The desktop and config writers use owner-only file permissions where the
  platform supports POSIX modes; Windows relies on the user's application-data
  directory ACL.
- The control-plane listener remains loopback-only under ADR-0009.
- The reveal operation requires the same Admin authorization as provider and
  configuration management.
- Support bundles and diagnostics must continue excluding configuration secrets.

## References

- [ADR-0005: Wails desktop gateway lifecycle](0005-wails-desktop-gateway-lifecycle.md)
- [ADR-0009: Separate data and control-plane listeners](0009-separate-data-and-control-plane-listeners.md)
- [Architecture](../architecture.md)
- [Admin API](../admin-api.md)
