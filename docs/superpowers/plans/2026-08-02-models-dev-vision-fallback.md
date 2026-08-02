# models.dev Vision Fallback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make models.dev the default source for OCR Vision fallback candidates while preserving manual capability overrides and excluding unknown models.

**Architecture:** The runtime server will expose catalog-aware Vision candidates and validate saves through the existing `multimodal.ResolveCapabilities` source of truth. Static config validation will distinguish an absent capability, which may be supplied by models.dev at runtime, from an explicit `unknown` or `unsupported` override. The frontend will render backend candidates instead of rebuilding capability logic.

**Tech Stack:** Go 1.25, `net/http`, existing model catalog and multimodal packages, React 19, TypeScript, Radix Select, Node static contract tests.

## Global Constraints

- Capability precedence is exactly: per-model override, provider default, models.dev, unknown.
- Only an effective `image_input: supported` model may be selected or used as Vision fallback.
- Missing, not-found, ambiguous, and unknown catalog results never enter Vision fallback.
- Catalog results are not persisted to `config.yaml`.
- Rejected Admin API updates must not modify the config file or active runtime snapshot.
- The implementation follows ADR-0002 from documentation PR #13, does not create a duplicate ADR, and updates all affected public documentation.
- No new runtime or frontend dependency is introduced.

---

### Task 1: Make static validation catalog-deferable

**Files:**
- Modify: `internal/config/validate.go:197-218`
- Modify: `internal/config/validate_test.go:132-166`

**Interfaces:**
- Consumes: `ProviderConfig.DefaultCapabilities`, `ProviderConfig.ModelCapabilities`, and `modelcapability.SupportState`.
- Produces: `validateVisionFallback(*RuntimeConfig) []ValidationIssue` with error-versus-warning semantics that later runtime validation relies on.

- [ ] **Step 1: Write failing tests for absent and explicit capabilities**

Replace the single explicit-capability test with table-driven coverage:

```go
func TestValidateRuntimeVisionFallbackCapabilityPrecedence(t *testing.T) {
    tests := []struct {
        name               string
        providerCapability modelcapability.SupportState
        modelCapability    modelcapability.SupportState
        wantError          bool
        wantCode           string
    }{
        {name: "explicit supported", providerCapability: modelcapability.SupportSupported},
        {name: "catalog deferred", wantCode: "vision_fallback_unverified"},
        {name: "provider unknown", providerCapability: modelcapability.SupportUnknown, wantError: true, wantCode: "vision_fallback_invalid"},
        {name: "provider unsupported", providerCapability: modelcapability.SupportUnsupported, wantError: true, wantCode: "vision_fallback_invalid"},
        {name: "model unknown overrides absent default", modelCapability: modelcapability.SupportUnknown, wantError: true, wantCode: "vision_fallback_invalid"},
    }
    for _, test := range tests {
        t.Run(test.name, func(t *testing.T) {
            modelCapabilities := map[string]modelcapability.ModelCapabilities{}
            if test.modelCapability != "" {
                modelCapabilities["vision-model"] = modelcapability.ModelCapabilities{ImageInput: test.modelCapability}
            }
            cfg, err := CompileSimple(SimpleConfig{
                Multimodal: MultimodalConfig{Enabled: true, VisionFallbackModel: "vibe-vision"},
                Providers: map[string]ProviderConfig{"vision": {
                    Type: "openai-compatible", BaseURL: "https://vision.example/v1",
                    Auth: upstreamauth.Profile{Type: "none"}, Models: []string{"vision-model"},
                    DefaultCapabilities: modelcapability.ModelCapabilities{ImageInput: test.providerCapability},
                    ModelCapabilities: modelCapabilities,
                }},
                Models: ModelsConfig{Aliases: map[string]string{"vibe-vision": "vision/vision-model"}},
            })
            if err != nil { t.Fatal(err) }
            issues := ValidateRuntime(cfg)
            if HasErrors(issues) != test.wantError { t.Fatalf("issues = %+v", issues) }
            if test.wantCode != "" && !hasIssueCode(issues, test.wantCode) {
                t.Fatalf("missing %s: %+v", test.wantCode, issues)
            }
        })
    }
}
```

