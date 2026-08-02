# Architecture Decision Records Governance and Backfill Design

**Status:** Approved for implementation on 2026-08-02

## 1. Context

vibe-proxy already has substantial architecture documentation. The current-state documents describe the runtime, protocols, configuration, and extension points, while dated design documents explain how larger features were planned and implemented.

The repository does not, however, have a durable architecture decision log. Important choices are spread across long-form documents and commit history, which makes several questions unnecessarily difficult to answer:

- Why was the current approach selected?
- Which alternatives were rejected?
- Which constraints must future changes preserve?
- Is a decision still active, deprecated, or superseded?
- Does a bug fix restore an existing decision or introduce a new one?

Adding a single ADR for the current models.dev capability fix would not solve this problem. Calling it the first ADR would also obscure several earlier decisions that still define the system.

## 2. Goals

- Establish a small, durable ADR system under `docs/adr/`.
- Distinguish current-state documentation, implementation design documents, and decision records.
- Retrospectively record only the major architectural decisions that are still active.
- Define an objective trigger for when future pull requests require an ADR.
- Make supersession explicit instead of rewriting accepted decisions.
- Connect the in-flight models.dev fix to the existing capability-resolution decision.

## 3. Non-goals

- Creating one ADR for every historical commit, feature, or bug fix.
- Reconstructing discussions that cannot be supported by repository evidence.
- Replacing current-state documentation or detailed design documents with ADRs.
- Changing runtime behavior as part of the documentation backfill.
- Turning ADR approval into a heavyweight committee process.

## 4. Documentation Model

The repository will use three complementary documentation layers.

### 4.1 Current-state documentation

Documents such as `docs/architecture.md`, `docs/config-schema.md`, and `docs/admin-api.md` describe how the current version works. They may be updated in place whenever behavior changes.

### 4.2 Design specifications and implementation plans

Dated files under `docs/superpowers/specs/` and `docs/superpowers/plans/` capture the scope, alternatives, sequencing, and verification strategy for a bounded body of work. They are historical implementation artifacts and are not the authoritative description of the current version.

### 4.3 Architecture Decision Records

Files under `docs/adr/` record one durable decision each. An ADR focuses on context, the selected decision, alternatives, consequences, and relationships to earlier decisions. Accepted ADRs are historical records; a later decision supersedes them instead of silently rewriting their meaning.

## 5. ADR Trigger

A change requires a new ADR when it introduces or changes at least one of the following:

- ownership or boundaries between major modules;
- a public protocol, API, configuration, or compatibility contract;
- persisted data format, storage technology, or migration policy;
- an authentication, authorization, secret-handling, or trust boundary;
- cross-cutting runtime policy such as routing, fallback, or precedence;
- supported deployment topology, platform matrix, or release artifact policy;
- a long-lived operational constraint with meaningful maintenance cost.

A new ADR is normally not required for:

- a bug fix that restores behavior already required by an accepted ADR;
- a local refactor that preserves boundaries and contracts;
- tests for existing behavior;
- presentation-only UI changes;
- dependency or tool updates that do not alter a durable architectural constraint;
- documentation corrections.

Those changes must still update affected current-state documentation when necessary and should reference the governing ADR in the pull request.

## 6. ADR Format and Lifecycle

Each ADR will use a four-digit sequential identifier and a descriptive filename, for example `0001-canonical-ir-adapter-boundary.md`.

The standard record contains:

- title and identifier;
- status;
- original decision date, when known;
- recording date when the ADR is retrospective;
- context and constraints;
- decision;
- positive, negative, and neutral consequences;
- alternatives considered;
- security and operational considerations when applicable;
- references to current documentation, design documents, commits, or pull requests;
- supersession relationships.

Supported statuses are:

- **Proposed:** under review and not yet authoritative;
- **Accepted:** the active architectural decision;
- **Deprecated:** retained for compatibility but no longer recommended;
- **Superseded by ADR-NNNN:** replaced by a later decision.

After acceptance, only spelling, link, metadata, and status corrections may be edited in place. A material decision change requires a new ADR with explicit `Supersedes` and `Superseded by` links.

## 7. Retrospective Backfill Scope

The first documentation pull request will backfill six decisions. The ordering follows architectural dependency and original introduction date rather than the date on which the ADR text is written.

### ADR-0001: Use Canonical IR between client and provider adapters

Record the canonical request, response, and stream-event boundary that keeps client protocols independent from provider protocols. Evidence comes from the canonical runtime implementation, conformance tests, and current architecture documentation introduced around 2026-07-05.

