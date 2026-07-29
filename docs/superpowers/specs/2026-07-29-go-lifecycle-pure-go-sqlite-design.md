# Go 生命周期与纯 Go SQLite 迁移设计

## 背景

vibe-proxy 当前在 `go.mod`、Dockerfile、开发脚本和 GitHub Actions 中使用
Go 1.22，并通过 `github.com/mattn/go-sqlite3` 访问 SQLite。该驱动要求
CGO 和 C 编译器，增加了 Windows 本地开发、跨平台交叉编译和便携版 Release
的复杂度。

本阶段先完成 Go 生命周期升级和 SQLite 驱动迁移，为后续 Windows EXE、
macOS DMG、Linux tar.gz/DEB Release 提供无 CGO 的单文件构建基础。

## 目标

1. 项目最低 Go 版本升级到官方仍支持的 Go 1.25。
2. CI 同时验证 Go 1.25 和 Go 1.26。
3. Release 和容器构建使用当前最新安全补丁版本 Go 1.26.5。
4. 将 SQLite 驱动迁移到 `modernc.org/sqlite`，生产构建不再依赖 CGO。
5. 现有 SQLite 数据库、表结构、可观测性数据和上层 Store API 保持兼容。
6. Windows、macOS 和 Linux 的 amd64/arm64 目标可以直接交叉编译。

## 非目标

- 本阶段不制作 EXE、DMG、DEB 或 GitHub Release。
- 不改变数据库 schema 或数据保留策略。
- 不引入远程数据库或可插拔数据库抽象层。
- 不增加 SQLite 扩展、全文搜索或加密能力。
- 不实现后台服务、托盘程序或自动更新。

## Go 版本策略

### 当前基线

- `go.mod`: `go 1.25.0`
- CI 最低版本：Go 1.25.12
- CI 当前版本：Go 1.26.5
- Docker/Release 工具链：Go 1.26.5

### 长期规则

Go 官方同时维护最近两个大版本。vibe-proxy 的 `go` 指令使用其中较旧的
受支持大版本，CI 覆盖两个受支持大版本，Release 使用最新大版本的最新补丁。

新 Go 大版本发布后：

1. 等待两至四周的生态兼容性观察期。
2. 在独立变更中验证全部测试和目标平台构建。
3. 将最低版本移动到新的较旧受支持大版本。
4. 同步更新 `go.mod`、Dockerfile、CI、开发脚本和文档。

## SQLite 驱动选择

使用：

```text
modernc.org/sqlite v1.55.0
```

该版本的最低 Go 版本为 1.25，与新的项目基线一致。依赖版本由 `go.mod` 和
`go.sum` 固定；不得单独覆盖其 `modernc.org/libc` 版本。

## Store 接口与数据兼容

保持以下上层契约不变：

```go
func Open(path string) (*SQLite, error)
```

`SQLite` 的事件写入、历史请求、指标汇总、指标时间序列和数据保留行为均不变。
数据库继续使用同一个 SQLite 文件，不进行导出或重建。

现有 schema 继续由 `migrate()` 创建和补充：

```text
request_logs
```

迁移后的驱动必须能够直接打开迁移前生成的数据库文件，并继续读写其中的数据。

## 连接设计

新增内部 DSN 构造函数：

```go
func sqliteDSN(path string) (string, error)
func sqliteDSNFromAbsolutePath(absolutePath, goos string) string
```

职责：

1. 将数据库路径转换为绝对路径。
2. 使用 `file:` URI，正确编码空格、`?`、`#` 和平台路径分隔符。
3. Windows 盘符使用空 authority 的 `file:///C:/...`，UNC 路径保留为
   `file:////server/share/...`。
4. 为每个数据库连接设置：
   - `_busy_timeout=5000`
   - `_journal_mode=WAL`

打开数据库时使用：

```go
sql.Open("sqlite", dsn)
```

`Open` 必须在返回前完成连接验证和 schema migration；任何失败都关闭已创建的
`*sql.DB`，避免资源泄漏。

## 兼容性与回归测试

### 驱动行为

- 数据库实际处于 WAL 模式。
- 每个连接的 `busy_timeout` 为 5000ms。
- 带空格、`?` 和 `#` 的数据库路径可以正常创建和重新打开。
- Windows 盘符和 UNC 路径生成 SQLite 可接受的空-authority file URI。

### 旧数据库

提交一个由迁移前 `mattn/go-sqlite3` 生成的小型数据库 fixture。测试必须验证：

- 可以读取既有请求日志。
- transformation JSON 保持可解析。
- 可以向旧数据库继续写入新请求。
- 重新打开后新旧数据均存在。
- 可以恢复异常退出后仅存在于旧 `-wal` 文件中的已提交请求。

### 现有行为

保留并运行已有 Store 测试：

- transformation 持久化。
- MetricsSummary。
- MetricsHistory。
- RecentFinished。

### 无 CGO 构建

以下命令必须通过：

```bash
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 go build ./cmd/vibe-proxy
```

### 目标平台构建

验证以下组合：

```text
windows/amd64
windows/arm64
darwin/amd64
darwin/arm64
linux/amd64
linux/arm64
```

交叉构建只验证编译和链接；实际安装包及目标系统端到端运行留到 Release 阶段。

## Docker 与开发环境

Dockerfile 更新为 Go 1.26.5，并使用 `CGO_ENABLED=0`。删除外部链接和静态 C
链接参数，继续保留：

- `netgo`
- `osusergo`
- `-trimpath`
- `-s -w`
- scratch 运行镜像

开发 Docker 脚本使用 Go 1.26.5。README 和开发文档声明最低 Go 1.25。

## CI

现有测试工作流调整为 Go 1.25.12/1.26.5 精确版本矩阵，并执行：

```bash
go test ./...
go vet ./...
```

增加一个无 CGO 构建验证步骤，确保后续依赖变更不会重新引入 CGO。
增加 Windows + Go 1.26.5 的无 CGO 全量测试任务，验证真实 Windows 路径、
文件锁和 SQLite 运行时行为。

## 风险与回退

### 主要风险

- 驱动对 DSN、时间值或锁行为的处理差异。
- `modernc` 依赖树增加编译时间和二进制大小。
- 旧数据库附带 WAL 文件时的恢复行为差异。
- 新驱动版本与最低 Go 版本不匹配。

### 缓解措施

- 使用真实旧数据库 fixture，而不是只测试 SQL dump。
- 查询 PRAGMA 验证运行时配置。
- 在两个受支持 Go 版本上运行全量测试。
- 在提交前完成六个目标平台的无 CGO 构建。
- 记录迁移前后 release-mode 二进制大小。

### 回退条件

出现以下任一情况则暂不迁移，恢复 `mattn/go-sqlite3` 并采用各平台原生 CI 构建：

- 旧数据库 fixture 无法无损打开或继续写入。
- WAL/超时配置无法稳定保持。
- 全量测试或主要平台构建失败。
- 二进制或构建资源增长达到不可接受程度且无法通过合理调整解决。

## 提交边界

设计与计划先独立提交。实现完成后形成一个原子提交：

```text
refactor: migrate sqlite storage to pure go
```

跨平台 Release 作为后续独立阶段实施。
