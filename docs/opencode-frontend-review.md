# opencode 前端重构审阅意见

> 日期：2026-07-06  
> 项目：vibe-proxy  
> 审阅目标：检查当前前端重构设计与实现是否可合入、是否符合 vibe-proxy 的产品方向。

## 总体结论

当前方向是正确的：使用 React SPA 作为控制面，并通过 Go 单端口服务嵌入前端资源，符合 vibe-proxy “local-first、开箱即用、控制面可配置”的目标。

但当前实现还处于 **“漂亮原型 + 后端半接线”** 阶段，暂时不建议直接合入。主要问题不是 UI 页面本身，而是：

1. 后端路由有未实现的 handler，可能导致 Go 编译失败。
2. Go embed 路径设计不正确，当前写法大概率找不到前端构建产物。
3. 前端已经调用了一批新 admin API，但后端 dispatch 和测试还没有完整闭环。
4. 仓库里混入了不应提交的构建产物和依赖目录。
5. 部分设计文档与实际实现不一致。

建议先收敛到一个可合入的最小闭环，再继续扩展 UI 页面。

---

## 值得保留的方向

### 1. React SPA + Go 单端口嵌入

这个方向是对的。用户最终应该只需要打开：

```text
http://127.0.0.1:8080/
```

不应该要求用户理解 Vite dev server、Go backend、两个端口、代理配置等细节。

### 2. 页面规划覆盖了关键产品能力

当前设计文档规划的页面基本覆盖 vibe-proxy 的核心控制面：

- Dashboard
- Providers
- Playground
- Observability
- Client Keys
- Model Routing / Aliases
- Configuration
- Settings

这和 vibe-proxy 当前产品方向一致。

### 3. Client Keys 是关键补齐

Client Keys 页面方向很好。vibe-proxy 的正确使用方式应该是：

- 上游 provider key 存在 vibe-proxy 配置里；
- agent/client 只拿 vibe-proxy 自己发放的 key；
- 用户不应该把 new-api、OpenAI、Anthropic 等上游 key 直接交给各种 agent。

这块应该作为控制面 MVP 的核心能力之一。

### 4. 前端构建已通过

当前 `frontend` 可以完成 production build，说明 UI 代码本身不是纯空壳，已经有一定完成度。

---

## 当前必须修复的问题

## P0：后端 handler 未接完整，可能编译失败

`internal/runtime/server.go` 新增了这些路由：

```go
mux.HandleFunc("/admin/providers", s.adminProviders)
mux.HandleFunc("/admin/client-keys", s.adminClientKeys)
mux.HandleFunc("/admin/client-keys/", s.adminClientKeysByName)
```

但当前没有找到对应方法实现：

```go
func (s *Server) adminProviders(...)
func (s *Server) adminClientKeys(...)
func (s *Server) adminClientKeysByName(...)
```

已有的是更细粒度的函数：

```go
adminProviderDelete
adminProviderUpdate
adminClientKeysList
adminClientKeysCreate
adminClientKeysUpdate
adminClientKeysDelete
```

### 建议修复方式

补三个 dispatch handler：

```go
func (s *Server) adminProviders(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case http.MethodGet:
        s.adminSnapshot(w, r) // 或返回 providers-only 结构
    case http.MethodPost:
        s.adminLocalConfigure(w, r)
    case http.MethodPut:
        s.adminProviderUpdate(w, r)
    case http.MethodDelete:
        s.adminProviderDelete(w, r)
    default:
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
    }
}
```

```go
func (s *Server) adminClientKeys(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case http.MethodGet:
        s.adminClientKeysList(w, r)
    case http.MethodPost:
        s.adminClientKeysCreate(w, r)
    default:
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
    }
}
```

```go
func (s *Server) adminClientKeysByName(w http.ResponseWriter, r *http.Request) {
    switch r.Method {
    case http.MethodPut:
        s.adminClientKeysUpdate(w, r)
    case http.MethodDelete:
        s.adminClientKeysDelete(w, r)
    default:
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
    }
}
```

注意：如果 `/admin/providers` 的 `GET` 直接复用 `adminSnapshot`，前端要确认响应结构是否一致。现在前端的 `providerApi.list()` 调的是 `/admin/config/snapshot`，所以 `/admin/providers GET` 可以暂时不做，避免多一套 API。

---

## P0：Go embed 路径不正确

当前 `internal/runtime/embed.go` 使用：

```go
//go:embed all:frontend/dist
var spaFiles embed.FS
```

