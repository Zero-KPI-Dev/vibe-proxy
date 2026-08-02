# Architecture Decision Records Governance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish a durable ADR workflow and retrospectively record the six active architectural decisions approved in the governance design.

**Architecture:** Add a self-contained `docs/adr/` decision log that complements, rather than replaces, current-state and dated design documentation. Integrate the trigger into contributor and pull-request guidance, then validate every historical claim and local link against repository evidence.

**Tech Stack:** Markdown, Git history, PowerShell validation commands

## Global Constraints

- Do not change runtime code, configuration behavior, workflows, or dependencies.
- Backfill only the six decisions approved in `docs/superpowers/specs/2026-08-02-architecture-decision-records-governance-design.md`.
- Do not invent historical rationale; distinguish repository evidence from present-day interpretation.
- Accepted ADRs may later receive only spelling, link, metadata, or status corrections; material changes require a superseding ADR.
- Use four-digit sequential identifiers and one decision per ADR.
- A bug fix that restores an accepted ADR references that ADR instead of creating a new decision record.
- Keep the review workflow manual; do not add CI enforcement in this change.

---

## File Map

- Create `docs/adr/README.md`: ADR purpose, trigger, lifecycle, index, template, and authoring rules.
- Create `docs/adr/0001-canonical-ir-adapter-boundary.md`: Canonical IR and adapter separation decision.
- Create `docs/adr/0002-model-capability-resolution-precedence.md`: effective model capability source precedence.
- Create `docs/adr/0003-ocr-then-vision-image-fallback.md`: multimodal preprocessing and fallback boundary.
- Create `docs/adr/0004-pure-go-sqlite-storage.md`: pure-Go SQLite and CGO-free build decision.
- Create `docs/adr/0005-wails-desktop-gateway-lifecycle.md`: desktop host, gateway lifecycle, and security boundary.
- Create `docs/adr/0006-installer-oriented-release-artifacts.md`: supported platform artifact matrix.
- Modify `docs/contributing-architecture.md`: point contributors to the ADR trigger and explain when to cite versus create an ADR.
- Create `.github/pull_request_template.md`: add a lightweight architecture-impact checklist without requiring CI inference.

---

### Task 1: Establish the ADR workflow and review trigger

**Files:**

- Create: `docs/adr/README.md`
- Modify: `docs/contributing-architecture.md`
- Create: `.github/pull_request_template.md`

**Interfaces:**

- Consumes: the trigger, lifecycle, and immutability rules in the approved governance design.
- Produces: the normative ADR template and authoring workflow used by Tasks 2 and 3.

- [ ] **Step 1: Create the ADR index and authoring guide**

Write `docs/adr/README.md` with these exact sections:

```markdown
# Architecture Decision Records

## Purpose
## Documentation Layers
## When an ADR Is Required
## When an ADR Is Not Required
## Status and Supersession
## Naming and Numbering
## Retrospective Records
## Index
## Template
```

The `Index` table must list ADR-0001 through ADR-0006 with `Accepted` status. The template must include `Status`, `Decision date`, `Recorded date`, `Context`, `Decision`, `Consequences` with positive/negative/neutral subsections, `Alternatives considered`, `Security and operational considerations`, and `References`.

- [ ] **Step 2: Integrate ADR guidance into contributor documentation**

Append an `Architecture Decision Records` section to `docs/contributing-architecture.md`. It must:

- link to `adr/README.md`;
- repeat the high-level trigger categories without copying the entire template;
- state that fixes restoring an accepted decision cite the ADR;
- state that material decision changes create a superseding ADR;
- remind authors to update current-state documentation independently.

- [ ] **Step 3: Add the pull-request architecture check**

Create `.github/pull_request_template.md` with concise `Summary`, `Verification`, and `Architecture impact` sections. The architecture section must ask authors to check one of:

```markdown
- [ ] No durable architecture decision is introduced or changed.
- [ ] This change follows an existing ADR: <!-- ADR link -->
- [ ] This change introduces or supersedes a decision: <!-- ADR link -->
```