- [ ] **Step 2: Run the focused test and verify RED**

```powershell
go test ./internal/config -run TestValidateRuntimeVisionFallbackCapabilityPrecedence -count=1
```

Expected: FAIL because an absent capability currently produces `vision_fallback_invalid` instead of a warning.

- [ ] **Step 3: Implement explicit-versus-absent validation**

```go
support := provider.DefaultCapabilities.ImageInput
explicit := support != ""
if model, exists := provider.ModelCapabilities[target.Model]; exists && model.ImageInput != "" {
    support = model.ImageInput
    explicit = true
}
if !explicit {
    return []ValidationIssue{issue(
        "warning",
        "multimodal.vision_fallback_model",
        "vision_fallback_unverified",
        "Vision fallback image support will be verified from models.dev at runtime.",
    )}
}
if support != modelcapability.SupportSupported {
    return []ValidationIssue{issue(
        "error",
        "multimodal.vision_fallback_model",
        "vision_fallback_invalid",
        "Vision fallback model is explicitly marked as not supporting image input.",
    )}
}
```

- [ ] **Step 4: Run focused config tests and verify GREEN**

```powershell
go test ./internal/config -run 'TestValidateRuntime.*VisionFallback' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the static validation behavior**

```powershell
git add internal/config/validate.go internal/config/validate_test.go
git commit -m "fix: defer catalog-backed vision validation"
```

---

### Task 2: Expose and enforce effective Vision candidates

**Files:**
- Create: `internal/runtime/vision_fallback_models.go`
- Create: `internal/runtime/multimodal_handlers_test.go`
- Modify: `internal/runtime/multimodal_handlers.go:15-48,143-160`

**Interfaces:**
- Consumes: `multimodal.ResolveCapabilities`, `modelresolver.New`, `Server.catalog`, `Server.providerAdapters`, and `config.RuntimeConfig`.
- Produces:
  - `type visionFallbackModel struct { Target, ProviderID, Model string; Source modelcapability.Source }`
  - `func (s *Server) visionFallbackModels(cfg *config.RuntimeConfig) []visionFallbackModel`
  - `func (s *Server) validateVisionFallbackSelection(cfg *config.RuntimeConfig, requested string) error`
  - `func (s *Server) multimodalAdminSnapshot(cfg *config.RuntimeConfig) map[string]any`

- [ ] **Step 1: Write failing candidate-generation tests**

Create focused runtime tests with a catalog imported through `s.catalog.Import`:

```go
func TestRuntimeAdminMultimodalListsEffectiveVisionFallbackModels(t *testing.T) {
    t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
    cfg, err := config.CompileSimple(config.SimpleConfig{
        Security: config.SecurityConfig{AdminBearerTokenEnv: "VIBE_PROXY_ADMIN_TOKEN"},
        Providers: map[string]config.ProviderConfig{"gateway": {
            Type: "openai-compatible", BaseURL: "https://gateway.example/v1",
            CatalogProvider: "moonshotai", Auth: upstreamauth.Profile{Type: "none"},
            Models: []string{"catalog-vision", "blocked", "manual-vision", "unknown"},
            ModelCapabilities: map[string]modelcapability.ModelCapabilities{
                "blocked": {ImageInput: modelcapability.SupportUnsupported},
                "manual-vision": {ImageInput: modelcapability.SupportSupported},
            },
        }, "ambiguous-proxy": {
            Type: "openai-compatible", BaseURL: "https://proxy.example/v1",
            Auth: upstreamauth.Profile{Type: "none"}, Models: []string{"shared"},
        }},
        Models: config.ModelsConfig{AllowRaw: true},
    })
    if err != nil { t.Fatal(err) }
    testPromOnce.Do(func() { testProm = metrics.New() })
    s := New("", cfg, metrics.MultiSink{}, testProm)
    err = s.catalog.Import(strings.NewReader(`{"moonshotai":{"id":"moonshotai","models":{"catalog-vision":{"id":"catalog-vision","modalities":{"input":["text","image"],"output":["text"]}},"blocked":{"id":"blocked","modalities":{"input":["text","image"],"output":["text"]}},"manual-vision":{"id":"manual-vision","modalities":{"input":["text"],"output":["text"]}},"shared":{"id":"shared","modalities":{"input":["text","image"],"output":["text"]}}}},"other":{"id":"other","models":{"shared":{"id":"shared","modalities":{"input":["text"],"output":["text"]}}}}}`))
    if err != nil { t.Fatal(err) }
    req := adminJSONRequest(http.MethodGet, "/admin/multimodal", "")
    w := httptest.NewRecorder()
    s.Routes().ServeHTTP(w, req)
    var response struct { VisionFallbackModels []visionFallbackModel `json:"vision_fallback_models"` }
    if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil { t.Fatal(err) }
    want := []visionFallbackModel{
        {Target: "gateway/catalog-vision", ProviderID: "gateway", Model: "catalog-vision", Source: modelcapability.SourceModelsDev},
        {Target: "gateway/manual-vision", ProviderID: "gateway", Model: "manual-vision", Source: modelcapability.SourceModelOverride},
    }
    if !reflect.DeepEqual(response.VisionFallbackModels, want) {
        t.Fatalf("candidates = %#v, want %#v", response.VisionFallbackModels, want)
    }
}
```

The `ambiguous-proxy/shared` model in this test proves conflicting global matches are excluded, while `catalog_provider: moonshotai` proves an explicit provider hint disambiguates catalog models.

- [ ] **Step 2: Run candidate tests and verify RED**

```powershell
go test ./internal/runtime -run TestRuntimeAdminMultimodalListsEffectiveVisionFallbackModels -count=1
```

Expected: FAIL because the response has no `vision_fallback_models`.

- [ ] **Step 3: Implement the candidate helper and enriched snapshot**

```go
type visionFallbackModel struct {
    Target     string                 `json:"target"`
    ProviderID string                 `json:"provider_id"`
    Model      string                 `json:"model"`
    Source     modelcapability.Source `json:"source"`
}