但 `go:embed` 的路径是相对当前 package 目录的。这个写法会尝试读取：

```text
internal/runtime/frontend/dist
```

而真实构建产物在：

```text
frontend/dist
```

Go embed 不能直接用 `../..` 跨到项目根目录外部路径，因此当前方案需要调整。

### 建议方案 A：复制构建产物到 runtime 包内

推荐结构：

```text
internal/runtime/web/dist/
```

然后：

```go
//go:embed all:web/dist
var spaFiles embed.FS

const spaPrefix = "web/dist"
```

构建流程：

```text
frontend build -> copy frontend/dist -> internal/runtime/web/dist
```

可以通过 `Makefile` 或脚本统一完成。

### 建议方案 B：保留旧 dashboard fallback

在 SPA build 产物不存在时，后端最好不要直接 panic。可以短期保留旧的 `dashboard.go` 作为 fallback，避免开发环境因为没 build 前端而打不开控制面。

建议行为：

- 如果 embed dist 存在：serve SPA；
- 如果不存在：serve 简化 dashboard 或提示“请先构建前端”。

---

## P0：`/admin/aliases` 前后端行为不一致

前端 `aliasApi.create()` 调用：

```ts
POST /admin/aliases
```

但后端 `adminAliases` 当前只处理 `GET`，创建 alias 的逻辑在独立函数：

```go
adminAliasesCreate
```

但是路由没有 dispatch 到它。

### 建议修复

让 `adminAliases` 支持：

```go
switch r.Method {
case http.MethodGet:
    // list aliases
case http.MethodPost:
    s.adminAliasesCreate(w, r)
default:
    http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
```

并增加测试覆盖。

---

## P1：新增 admin API 必须补测试

这次前端重构引入了多组新的 admin API：

- provider update/delete
- aliases list/create/delete/default
- client keys list/create/update/delete
- raw config get/save
- provider health
- metrics summary/history

这些 API 都会直接修改本地配置文件，属于高风险控制面能力，必须补测试。

### 至少需要覆盖

1. Provider 删除后：
   - provider 从配置消失；
   - 指向该 provider 的 alias 被清理；
   - snapshot 热更新。

2. Provider 更新后：
   - base_url、models、auth profile 正确写入；
   - `auth_type=none` 不要求 api key env；
   - `api_key_header` 能正确保存 header。

3. Alias 创建/删除/default：
   - alias target 格式正确；
   - default_model 正确保存；
   - allow_raw 正确保存。

4. Client Key 创建：
   - 返回 raw key；
   - 配置里只保存 hash，不保存 raw key；
   - 新 key 能通过 data-plane 鉴权。

5. Client Key 更新/删除：
   - enabled 生效；
   - allowed_models 生效；
   - 删除后无法再鉴权。

6. Raw Config：
   - 非法 YAML 返回错误；
   - 合法 YAML 保存后 snapshot 更新。

---

## P1：仓库文件需要整理

当前出现了不适合提交的文件或目录。

### 不应该提交

```text
frontend/node_modules/
frontend/tsconfig.tsbuildinfo
```

通常也不建议直接提交：

```text
frontend/dist/
```

除非项目明确选择“提交构建产物用于 Go embed”。如果采用 `internal/runtime/web/dist` 的 embed 方案，也应明确这个目录是否提交。

### 建议加入 `.gitignore`

```gitignore
frontend/node_modules/
frontend/tsconfig.tsbuildinfo
frontend/dist/
```

如果后续使用复制后的 embed dist，可以再按选择加入或排除：

```gitignore
# 如果不提交 embed 构建产物
internal/runtime/web/dist/
```

或者明确提交它，但需要接受每次前端改动都会产生较大的构建产物 diff。

---

## P1：Provider 表单需要支持完整 auth profile

当前前端表单有 `auth_type`，但还有几个缺口。

### 问题 1：`auth_type=none` 时仍然强制要求 `api_key_env`

当前 zod schema：

```ts
api_key_env: z.string().min(1, "API key env var is required")
```

这会导致无鉴权 provider 无法通过表单创建。

### 问题 2：`api_key_header` 没有 header 输入框

后端 `LocalProviderInput` 已支持 header，但表单没有暴露字段。对于很多 MaaS 或私有部署，header 名可能不是 `x-api-key`。

### 建议

Provider 表单根据 auth type 动态显示字段：

- `bearer`
  - API Key Env Var 必填
- `api_key_header`
  - Header Name 必填，默认 `x-api-key`
  - API Key Env Var 必填
