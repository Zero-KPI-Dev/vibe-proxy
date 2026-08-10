# vibe-proxy 前端重新设计

## 1. 架构概览

```
┌─────────────────────────────────────────────────────────┐
│                   用户浏览器                             │
│  ┌──────────────────────────────────────────────────┐   │
│  │           React SPA (port 5173 dev / embed)       │   │
│  │  ┌─────┐ ┌──────────┐ ┌────────┐ ┌───────────┐  │   │
│  │  │Dashboard│Providers │Playground│Observability│  │   │
│  │  └─────┘ └──────────┘ └────────┘ └───────────┘  │   │
│  └──────────────────────────────────────────────────┘   │
│                     │ fetch('/admin/...')                │
└─────────────────────┼───────────────────────────────────┘
                      │
┌─────────────────────┼───────────────────────────────────┐
│         Go HTTP Server (port 8080)                      │
│  ┌──────────────────────────────────────────────────┐   │
│  │  runtime.Server                                  │   │
│  │  - GET  / → dashboard HTML (or SPA index.html)   │   │
│  │  - GET  /healthz                                 │   │
│  │  - GET  /admin/config/snapshot                   │   │
│  │  - POST /admin/local/configure                   │   │
│  │  - ... (all admin APIs)                          │   │
│  │  - POST /v1/chat/completions (proxy)             │   │
│  │  - POST /anthropic/v1/messages (proxy)           │   │
│  └──────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
```

**生产部署**: Vite 构建产物 → `//go:embed` → Go 二进制在 `/` 路由 serve，单端口部署。

**开发模式**: 两个进程并行，Vite dev server 代理 `/admin/*` 和 `/healthz` 到 Go backend。

---

## 2. 技术栈

| 层 | 技术 | 理由 |
|---|---|---|
| 框架 | React 19 + TypeScript | 生态最大，shadcn/ui 依赖 React |
| 构建 | Vite 6 | 最快构建速度，Go embed 友好 |
| 样式 | Tailwind CSS v3 | 零运行时，内联 utility，与 shadcn/ui 原生集成（v4 稳定后可升级） |
| 组件库 | shadcn/ui 风格手写组件 | 开箱即用的高质量组件（表格/表单/对话框/下拉菜单等），手动封装 Radix primitives <br/>注意：当前未使用 shadcn CLI，组件为手写并直接使用 `@radix-ui/*`，无 `components.json` |
| 路由 | React Router v7 | SPA 路由标准方案 |
| 数据请求 | TanStack Query v5 | 自动缓存/刷新/loading/error 状态管理 |
| 表单 | React Hook Form + Zod | 类型安全的表单验证 |
| 图表 | Recharts | React 原生、轻量、够用 |
| Toast | Sonner | 轻量通知组件 |
| 图标 | Lucide React | 与 shadcn/ui 一致的图标集 |

---

## 3. 项目结构

```
frontend/
├── index.html
├── package.json
├── tsconfig.json
├── vite.config.ts
├── tailwind.config.ts
├── src/
│   ├── main.tsx             # 入口
│   ├── App.tsx              # 路由 + 布局
│   ├── lib/
│   │   ├── api.ts           # fetch 封装 + API client
│   │   ├── types.ts         # TypeScript 类型定义（对应后端所有 struct）
│   │   └── utils.ts         # Tailwind cn() 等工具
│   ├── hooks/
│   │   ├── use-health.ts    # /healthz 轮询
│   │   ├── use-providers.ts # provider CRUD
│   │   ├── use-requests.ts  # 最近请求
│   │   └── use-metrics.ts   # 指标数据
│   ├── layouts/
│   │   └── app-layout.tsx   # 侧边栏 + 顶栏 + 内容区
│   ├── pages/
│   │   ├── dashboard.tsx
│   │   ├── providers.tsx
│   │   ├── provider-new.tsx
│   │   ├── provider-edit.tsx
│   │   ├── playground.tsx
│   │   ├── observability.tsx
│   │   ├── client-keys.tsx
│   │   ├── model-routing.tsx
│   │   ├── configuration.tsx
│   │   └── settings.tsx
│   ├── components/
│   │   ├── ui/              # shadcn/ui 组件（自动生成）
│   │   ├── provider-card.tsx
│   │   ├── provider-form.tsx
│   │   ├── request-table.tsx
│   │   ├── chat-message.tsx
│   │   ├── chat-input.tsx
│   │   ├── model-selector.tsx
│   │   ├── stats-card.tsx
│   │   ├── health-badge.tsx
│   │   ├── metric-chart.tsx
│   │   └── empty-state.tsx
│   └── lib/
│       └── constants.ts
└── public/
    └── favicon.svg
```

---

## 4. 设计系统

### 4.1 色彩

在现有深色主题基础上做系统化延伸：