func (s *Server) visionFallbackModels(cfg *config.RuntimeConfig) []visionFallbackModel {
    providerIDs := make([]string, 0, len(cfg.Providers))
    for providerID := range cfg.Providers { providerIDs = append(providerIDs, providerID) }
    sort.Strings(providerIDs)
    result := []visionFallbackModel{}
    for _, providerID := range providerIDs {
        provider := cfg.Providers[providerID]
        adapter, ok := s.providerAdapters[provider.Type]
        if !ok || !adapter.Capabilities().Vision { continue }
        models := append([]string(nil), provider.Models...)
        sort.Strings(models)
        for _, model := range models {
            resolved := multimodal.ResolveCapabilities(providerID, model, provider, s.catalog.Snapshot())
            if resolved.Capabilities.ImageInput != modelcapability.SupportSupported { continue }
            result = append(result, visionFallbackModel{
                Target: providerID + "/" + model, ProviderID: providerID,
                Model: model, Source: resolved.Source,
            })
        }
    }
    return result
}
```

Change the snapshot function to accept the full runtime config and add:

```go
"vision_fallback_models": s.visionFallbackModels(cfg),
```

Use the enriched snapshot for both GET and successful PUT responses.

- [ ] **Step 4: Run candidate tests and verify GREEN**

```powershell
gofmt -w internal/runtime/vision_fallback_models.go internal/runtime/multimodal_handlers.go internal/runtime/multimodal_handlers_test.go
go test ./internal/runtime -run TestRuntimeAdminMultimodalListsEffectiveVisionFallbackModels -count=1
```

Expected: PASS.

- [ ] **Step 5: Write failing save-validation tests**

Define exact reusable fixtures in the test file:

```go
const catalogVisionFixture = `{"moonshotai":{"id":"moonshotai","models":{"catalog-vision":{"id":"catalog-vision","modalities":{"input":["text","image"],"output":["text"]}}}}}`