- `none`
  - 不要求 API Key Env Var
- 后续可扩展：custom headers/query params

---

## P1：Provider 模型列表不应只靠手填

当前 `models` 是手动输入逗号分隔字符串。可用，但体验一般。

### 建议

在 Provider 创建/编辑页增加：

```text
[Test & Import Models]
```

行为：

1. 调 `/admin/providers/test` 确认连通；
2. 对 openai-compatible provider 调 `/models`；
3. 自动填充 models；
4. 允许用户手动删减。

这会显著降低配置门槛。

---

## P1：Playground 应优先使用 vibe-proxy client key

Playground 是用户理解 vibe-proxy 的关键入口。它应该清晰表达：

- 当前请求使用的是 vibe-proxy client key；
- 不是上游 provider key；
- 请求会经过模型路由和协议转换。

建议 Playground 顶部显示：

```text
Client Key: [select/input]
Endpoint: /v1/chat/completions | /v1/responses | /anthropic/v1/messages
Model: [select]
```

并支持复制 curl 示例。

---

## P2：设计文档和实现需要对齐

`docs/frontend-redesign.md` 中写的是：

```text
Tailwind CSS v4
```

实际 `frontend/package.json` 使用的是：

```json
"tailwindcss": "^3.4.17"
```

文档中提到 shadcn/ui 的标准配置，但当前没有看到 `components.json`。如果是“shadcn 风格手写组件”，文档应改成真实情况，避免误导后续开发者。

---

## P2：前端包体偏大，需要后续优化

当前 build 提示：

```text
index JS > 1MB before gzip
```

短期可以接受，但后续建议：

- 对 Playground、Observability、Configuration 做路由级 lazy loading；
- 图表库 Recharts 只在 Observability 页面懒加载；
- markdown 渲染只在 Playground 懒加载。

这不是当前合入阻塞项。

---

## 建议的合入门槛

在继续扩展 UI 前，建议先完成以下 checklist：

- [ ] 后端 Go 编译通过。
- [ ] `go test ./...` 通过。
- [ ] `frontend npm run build` 通过。
- [ ] SPA 能在 `http://127.0.0.1:8080/` 打开。
- [ ] `/providers` 页面能新增、编辑、删除、测试 provider。
- [ ] `/client-keys` 页面能创建 key，并用该 key 请求 `/v1/models` 成功。
- [ ] `/routing` 页面能创建 alias，并用 alias 发起一次 chat 请求成功。
- [ ] `.gitignore` 清理 node_modules、tsbuildinfo、临时构建文件。
- [ ] 新增 admin API 有后端测试。

---

## 推荐下一步执行顺序

### Step 1：先修后端编译

优先补齐：

- `adminProviders`
- `adminClientKeys`
- `adminClientKeysByName`
- `adminAliases` POST dispatch
- embed 路径

### Step 2：补测试

先不继续做 UI 视觉优化，先保证控制面 API 可测、可回归。

### Step 3：清理仓库文件

移除或 ignore：

- `frontend/node_modules/`
- `frontend/tsconfig.tsbuildinfo`
- 临时 dist 产物

### Step 4：端到端验证

至少跑通：

1. 打开控制面；
2. 配置 provider；
3. test provider 显示 200；
4. 创建 vibe-proxy client key；
5. 用 client key 请求 `/v1/models`；
6. 用 alias 发一次 chat；
7. 最近请求能在 Observability/Dashboard 里看到。

---

## 产品体验建议

这次 UI 重构不要变成“企业级复杂网关后台”。vibe-proxy 当前最有差异化的是：

```text
本地优先、agent 优先、一个稳定入口、多协议转换、简单配置。
```

所以首页应该优先回答三个问题：

1. 我现在能不能用？
2. 我的 agent 应该填哪个 base URL 和 key？
3. 我的请求最终会走到哪个 provider/model？

建议 Dashboard 放一个醒目的 “Copy Setup” 区域：

```text
OpenAI-compatible Base URL:
http://127.0.0.1:8080/v1

Anthropic Base URL:
http://127.0.0.1:8080/anthropic

Client Key:
sk-...
```

这会比单纯展示统计卡片更符合 vibe-proxy 的 local tool 定位。

---

## 一句话总结

当前前端重构方向值得继续，但必须先把“后端 API 接线、Go embed、测试、仓库文件清理”补齐。完成这些之前，不建议继续扩大页面范围，也不建议直接合入 develop。