| Token | 值 | 用途 |
|---|---|---|
| `--bg` | `#07080a` | 页面背景 |
| `--surface` | `#0d0f14` | 卡片/面板背景 |
| `--surface-2` | `#11141b` | 二级表面（表格行、输入框等） |
| `--border` | `#222631` | 边框 |
| `--text` | `#f5f7fb` | 主文字 |
| `--text-muted` | `#8b93a7` | 辅助文字 |
| `--accent` | `#9b8cff` | 紫色主色 |
| `--accent-2` | `#38bdf8` | 青色辅助色 |
| `--success` | `#22c55e` | 成功/健康 |
| `--error` | `#fb7185` | 错误 |
| `--warning` | `#f59e0b` | 警告 |

### 4.2 布局结构

```
┌──────────────────────────────────────────┐
│  ┌────┐                                  │
│  │logo│  vibe-proxy     [● Running]      │ ← Top bar
│  └────┘                health badge      │
├──────────┬───────────────────────────────┤
│          │                               │
│ Dashboard│   内容区域                      │
│ Providers│                               │
│ Playground│                              │
│ Monitoring│                              │
│──────────│                               │
│ Client   │                               │
│   Keys   │                               │
│ Routing  │                               │
│ Config   │                               │
│ Settings │                               │
│          │                               │
└──────────┴───────────────────────────────┘
  侧边栏                   主区域
  (240px)
```

侧边栏导航，顶部显示状态 + 当前页面名。

---

## 5. 页面详细规格

### 5.1 Dashboard（概览首页）

**路由**: `/`

**功能**:
- 运行时健康状态 + 启动时间
- 统计卡片行：总请求数、活跃 Provider 数、模型数、今日 Token 消耗
- Provider 健康状态简要列表（每个 Provider 一个指示器）
- 最近请求一览表（前 10 条）

**数据源**:
- `GET /healthz` — 健康检查（轮询每 10s）
- `GET /admin/config/snapshot` — Provider 列表
- `GET /admin/requests/recent` — 最近请求
- `GET /admin/metrics/summary` — 聚合统计 [新 API]

### 5.2 Providers（Provider 管理）

**路由**: `/providers`

**功能**:
- Provider 列表（表格视图）：ID、类型、Base URL、模型数、状态、操作
- 每行操作：编辑、测试连通性、删除
- "添加 Provider" 按钮 → 进入表单页
- 测试结果以 toast 通知显示

**子路由**: `/providers/new`、`/providers/:id/edit`

**数据源**:
- `GET /admin/config/snapshot` — 获取列表
- `POST /admin/providers/test?id=xxx` — 测试连通性
- `POST /admin/local/configure` — 新增/更新
- `DELETE /admin/providers` [新 API] — 删除
- `PUT /admin/providers` [新 API] — 更新

**表单字段**:
| 字段 | 类型 | 说明 |
|---|---|---|
| Provider ID | text | 唯一标识符 |
| Type | select | openai-compatible / anthropic |
| Base URL | text | 上游地址 |
| API Key Env Var | text | 环境变量名（如 `OPENAI_API_KEY`） |
| Auth Type | select | bearer / api_key_header / none |
| Models | tags input | 该 Provider 提供的模型列表 |
| Max Concurrency | number | 最大并发数（默认 32） |
| Alias | text | 可选，虚拟模型名 |
| Alias Target | text | 可选，alias 指向的目标模型 |
| Default Model | text | 可选，全局默认模型 |

### 5.3 Playground（聊天调试）

**路由**: `/playground`

**UI 布局**:
```
┌─────────────────────────────────────────────┐
│ [模型选择 ▼] [Streaming ☑]  [参数设置 ⚙]    │ ← 控制栏
├─────────────────────────────────────────────┤
│                                             │
│  User: Hello!                               │
│  ──────────────────────────────────         │
│  Assistant: Hello! How can I...             │
│  ──────────────────────────────────         │
│  User: What is vibe-proxy?                  │
│  └─ (streaming output...)                   │
│                                             │
├─────────────────────────────────────────────┤
│ [输入框...                         ] [发送]  │ ← 输入栏
└─────────────────────────────────────────────┘
```

**功能**:
- 模型选择器：从 `/v1/models`（或本地缓存）加载模型列表
- Streaming 开关：开启时实时渲染 token 流
- 参数面板（可折叠）：Temperature、Max Tokens、Top P、Stop Sequences
- 多轮对话消息列表
- 流式输出实时渲染（markdown 代码块等）
- 清空对话、复制消息、查看原始 JSON 等操作
- 对话历史管理（本地存储 / 后端存储）

**数据源**:
- `GET /v1/models` — 获取可用模型
- `POST /v1/chat/completions` — 发送聊天请求（直接通过代理发，不走 admin API）
- `POST /v1/responses` — OpenAI Responses API
- `POST /anthropic/v1/messages` — Anthropic Messages API

