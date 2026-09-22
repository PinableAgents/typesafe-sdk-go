# typesafe-sdk-go

**独立、非官方的 TypeSafe Go SDK。** 基于官方 HTTP 协议用 Go 实现，不依赖 Python 运行，不包含 Jev 模型，也不属于 TypeSafe 官方认证 SDK。

当前对齐基线：**2026-09-22 官方文档 + Python SDK v0.7.1**，固定提交 `0ffd094c72ed9445223060b24ffd7a56aa781fb4`。Go 语言基线仍为 **1.23**，运行仅使用标准库。

已发布版本仍为 `v0.2.0`；本轮对齐、覆盖率和样例改动在开发分支/PR，未自动发布新版本。旧 tag 不会包含本轮新增内容。试用请检出对应 PR 或固定其提交。

## 能力

核心包 `typesafe` 提供 `SystemOne`、`ListModels` / `Models().List()`，Choice / Score / Noul、结构化 state/instructions/criteria、RawQuestion、ExtraBody、类型化响应、可空 usage、原始 HTTP 元数据、自定义响应、模型/重试/超时/headers 覆盖与并发调用。

`contrib/agentpolicy` 输出任务路由建议；`contrib/agenttool` 提供宿主中立的 JSON 工具；`cmd/typesafe-tool` 是单次 JSON stdin/stdout CLI。它们不执行任务、不授予权限，也不是 MCP server。低置信度、错误和写操作必须继续接受宿主授权/复核。

## 无密钥运行

```bash
go test ./...
go run ./examples/basic --mock
go run ./examples/models --mock
go run ./examples/agent --mock
go run ./cmd/typesafe-tool --list
```

`--mock` 是本地固定数据，不访问模型，不测量准确率或实服延迟。完整可执行样例和独立消费者 `main.go` 见 [中文使用文档](docs/usage.zh-CN.md)。

## 质量验收

```bash
make quality
make examples
```

`make quality` 使用 Python 3 执行开发用门禁，运行 Go vet/build/race 和精确全包语句覆盖率。要求 **100% Go 生产语句覆盖率**，包含 CLI、三个示例和内部 mock，没有文件排除。门禁重新插桩源码核对分母，拒绝删块、漏文件和四舍五入伪 100%。

Go/Python 共用 14 组协议样本，CI 实际安装固定 commit 的官方 Python SDK 比较请求和响应。18 个文档指纹与上游 main 每周检查，变化需要复核。**覆盖率与有限契约样本不能证明所有路径无缺陷、Python 语言机制完全等价或实服测试通过。** 本轮没有进行付费推理。

## 真实使用与默认行为

宿主通过环境注入 `TYPESAFE_API_KEY`；不要将密钥写入源码、日志、公开前端或共享安装包。SDK 不自动加载 `.env`。去掉 `--mock` 会访问远端，可能产生费用。

| 项目 | 行为 |
|---|---|
| API key | 显式值优先；默认空字符串继承环境。APIKeySet=true 可明确拒绝继承；裁剪首尾空白，拒绝内部空白/控制字符/非 ASCII |
| 模型 | 请求 Model → Config.Model → TYPESAFE_DEFAULT_MODEL → 默认 jev-latest；实际可用模型查 ListModels |
| Base URL | Config.BaseURL → TYPESAFE_BASE_URL → 官方 API root；不要追加末尾 /v1 |
| 超时 | 默认单次完整 HTTP 尝试 10 秒；WithTimeout 可覆盖，总时限用 context |
| 重试 | 默认初次之后最多 2 次；零值 RetryPolicy 不重试；重试不保证 exactly-once，断连可能发生在服务已处理之后 |
| Transport | Config.Transport 与 HTTPClient 互斥；前者可由 SDK 关闭空闲连接，后者借用、不接管生命周期 |
| 安全 | 不跟随重定向，响应体默认上限 8 MiB，明文 HTTP 需显式开启，仅建议本地测试 |
| 日志 | Observer 仅提供元数据，不自动记录正文，不读取 TYPESAFE_LOG_LEVEL |

`ValidateFor(request.Questions)` 是可选的更严格业务保护。直接读取缺失的 Go map 键会得到零值，不能把缺失答案当成正常判断。ExtraBody 覆盖题目后必须针对实际发送的题目校验。

## 文档和维护

[官方能力映射与差异](docs/upstream-compatibility.md) · [中文使用样例](docs/usage.zh-CN.md) · [Agent 工具接入](docs/agent-tools.zh-CN.md) · [组件验收](docs/component-validation.zh-CN.md)

本轮证据由 GitHub Actions artifacts 按 commit 保存；`docs/validation-report.md`、旧 coverage/log 是 2026-09-20 历史记录，不是本轮结果。

每周检查在合入默认分支后生效，只读发现变化，不自动接受新基线、合并或发版。管理员需把 `Full coverage (Go 1.23)` 和 `Official Python SDK differential contract` 设为 required checks 才能强制阻止不合格合并；添加工作流不等于已修改仓库保护规则。

依赖请固定已验收 tag 或 commit/伪版本；伪版本绑定不可变提交，浮动的是分支查询结果。实服测试仍显式 opt-in，SKIP/mock 不算 LIVE PASS。