It must also ask whether observable API, configuration, or operational behavior requires current-state documentation updates.

- [ ] **Step 4: Verify workflow consistency**

Run:

```powershell
rg -n "public protocol|persisted data|security|runtime policy|release artifact|supersed" docs/adr/README.md docs/contributing-architecture.md .github/pull_request_template.md
```

Expected: the README contains the detailed trigger and lifecycle; contributor guidance and the PR template point to the same policy without contradictory terminology.

- [ ] **Step 5: Commit the workflow**

```powershell
git add -- docs/adr/README.md docs/contributing-architecture.md .github/pull_request_template.md
git commit -m "docs: establish ADR governance"
```

---

### Task 2: Backfill runtime and multimodal decisions

**Files:**

- Create: `docs/adr/0001-canonical-ir-adapter-boundary.md`
- Create: `docs/adr/0002-model-capability-resolution-precedence.md`
- Create: `docs/adr/0003-ocr-then-vision-image-fallback.md`

**Interfaces:**

- Consumes: the ADR template from Task 1 and repository evidence in `docs/architecture.md`, `docs/canonical-ir.md`, `docs/adapter-sdk.md`, `docs/ocr-fallback-design.md`, and the cited commits.
- Produces: governing decisions for protocol translation, model capability resolution, and image fallback consumers.

- [ ] **Step 1: Record the Canonical IR boundary**

Create ADR-0001 with original decision date `2026-07-05` and recorded date `2026-08-02`. The decision must state that client adapters decode into Canonical IR, provider adapters encode from it, and canonical stream events separate upstream streaming from downstream protocol rendering.

Alternatives must include direct client-to-provider pairwise translation and a provider-native internal representation. Consequences must include reduced adapter coupling, a required extension mechanism for vendor-specific fields, and the cost of maintaining canonical semantics.

References must include:

- `../architecture.md`
- `../canonical-ir.md`
- `../adapter-sdk.md`
- commits `ebca59b`, `6d157af`, and `386d8cc`

- [ ] **Step 2: Record effective capability precedence**

Create ADR-0002 with original decision date `2026-07-20` and recorded date `2026-08-02`. State this precedence exactly:

1. per-model manual correction;
2. provider-level default;
3. models.dev catalog metadata;
4. unknown.

State that explicit unsupported and unresolved unknown capabilities are never eligible for a Vision fallback target. Require runtime, validation, admin API, and UI code to consume the same effective resolution rather than rebuilding a partial rule.

Alternatives must include manual-only declarations, catalog-only declarations, model-name heuristics, and optimistic treatment of unknown. References must include `../ocr-fallback-design.md`, `../config-schema.md`, commit `788e74b`, and the models.dev remediation design at `docs/superpowers/specs/2026-08-02-models-dev-vision-fallback-design.md` in commit `277a31a` on `codex/fix-models-dev-vision-fallback`.

- [ ] **Step 3: Record the OCR-then-Vision fallback boundary**

Create ADR-0003 with original decision date `2026-07-20` and recorded date `2026-08-02`. State that image-bearing requests keep original images for vision-capable targets; incompatible targets may use bounded OCR; and optional Vision fallback is used only when OCR output is unusable and the fallback target resolves as vision-capable.

Include image-size/count limits, no implicit remote URL download in the OCR path, separation of OCR-derived text from instructions, telemetry, fail-open versus fail-closed consequences, and unknown capability exclusion.

Alternatives must include rejecting incompatible requests, always using a vision model, performing OCR inside provider adapters, and sending unknown targets the original image. References must include `../ocr-fallback-design.md` and commits `0326666`, `68b781f`, `052207e`, `6f188b2`, and `73ef6c1`.

- [ ] **Step 4: Verify runtime ADR evidence and terminology**

Run:

```powershell
rg -n "Canonical IR|manual correction|provider-level default|models.dev|unknown|original images|remote URL|Security and operational" docs/adr/0001-*.md docs/adr/0002-*.md docs/adr/0003-*.md
git show --no-patch --oneline ebca59b 6d157af 386d8cc 788e74b 277a31a 0326666 68b781f 052207e 6f188b2 73ef6c1
```

Expected: every required term is present and every referenced commit resolves.

- [ ] **Step 5: Commit the runtime ADRs**

```powershell
git add -- docs/adr/0001-canonical-ir-adapter-boundary.md docs/adr/0002-model-capability-resolution-precedence.md docs/adr/0003-ocr-then-vision-image-fallback.md
git commit -m "docs: record runtime architecture decisions"
```

---

### Task 3: Backfill storage, desktop, and release decisions

**Files:**

- Create: `docs/adr/0004-pure-go-sqlite-storage.md`
- Create: `docs/adr/0005-wails-desktop-gateway-lifecycle.md`
- Create: `docs/adr/0006-installer-oriented-release-artifacts.md`

**Interfaces:**

- Consumes: the ADR template from Task 1 and repository evidence in the pure-Go SQLite, desktop GUI, and cross-platform release designs.
- Produces: governing decisions for storage portability, desktop process ownership, and release artifacts.

- [ ] **Step 1: Record the pure-Go SQLite decision**

Create ADR-0004 with original decision date `2026-07-29` and recorded date `2026-08-02`. State that the embedded store uses a pure-Go SQLite driver to keep default and cross-platform builds CGO-free while retaining the SQLite file format and Store interface.

Alternatives must include retaining `mattn/go-sqlite3`, selecting a different embedded database, and offering two build variants. Address driver-behavior compatibility, performance uncertainty, backup/recovery, and cross-compilation consequences. Reference `../superpowers/specs/2026-07-29-go-lifecycle-pure-go-sqlite-design.md` and commits `d35ffd1` and `9a4caf7`.

- [ ] **Step 2: Record the Wails desktop lifecycle decision**

Create ADR-0005 with original decision date `2026-07-30` and recorded date `2026-08-02`. State that the Wails desktop host embeds and owns the same gateway lifecycle used by the CLI/server, binds the management surface to loopback, bootstraps an authenticated browser session, stores data in the platform application-data directory, and surfaces startup/recovery failures.

Alternatives must include Electron, Tauri, an external child-process gateway, and a remote-only web UI. Address loopback trust limitations, session/password boundaries, single-owner startup/shutdown, port conflict, WebView failure, and packaging cost. Reference `../superpowers/specs/2026-07-30-desktop-gui-design.md` and commits `baceb95`, `15ec5be`, `a652ab0`, `dfd982a`, `c62553d`, `a71b380`, and `d0d9036`.

- [ ] **Step 3: Record the installer-oriented artifact decision**

Create ADR-0006 with original decision date `2026-08-02` and recorded date `2026-08-02`. State the target matrix precisely:

- Windows: installable desktop package;
- macOS: installable desktop package;
- Linux: CLI installation package;
- no portable duplicate archive in the default matrix.

Alternatives must include retaining the broad portable/native matrix, publishing only raw binaries, and requiring users to build locally. Address GitHub Actions storage/retention, signing/notarization as follow-up concerns, unsupported Linux GUI expectations, and the fact that implementation is tracked on `codex/slim-release-installers`. Reference `../release.md`, `../superpowers/specs/2026-07-30-cross-platform-release-design.md`, and commits `e8971df`, `733038c`, and `e138814` from the in-flight branch.

- [ ] **Step 4: Verify platform ADR evidence and scope**

Run:

```powershell
rg -n "CGO|Wails|loopback|session|Windows|macOS|Linux|portable|Security and operational" docs/adr/0004-*.md docs/adr/0005-*.md docs/adr/0006-*.md
git show --no-patch --oneline d35ffd1 9a4caf7 baceb95 15ec5be a652ab0 dfd982a c62553d a71b380 d0d9036 e8971df 733038c e138814
```