func writeCatalogVisionAdminConfig(t *testing.T) string {
    t.Helper()
    path := filepath.Join(t.TempDir(), "config.yaml")
    content := `version: vibeproxy.io/v1alpha1
security:
  admin_bearer_token_env: VIBE_PROXY_ADMIN_TOKEN
providers:
  gateway:
    type: openai-compatible
    base_url: https://gateway.example/v1
    catalog_provider: moonshotai
    auth: {type: none}
    models: [catalog-vision, unknown]
models:
  allow_raw: true
  aliases: {}
`
    if err := os.WriteFile(path, []byte(content), 0600); err != nil { t.Fatal(err) }
    return path
}
```

```go
func TestRuntimeAdminMultimodalValidatesVisionFallbackBeforeWriting(t *testing.T) {
    t.Setenv("VIBE_PROXY_ADMIN_TOKEN", "admin-token")
    path := writeCatalogVisionAdminConfig(t)
    cfg, err := config.LoadRuntime(path)
    if err != nil { t.Fatal(err) }
    testPromOnce.Do(func() { testProm = metrics.New() })
    s := New(path, cfg, metrics.MultiSink{}, testProm)
    if err := s.catalog.Import(strings.NewReader(catalogVisionFixture)); err != nil { t.Fatal(err) }

    valid := adminJSONRequest(http.MethodPut, "/admin/multimodal", `{"enabled":true,"provider":"builtin","vision_fallback_model":"gateway/catalog-vision"}`)
    validW := httptest.NewRecorder()
    s.Routes().ServeHTTP(validW, valid)
    if validW.Code != http.StatusOK { t.Fatalf("valid save: %d %s", validW.Code, validW.Body.String()) }

    before, err := os.ReadFile(path)
    if err != nil { t.Fatal(err) }
    invalid := adminJSONRequest(http.MethodPut, "/admin/multimodal", `{"enabled":true,"provider":"builtin","vision_fallback_model":"gateway/unknown"}`)
    invalidW := httptest.NewRecorder()
    s.Routes().ServeHTTP(invalidW, invalid)
    after, err := os.ReadFile(path)
    if err != nil { t.Fatal(err) }
    if invalidW.Code != http.StatusBadRequest || !strings.Contains(invalidW.Body.String(), "vision_fallback_invalid") {
        t.Fatalf("invalid save: %d %s", invalidW.Code, invalidW.Body.String())
    }
    if !bytes.Equal(before, after) { t.Fatal("invalid save overwrote config") }
}
```

Include a provider adapter without Vision support as a rejected target.

- [ ] **Step 6: Run save tests and verify RED**

```powershell
go test ./internal/runtime -run TestRuntimeAdminMultimodalValidatesVisionFallbackBeforeWriting -count=1
```

Expected: FAIL because save validation is still static-only.

- [ ] **Step 7: Implement catalog-aware pre-write validation**

```go
func (s *Server) validateVisionFallbackSelection(cfg *config.RuntimeConfig, requested string) error {
    requested = strings.TrimSpace(requested)
    if requested == "" { return nil }
    target, err := modelresolver.New(cfg.ModelResolver).Resolve(&ir.Request{RequestedModel: requested})
    if err != nil { return fmt.Errorf("vision_fallback_invalid: resolve target: %w", err) }
    provider, ok := cfg.Providers[target.ProviderID]
    if !ok { return fmt.Errorf("vision_fallback_invalid: provider is not configured") }
    resolved := multimodal.ResolveCapabilities(target.ProviderID, target.Model, provider, s.catalog.Snapshot())
    if resolved.Capabilities.ImageInput != modelcapability.SupportSupported {
        return fmt.Errorf("vision_fallback_invalid: model does not support image input")
    }
    adapter, ok := s.providerAdapters[provider.Type]
    if !ok || !adapter.Capabilities().Vision {
        return fmt.Errorf("vision_fallback_invalid: provider adapter cannot transport images")
    }
    return nil
}
```

Call it before `config.SaveMultimodal`, using `s.current().Config` because this PUT changes only multimodal fields.

- [ ] **Step 8: Run all focused backend tests and verify GREEN**

```powershell
gofmt -w internal/config/validate.go internal/config/validate_test.go internal/runtime/vision_fallback_models.go internal/runtime/multimodal_handlers.go internal/runtime/multimodal_handlers_test.go
go test ./internal/config ./internal/runtime -run 'VisionFallback|Multimodal' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit backend candidate and validation behavior**