Playground 是一个 **真正的客户端**，它通过 vibe-proxy 的 data-plane API 发送请求，和任何其他 LLM 客户端一样，走完整的认证/路由/代理流程。

### 5.4 Observability（可观测性）

**路由**: `/observability`

**功能**:
- **请求量趋势图**：折线图，支持时间范围选择（1h/6h/24h/7d）
- **延迟分布**：TTFT 和 TPOT 的 P50/P95/P99 折线或柱状图
- **Token 消耗**：按模型/Provider 分组的堆叠柱状图
- **错误率**：成功/4xx/5xx 堆叠面积图
- **Provider 健康**：Provider 列表 + 最近测试时间/状态

**数据源**:
- `GET /admin/metrics/history?range=1h` [新 API] — Prometheus 指标快照查询
- 或直接从 Prometheus `/metrics` 端点 scrape

### 5.5 Client Keys（客户端密钥管理）

**路由**: `/client-keys`

**功能**:
- 密钥列表：名称、可点击显示/隐藏的密钥（默认掩码）、复制按钮、状态、权限范围、RPM 限制
- "创建密钥" 对话框 → 生成新密钥（只显示一次）
- 吊销/启用密钥
- 编辑密钥权限（Allowed Models）

**数据源**:
- `GET /admin/client-keys` [新 API]
- `POST /admin/client-keys` [新 API]
- `DELETE /admin/client-keys/:id` [新 API]
- `PUT /admin/client-keys/:id` [新 API]

### 5.6 Model Routing（模型路由）

**路由**: `/routing`

**功能**:
- Alias 映射表：虚拟名 → Provider/模型
- 添加/编辑/删除映射
- Allow Raw 切换
- Default Model 设置
- 路由规则验证工具

**数据源**:
- `GET /admin/config/snapshot` — 当前路由配置
- `POST /admin/aliases` [新 API] — 新增/更新别名
- `DELETE /admin/aliases/:alias` [新 API] — 删除别名

### 5.7 Configuration（配置管理）

**路由**: `/configuration`

**功能**:
- YAML 在线编辑器（带语法高亮）
- 保存并热重载
- 配置验证
- Profile 管理：保存当前配置快照、切换 Profile
- 配置历史查看

**数据源**:
- `GET /admin/config/raw` [新 API] — 获取原始 YAML
- `PUT /admin/config/raw` [新 API] — 保存 YAML
- `POST /admin/config/reload` — 热重载
- `GET /admin/config/validate` — 验证

---

## 6. API 契约

### 6.1 现有 API（保留不变）

| 方法 | 路径 | 请求 | 响应 |
|---|---|---|---|
| GET | `/healthz` | - | `{ok: bool, loaded_at: string}` |
| GET | `/admin/config/snapshot` | - | `{loaded_at, providers: [], model_resolver: {}}` |
| GET | `/admin/config/validate` | - | `{valid: bool, issues: []}` |
| POST | `/admin/local/configure` | `LocalProviderInput` | `{ok: bool, loaded_at, issues}` |
| POST | `/admin/providers/test?id=xxx` | - | `{ok, provider, status, latency_ms, target}` |
| GET | `/admin/requests/recent` | - | `{active: [], recent: []}` |
| POST | `/admin/config/reload` | - | `{reloaded: bool, loaded_at}` |

### 6.2 新增 API（前后端一起实现）

#### Provider CRUD 补充

```
DELETE /admin/providers
  Request:  { id: string }
  Response: { ok: bool }

PUT /admin/providers
  Request:  LocalProviderInput (same as POST)
  Response: { ok: bool, loaded_at, issues }
```

#### Client Keys CRUD

```
GET /admin/client-keys
  Response: { keys: [{ name, key_prefix, key_hash, enabled, allowed_models, rpm, created_at }] }

POST /admin/client-keys
  Request:  { name, allowed_models?: string[], rpm?: number }
  Response: { key: { name, key_prefix, enabled, allowed_models, rpm }, raw_key: string }

PUT /admin/client-keys/:name
  Request:  { enabled?: bool, allowed_models?: string[], rpm?: number }
  Response: { ok: bool }

DELETE /admin/client-keys/:name
  Response: { ok: bool }
```

#### Aliases CRUD

```
GET /admin/aliases
  Response: { default_model: string, allow_raw: bool, aliases: { [alias]: string } }

POST /admin/aliases
  Request:  { alias: string, target: string }  // target = "provider/model"
  Response: { ok: bool }

DELETE /admin/aliases/:alias
  Response: { ok: bool }

PUT /admin/aliases/default
  Request:  { default_model: string, allow_raw: bool }
  Response: { ok: bool }
```

#### Metrics / Observability