### ADR-0002: Resolve model capabilities through explicit precedence

Record the authoritative precedence used for model capabilities:

1. per-model manual correction;
2. provider-level default;
3. models.dev catalog metadata;
4. unknown when no source resolves the capability.

Unknown and explicitly unsupported models must not be treated as vision-capable. All runtime, validation, admin API, and UI consumers must use the effective resolved capability rather than reimplementing partial rules.

The current models.dev OCR fallback defect is a conformance bug against this decision, so its fix should reference ADR-0002 instead of creating a separate architectural decision.

### ADR-0003: Use OCR-then-Vision preprocessing for incompatible image requests

Record the multimodal preprocessing boundary, tri-state capability model, safe treatment of unknown capability, OCR replacement behavior, optional Vision fallback, limits, and preservation of original images for vision-capable targets. Evidence comes from the OCR design and implementation introduced around 2026-07-20.

### ADR-0004: Use pure-Go SQLite for the embedded store

Record the choice to remove CGO from the default build, preserve SQLite data compatibility, and accept the operational and performance trade-offs of the selected pure-Go driver. Evidence comes from the migration design and implementation introduced on 2026-07-29.

### ADR-0005: Embed and own the local gateway lifecycle in the Wails desktop host

Record the Wails selection, shared gateway lifecycle, loopback control-plane boundary, session bootstrap, application-data ownership, startup recovery, and desktop packaging constraints. Evidence comes from the desktop design and implementation introduced from 2026-07-30 onward.

### ADR-0006: Publish an installer-oriented cross-platform artifact matrix

Record the durable distribution policy: Windows and macOS receive installable desktop packages, Linux receives a CLI installation package, and portable duplicate artifacts are not part of the default release matrix. The ADR will identify GitHub Actions storage and release clarity as operational constraints and will reference the in-flight installer-only release work.

Because the artifact-policy implementation is currently on a separate branch, the ADR records the accepted target decision and the release branch remains responsible for satisfying it.

## 8. Repository Integration

The documentation change will add:

- `docs/adr/README.md` with the index, trigger, lifecycle, and template;
- ADR-0001 through ADR-0006;
- an ADR section in `docs/contributing-architecture.md`;
- `.github/pull_request_template.md` with a lightweight architecture-impact check.

The pull request checklist will ask whether the change affects architecture, public contracts, persistence, security boundaries, cross-cutting runtime policy, or release topology. Authors will either link an ADR or mark the item not applicable with a short reason.

No CI check will initially attempt to infer whether an ADR is required. Automated enforcement would be unreliable without repository-specific evidence and can be considered only if the manual workflow proves insufficient.

## 9. Interaction with In-flight Work

After the ADR documentation branch is merged:

- the models.dev fallback branch will update its design and plan to reference ADR-0002 and remove the proposed standalone ADR-0001 task;
- the implementation will update current-state API and configuration documentation because observable behavior changes;
- the installer-only release branch will reference ADR-0006;
- neither branch will duplicate the retrospective ADR text.

This keeps feature pull requests focused while still making the governing decisions discoverable.

## 10. Risks and Mitigations

### Retrospective hindsight bias

Historical rationale may be overstated when written after implementation. Each ADR will distinguish repository evidence from present-day interpretation, use original dates only when supported, and avoid inventing unavailable discussion.

### ADR proliferation

If every meaningful code change gets an ADR, the log becomes noise. The trigger is limited to durable architectural choices; fixes that restore an accepted decision cite that decision instead.

### Documentation drift

ADRs explain why, but do not replace current behavior documentation. Pull requests that alter observable behavior must update current-state documents separately.

### In-flight branch mismatch

ADR-0002 and ADR-0006 govern work that is not yet fully merged into `main`. Their references will clearly identify the implementation branch or follow-up pull request, and verification will confirm that no ADR claims an already-shipped behavior that is still only proposed.

### Process friction

The initial workflow remains review-based and adds only one checklist item. No mandatory automation or external approval service is introduced.

## 11. Verification and Acceptance Criteria

The documentation backfill is complete when:

- all six ADRs follow the repository template and appear in the index;
- every retrospective claim has a repository document or commit reference;
- every ADR covers alternatives, trade-offs, and relevant failure or security implications;
- supersession and status rules are unambiguous;
- contributor and pull-request guidance use the same trigger criteria;
- local Markdown links resolve;
- the change contains no runtime or configuration modification;
- the models.dev and installer-only branches have an explicit follow-up path to their governing ADRs.