Expected: every required constraint is present and every referenced commit resolves, including commits reachable only from the installer branch.

- [ ] **Step 5: Commit the platform ADRs**

```powershell
git add -- docs/adr/0004-pure-go-sqlite-storage.md docs/adr/0005-wails-desktop-gateway-lifecycle.md docs/adr/0006-installer-oriented-release-artifacts.md
git commit -m "docs: record platform architecture decisions"
```

---

### Task 4: Validate the complete decision log

**Files:**

- Verify: `docs/adr/*.md`
- Verify: `docs/contributing-architecture.md`
- Verify: `.github/pull_request_template.md`
- Verify: `docs/superpowers/specs/2026-08-02-architecture-decision-records-governance-design.md`

**Interfaces:**

- Consumes: all documentation produced by Tasks 1 through 3.
- Produces: an evidence-backed, link-consistent ADR set ready for pull-request review.

- [ ] **Step 1: Check required sections and prohibited placeholders**

Run:

```powershell
$adrFiles = Get-ChildItem -LiteralPath docs/adr -Filter '*.md' | Where-Object { $_.Name -ne 'README.md' }
$required = @('## Status', '## Decision date', '## Recorded date', '## Context', '## Decision', '## Consequences', '## Alternatives considered', '## Security and operational considerations', '## References')
foreach ($file in $adrFiles) { $content = Get-Content -Raw -LiteralPath $file.FullName; foreach ($heading in $required) { if (-not $content.Contains($heading)) { throw "$($file.Name) missing $heading" } } }
if ($adrFiles.Count -ne 6) { throw "Expected 6 ADRs, found $($adrFiles.Count)" }
rg -n "T[B]D|T[O]DO|F[I]XME|PLACEH[O]LDER" docs/adr docs/contributing-architecture.md .github/pull_request_template.md
```

Expected: the heading script exits successfully; the placeholder search produces no matches.

- [ ] **Step 2: Check relative Markdown links**

Run:

```powershell
$files = Get-ChildItem -LiteralPath docs/adr -Filter '*.md'
$linkPattern = '\[[^\]]+\]\((?!https?://|mailto:|#)([^)#]+)(?:#[^)]+)?\)'
foreach ($file in $files) {
    $content = Get-Content -Raw -LiteralPath $file.FullName
    foreach ($match in [regex]::Matches($content, $linkPattern)) {
        $target = [Uri]::UnescapeDataString($match.Groups[1].Value)
        $resolved = [IO.Path]::GetFullPath((Join-Path $file.DirectoryName $target.Replace('/', [IO.Path]::DirectorySeparatorChar)))
        if (-not (Test-Path -LiteralPath $resolved)) {
            throw "$($file.Name) has missing link target $target"
        }
    }
}
```

Expected: all local references resolve.

- [ ] **Step 3: Check formatting and branch scope**

Run:

```powershell
git diff origin/main...HEAD --check
git diff --check
git status --short
git diff origin/main...HEAD --stat
```

Expected: both diff checks succeed; status is clean after the task commits; the branch diff contains only Markdown documentation and the pull-request template.

- [ ] **Step 4: Review decision consistency**

Confirm manually:

- ADR-0002 excludes unknown and unsupported fallback targets;
- ADR-0003 consumes ADR-0002 rather than defining a conflicting precedence;
- ADR-0005 separates desktop lifecycle ownership from release packaging;
- ADR-0006 describes the accepted target and clearly labels the implementation branch;
- README, contributor guidance, and PR template use the same ADR trigger;
- no ADR presents a design specification as authoritative current behavior.

- [ ] **Step 5: Record validation-only completion**

No additional commit is required if validation produces no edits. If a correction is needed, stage only the corrected documentation and commit it as:

```powershell
git add -- docs/adr docs/contributing-architecture.md .github/pull_request_template.md
git commit -m "docs: align ADR references and guidance"
```