```powershell
git add internal/runtime/vision_fallback_models.go internal/runtime/multimodal_handlers.go internal/runtime/multimodal_handlers_test.go
git commit -m "fix: use effective vision fallback capabilities"
```

---

### Task 3: Render backend Vision candidates in the OCR settings UI

**Files:**
- Create: `frontend/scripts/check-ocr-fallback.mjs`
- Modify: `frontend/package.json:7-15`
- Modify: `frontend/src/lib/types.ts:315-337`
- Modify: `frontend/src/lib/api.ts:154-166`
- Modify: `frontend/src/components/ocr-fallback-settings.tsx:1-72,260-285`
- Modify: `frontend/src/locales/en.json:519-550`
- Modify: `frontend/src/locales/zh-CN.json:519-550`

**Interfaces:**
- Consumes: `vision_fallback_models` from GET and PUT `/admin/multimodal`.
- Produces `VisionFallbackModelOption`, `MultimodalAdminConfig.vision_fallback_models`, and a selector driven only by backend effective candidates.

- [ ] **Step 1: Write the failing frontend contract test**

Create `check-ocr-fallback.mjs` with these assertions:

```js
assert.match(types, /interface VisionFallbackModelOption/)
assert.match(types, /vision_fallback_models: VisionFallbackModelOption\[\]/)
assert.match(component, /response\.vision_fallback_models/)
assert.doesNotMatch(component, /providerApi\.list/)
assert.doesNotMatch(component, /provider\.model_capabilities/)
assert.match(component, /visionFallbackUnavailable/)
assert.match(english, /"visionFallbackSourceModelsDev"/)
assert.match(chinese, /"visionFallbackSourceModelsDev"/)
```

Add `test:ocr-fallback` to `package.json` and include it in the aggregate `test` script.

- [ ] **Step 2: Run the contract test and verify RED**

```powershell
npm --prefix frontend run test:ocr-fallback
```

Expected: FAIL because the response type and rendering do not exist.

- [ ] **Step 3: Add response types without sending candidates in PUT input**

```ts
export interface VisionFallbackModelOption {
  target: string
  provider_id: string
  model: string
  source: "model_override" | "provider_default" | "models_dev"
}

export interface MultimodalAdminConfig extends MultimodalAdminFields {
  vision_fallback_models: VisionFallbackModelOption[]
}

export interface MultimodalAdminInput extends MultimodalAdminFields {
  api_key?: string
}
```

Keep `multimodalApi.save` accepting input fields and returning the enriched config.

- [ ] **Step 4: Replace frontend-derived candidates with response state**

Remove `providerApi` and `ProviderConfig`. Apply GET and PUT snapshots through:

```ts
const [visionModels, setVisionModels] = useState<VisionFallbackModelOption[]>([])

const applySnapshot = (response: MultimodalAdminConfig) => {
  const { vision_fallback_models, ...fields } = response
  setConfig({ ...defaultConfig, ...fields, api_key: "" })
  setVisionModels(vision_fallback_models ?? [])
}
```

Render options by target and source. Preserve an existing missing target with a `当前不可用` label; once switched away, it disappears and cannot be newly selected.