```
GET /admin/metrics/summary
  Response: {
    total_requests: number,
    active_providers: number,
    total_models: number,
    today_tokens: { prompt, completion, total }
  }

GET /admin/metrics/history?range=1h|6h|24h|7d
  Response: {
    range: string,
    points: [{ timestamp, requests, errors, ttft_p50, ttft_p95, ttft_p99, tpot_p50, tpot_p95, tokens_prompt, tokens_completion }]
  }

GET /admin/providers/health
  Response: {
    providers: [{ id, type, base_url, last_tested, healthy, latency_ms }]
  }
```

#### Config

```
GET /admin/config/raw
  Response: { yaml: string }

PUT /admin/config/raw
  Request:  { yaml: string }
  Response: { ok: bool, issues: [], loaded_at }
```

---

## 7. 后端变更清单

| # | 文件 | 变更 |
|---|---|---|
| 1 | `internal/runtime/server.go` | 新增 `adminClientKeys`、`adminAliases`、`adminMetrics`、`adminRawConfig`、`adminProviderDelete`、`adminProviderUpdate` 等 handler |
| 2 | `internal/runtime/server.go` | Routes() 注册新端点 |
| 3 | `internal/config/local_update.go` | 新增 `DeleteProvider`、`UpdateProvider`、`UpsertAlias`、`DeleteAlias` 等函数 |
| 4 | `internal/config/client_keys.go` | 新增文件：ClientKey CRUD （读写 YAML） |
| 5 | `internal/config/config.go` | 新增 `RawConfig()` / `SaveRawConfig()` |
| 6 | `internal/metrics/prom.go` | 新增 `Snapshot()` 方法，暴露当前累计指标 |
| 7 | `internal/runtime/server.go` | dashboard() 改为 serve 嵌入的 SPA 静态文件 |

---

## 8. 前端 Embed 策略

Vite 构建后所有文件输出到 `frontend/dist/`：

```
frontend/dist/
├── index.html
├── assets/
│   ├── index-abc123.js
│   └── index-xyz789.css
```

Go 侧通过 `//go:embed` 嵌入：

```go
// internal/runtime/embed.go
package runtime

import (
    "embed"
    "io/fs"
    "net/http"
)

//go:embed all:frontend/dist
var spaFiles embed.FS

func spaHandler() http.Handler {
    sub, _ := fs.Sub(spaFiles, "frontend/dist")
    return http.FileServer(http.FS(sub))
}
```

在 `Routes()` 中将根路由指向 SPA handler，需要支持 SPA fallback（所有非 API 路径返回 `index.html`）：

```go
mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
    // 如果是 API 路径，不在此处理
    if strings.HasPrefix(r.URL.Path, "/admin/") ||
       strings.HasPrefix(r.URL.Path, "/v1/") ||
       strings.HasPrefix(r.URL.Path, "/healthz") ||
       strings.HasPrefix(r.URL.Path, "/metrics") {
        http.NotFound(w, r)
        return
    }
    // SPA fallback
    // serve index.html or static file
})
```

**go:generate**: 可在 Go 中配置 `//go:generate npm run build` 自动化构建流程。

---

## 9. 各阶段执行计划

### Phase 1: 基础架构 + Provider CRUD + Dashboard

1. 初始化 frontend/ 项目（Vite + React + TS + Tailwind + shadcn/ui）
2. 实现 App Layout（侧边栏 + 顶栏 + 路由框架）
3. 实现 Dashboard 页面（健康状态 + 统计 + 最近请求）
4. 实现 Providers 列表页
5. 实现 Provider 表单（新增/编辑）
6. 实现 Provider 删除
7. 后端补充 `DELETE /admin/providers` 和 `PUT /admin/providers`

### Phase 2: Playground

1. 实现模型选择器
2. 实现聊天消息组件（流式渲染）
3. 实现参数面板
4. 实现对话历史管理（本地存储）
5. 接入 data-plane API 发送请求

### Phase 3: Client Keys + Model Routing

1. 实现 Client Keys 列表、创建、吊销
2. 实现 Model Routing 别名管理
3. 后端补充 Client Key 和 Alias CRUD API

### Phase 4: Observability Charts

1. 实现时间范围选择器
2. 实现请求量趋势图
3. 实现延迟分布图
4. 实现 Token 消耗图
5. 实现错误率图
6. 后端补充 Metrics 聚合 API

### Phase 5: Configuration + Settings

1. 实现 YAML 在线编辑器（CodeMirror/Monaco）
2. 实现保存 + 热重载
3. 实现 Profile 管理
4. 后端补充 Raw Config API
5. 实现 Embed 集成

---

## 10. 类型定义（TypeScript → Go 对应）

完整 TypeScript 类型定义对应所有 Go struct，确保前后端类型一致。详见 `frontend/src/lib/types.ts`（在实现阶段生成）。
