# Architecture Decision Records

Architecture Decision Records (ADRs) preserve the reasoning behind durable technical choices in vibe-proxy. They complement the current-state documentation and dated implementation designs; they do not replace either one.

## Purpose

Use an ADR to make a long-lived decision discoverable, reviewable, and explicitly supersedable. A reader should be able to understand why the decision exists, what alternatives were rejected, which constraints future changes must preserve, and what costs the project accepted.

## Documentation Layers

- Current-state documents such as [Architecture](../architecture.md), [Configuration Schema](../config-schema.md), and [Admin API](../admin-api.md) describe how the current version works and may be updated in place.
- Dated specifications and plans under `docs/superpowers/` capture the scope and execution history of a bounded change. They are not authoritative current-state references.
- ADRs under this directory record one durable decision each. An accepted ADR remains historical evidence after a later ADR supersedes it.

## When an ADR Is Required

Create a new ADR when a change introduces or materially changes any of these:

- ownership or boundaries between major modules;
- a public protocol, API, configuration, or compatibility contract;
- persisted data format, storage technology, or migration policy;
- an authentication, authorization, secret-handling, or trust boundary;
- cross-cutting runtime policy such as routing, fallback, or precedence;
- supported deployment topology, platform matrix, or release artifact policy;
- a long-lived operational constraint with meaningful maintenance cost.

## When an ADR Is Not Required

A new ADR is normally unnecessary for:

- a bug fix that restores behavior already required by an accepted ADR;
- a local refactor that preserves boundaries and contracts;
- tests for existing behavior;
- presentation-only UI changes;
- dependency or tool updates that do not alter a durable constraint;
- documentation corrections.

Reference the governing ADR in the pull request when restoring or implementing an existing decision. Update affected current-state documentation whenever observable behavior changes, whether or not a new ADR is required.

## Status and Supersession

- **Proposed:** under review and not yet authoritative.
- **Accepted:** the active architectural decision.
- **Deprecated:** retained for compatibility but no longer recommended.
- **Superseded by ADR-NNNN:** replaced by a later accepted decision.

After acceptance, edit an ADR in place only to correct spelling, links, metadata, or status. A material change requires a new ADR. The new ADR links to the earlier record with `Supersedes`, and the earlier record changes status to `Superseded by ADR-NNNN`.

## Naming and Numbering

- Use the next four-digit sequential identifier.
- Use one decision per file.
- Name files `NNNN-short-descriptive-title.md`.
- Number retrospective records by architectural dependency and original decision date, not by the date their text was written.
- Do not reuse an identifier after an ADR is removed or superseded.

## Retrospective Records

Retrospective ADRs must distinguish the original decision date from the recording date. Use repository documents, tests, implementation commits, or pull requests as evidence. Do not reconstruct or attribute discussions that are not present in the repository; label present-day interpretation as such when needed.

## Index

| ADR | Decision | Status | Original decision date |
| --- | --- | --- | --- |
| [ADR-0001](0001-canonical-ir-adapter-boundary.md) | Use Canonical IR between client and provider adapters | Accepted | 2026-07-05 |
| [ADR-0002](0002-model-capability-resolution-precedence.md) | Resolve model capabilities through explicit precedence | Accepted | 2026-07-20 |
| [ADR-0003](0003-ocr-then-vision-image-fallback.md) | Use OCR-then-Vision preprocessing for incompatible image requests | Accepted | 2026-07-20 |
| [ADR-0004](0004-pure-go-sqlite-storage.md) | Use pure-Go SQLite for the embedded store | Accepted | 2026-07-29 |
| [ADR-0005](0005-wails-desktop-gateway-lifecycle.md) | Embed and own the local gateway lifecycle in the Wails desktop host | Accepted | 2026-07-30 |
| [ADR-0006](0006-installer-oriented-release-artifacts.md) | Publish an installer-oriented cross-platform artifact matrix | Accepted | 2026-08-02 |

## Template

```markdown
# ADR-NNNN: Decision title

## Status

Proposed

## Decision date

YYYY-MM-DD

## Recorded date

YYYY-MM-DD

## Context

Describe the problem, constraints, and forces that require a durable decision.

## Decision

State the selected approach and its required invariants.

## Consequences

### Positive

- Benefit introduced by the decision.

### Negative

- Cost or limitation accepted by the decision.

### Neutral

- Relevant side effect that is neither inherently positive nor negative.

## Alternatives considered

Describe each meaningful alternative and why it was not selected.

## Security and operational considerations

Describe trust boundaries, failure modes, maintenance, rollout, and recovery implications. Write `None beyond existing project constraints` only when the decision genuinely adds none.

## References

- [Architecture or another relevant repository document](../architecture.md)
- Repository commit or pull request
```