- [ ] **Step 5: Add bilingual source and unavailable labels**

Add keys for `visionFallbackSourceModelsDev`, `visionFallbackSourceModelOverride`, `visionFallbackSourceProviderDefault`, and `visionFallbackUnavailable`. Chinese values are `models.dev`, `模型人工修正`, `服务商默认设置`, and `当前不可用`.

- [ ] **Step 6: Run frontend tests and verify GREEN**

```powershell
npm --prefix frontend run test:ocr-fallback
npm --prefix frontend run test:i18n
npm --prefix frontend run build
```

Expected: all commands exit 0.

- [ ] **Step 7: Commit the frontend behavior**

```powershell
git add frontend/package.json frontend/scripts/check-ocr-fallback.mjs frontend/src/lib/types.ts frontend/src/lib/api.ts frontend/src/components/ocr-fallback-settings.tsx frontend/src/locales/en.json frontend/src/locales/zh-CN.json
git commit -m "fix: list catalog vision fallback models"
```

---

### Task 4: Reference ADR-0002 and update public contracts

**Files:**
- Modify: `docs/config-schema.md:276-280`
- Modify: `docs/admin-api.md:198-217`
- Modify: `docs/ocr-fallback-design.md:421-431,784-790`

**Interfaces:**
- Consumes: ADR-0002 from PR #13, implemented precedence, API response, and error behavior.
- Produces: updated public contracts that reference the governing decision without duplicating it.

- [ ] **Step 1: Reference the governing ADR**

Add a short architecture-decision reference to the affected documentation:

```markdown
This behavior follows ADR-0002, Model capability resolution precedence,
introduced by documentation PR #13.
```

After PR #13 is merged and this branch is rebased, use a relative link to
`adr/0002-model-capability-resolution-precedence.md`. Until then, use the PR URL
so this branch contains no broken local link.

- [ ] **Step 2: Update schema, Admin API, and OCR design references**

Document the exact precedence, `vision_fallback_models` fields and sources,
catalog unavailable behavior, HTTP 400 save behavior,
`vision_fallback_unverified`, and non-persistence of catalog results.

- [ ] **Step 3: Check documentation consistency**

```powershell
rg -n -S "catalog inference alone is not accepted|must be explicitly marked" docs
rg -n -S "vision_fallback_models|vision_fallback_unverified|model_override.*provider_default.*models_dev" docs
git diff --check
```

Expected: obsolete explicit-only statements have no matches; new API, warning,
and precedence have matches; `git diff --check` exits 0.

- [ ] **Step 4: Commit ADR and contract documentation**

```powershell
git add docs/config-schema.md docs/admin-api.md docs/ocr-fallback-design.md
git commit -m "docs: align vision fallback contracts"
```

---

### Task 5: Run complete regression verification

**Files:**
- Verify only; no planned source changes.

**Interfaces:**
- Consumes all Task 1-4 deliverables.
- Produces fresh evidence that the fix is safe to publish.

- [ ] **Step 1: Run all Go tests without CGO**

```powershell
$env:CGO_ENABLED='0'
go test ./... -count=1
```

Expected: PASS with zero failing packages.

- [ ] **Step 2: Run Go vet**

```powershell
$env:CGO_ENABLED='0'
go vet ./...
```

Expected: exit 0 with no diagnostics.

- [ ] **Step 3: Run all frontend contract tests**

```powershell
npm --prefix frontend test
```

Expected: all scripts pass, including `test:ocr-fallback`.

- [ ] **Step 4: Build the production frontend**

```powershell
npm --prefix frontend run build
```

Expected: TypeScript and Vite exit 0.

- [ ] **Step 5: Verify repository state and review the complete diff**

```powershell
git status --short
git diff --check origin/main...HEAD
git diff --stat origin/main...HEAD
git log --oneline origin/main..HEAD
```

Expected: no unstaged source changes, no whitespace errors, and only planned backend, frontend, test, ADR, and documentation files differ.
