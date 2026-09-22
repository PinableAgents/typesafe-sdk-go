# 官方文档对齐、证据与维护边界

核对日期：**2026-09-22**。这是独立、非官方的 Go SDK，不包含 Jev 模型，不依赖 Python 运行。

基线为 TypeSafe 官方公开 HTTP/原语/SDK 文档，以及官方 Python SDK **v0.7.1**（2026-09-21），固定提交 **`0ffd094c72ed9445223060b24ffd7a56aa781fb4`**。`docs/parity-lock.json` 保存 18 个文档来源的 SHA-256 与上游 main 提交，共 19 项来源。文档指纹相同只能证明来源未变化，不自动证明行为等价。

## 三个不同的验收口径

**Go 语句覆盖率：** `go test -race -count=1 -shuffle=on -coverpkg=./...` 配合源码分母校验，要求所有当前构建的 Go 生产代码语句被执行；没有排除 CLI、示例或内部 mock。不是分支覆盖、路径穷举或“没有 bug”的证明；Python QA 脚本另有单元测试，不计入 Go 语句覆盖率。

**公开能力对齐：** 下表每个公开 HTTP 能力均有 Go 对应入口和离线测试。另用同一份 `testdata/docs-contract.json`，实际运行 Go SDK 和固定提交的官方 Python SDK，比较 14 组请求与响应。比较器使用 mock HTTP，绝不把模拟数据称为实服成功。有限样本通过不等于两种语言的一切运行时行为完全相同。

**实服验证：** 本轮未使用真实密钥进行付费推理。现有 live suite 保留显式 opt-in，跳过不算通过；服务端模型质量、账单、限额和业务阈值需要独立实服验收。2026-09-20 的旧报告和 coverage 文件是历史证据，不是本轮结果。

## 能力映射与测试入口

| 官方能力 | Go 实现 | 回归证据 |
|---|---|---|
| POST /v1/systemone | Client.SystemOne | client_test.go、TestOfficialSDKCorpus |
| GET /v1/models | ListModels、Models().List | client_test.go、models_empty/models_metadata corpus |
| State 文本、对象、数组 | SystemOneRequest.State | request_test.go、结构化 corpus |
| Choice 标签、描述、概率、confidence | Choice、ChoiceLabels、ChoiceAnswer | request/response tests、choice corpus |
| Score 有序等级、期望分数、legend | Score、ScoreLevels、ScoreAnswer | request/response tests、score corpus |
| Noul 0–1、可选正反标准 | Noul、NoulCriteria、NoulAnswer | request/response tests、noul corpus |
| 同次请求混合多个问题 | Questions | mixed_one_request corpus、ExampleClient_SystemOne |
| 嵌套 JSON/null、大整数 | JSONContent、RawMessage、UseNumber | structured corpus、precision regression |
| 原始问题和扩展字段 | RawQuestion | raw_question_extensions corpus、request_test.go |
| 模型覆盖与环境默认 | Request.Model、Config.Model | client_test.go、model_override corpus |
| API key 优先级和早期校验 | Config.APIKey、APIKeySet | TestDocumentedAPIKeyValidation |
| 客户端/单次请求 headers | Config.Headers、WithHeaders | client_test.go 保护头与快照测试 |
| 自定义 transport/client | Config.Transport / HTTPClient，互斥 | TestTransportOwnershipAndMutualExclusion |
| 自定义响应模型 | SystemOneInto + Validate() | client_test.go、ExampleClient_SystemOneInto |
| extra_body 浅合并后写覆盖 | SystemOneRequest.ExtraBody | shallow_extra_override corpus |
| 分类响应、可空 usage、模型元数据 | Answers/Choices/Scores/Nouls、Usage、ModelMetadata | response_test.go、corpus |
| 未知响应类型前向兼容 | UnknownAnswers、HTTP.Body | unknown_answer_and_fields corpus |
| 原始响应、request ID | HTTPResponse | client_test.go |
| 客户端/请求级重试 | RetryPolicy、WithRetry、Predicate | retry_test.go、client_test.go |
| 超时、取消、重试预算 | Timeout、WithTimeout、context | client/retry tests、deadline regression |
| HTTP/连接/超时/响应错误分类 | errors.As/Is、APIError.Kind | errors/client/Agent adapter tests |
| 异步调用能力 | 同一 Client + goroutine + context | 并发测试、ExampleClient_SystemOne_concurrent |

另外提供 Agent 策略与 JSON 工具适配、单次调用 CLI；它们不是官方 HTTP API 的必要组成部分，也不是 MCP server。`agenttool.NewWithConfig` 只允许可信宿主调整策略，JSON 工具参数仍不能改 key、URL、headers、ExtraBody 或授权规则。

## 本轮实现修复

密钥按照 v0.7.1 规则裁剪首尾空白，拒绝空值、内部空白、控制字符、DEL 和非 ASCII 字符，错误消息不回显密钥。保留既有 `Config{}` 环境继承行为；需要明确传空并拒绝继承时使用 `APIKeySet: true`。

增加可独立注入的 `Config.Transport`，与 `HTTPClient` 互斥。修正 Score 严格业务校验的边界：即使落在舍入容差内，也不能低于 0 或超过最大等级。结构化 legend 单次解码并保留大整数，避免重复解析。CLI 在退出前释放信号注册；构造失败、读写失败和三个示例的真实入口也接受测试。

## 有意保留的 Go/安全差异

**不宣称 Python 内部实现 100% 等价。** Go 使用 goroutine/context，不复制 AsyncTypeSafeClient、Pydantic 类继承、pickle 或 frozen 对象；自定义响应使用嵌套 Answers 字段与 Validate，而不是自动把答案提升到顶层。普通 Go struct 不会自动强制 required 字段，必须显式检查。

Go `Timeout` 是单次完整 HTTP 尝试的时限，不是 Python 的逐 I/O 阶段 timeout。重试预算不是整个调用的硬超时；用父 context 设置总时限。重试不保证 exactly-once，服务可能已经完成并计费。

借入的 `HTTPClient` 由宿主负责生命周期，SDK 不关闭其 transport；明确通过 `Config.Transport` 交给 SDK 的 transport，Close 会调用其 CloseIdleConnections（若实现）。Python 会关闭传入的 client，这一点不同。Go 统一拒绝重定向、限制响应体大小，明文 HTTP 必须显式启用。

Go 保留 metadata-only `Observer`，不实现 `TYPESAFE_LOG_LEVEL` 自动正文日志，避免泄露任务数据。回调和自定义 transport 必须支持并发；应用不得在请求编码期间并发修改其 map/slice。

Go 对已知问题形状、概率范围、非负 token 和规范整数键做更严格检查。`ValidateFor` 是额外的应用保护，不是上游默认解码规则；还验证题目与答案集合、最大选择、概率和与 Score 的一致性。Score 保留既有舍入容差 `max(1e-3, 0.005 * Σ等级索引)`，但本轮没有重新测量远端舍入行为，不沿用旧报告的实服误判比例作为本轮证据。

Score 允许单个非空等级以兼容参考 Python SDK 的输入行为；HTTP 文档的服务端限制仍以真实返回为准。Go 不硬编码易变化的模型容量上限，不承诺所有本地有效输入都会被每个服务端模型接受。原始未知问题类型亦同。

## 保持对齐的流程

`make quality` 运行 vet、build、QA 脚本测试、race 和精确全包覆盖率。门禁按位置合并不同测试二进制的计数，再重新插桩所有 `go list ./...` 源文件核对语句分母；空报告、漏文件、删块、计数不一致和四舍五入伪 100% 都失败。

PR/main 的 `Documentation parity and full coverage` 工作流执行两个独立检查：`Full coverage (Go 1.23)` 和 `Official Python SDK differential contract`，后者还检查来源指纹。`Weekly TypeSafe documentation drift` 每周一 01:17 UTC（新加坡 09:17）只读检查；合入默认分支后才会按计划生效。GitHub 调度可能延后。

检测到文档或上游 main 变化、来源缺失、网络失败时检查失败，不自动修改 lock、不自动合并。维护者需读取差异，补实现/测试/样例，更新固定 Python 提交和经过复核的指纹，然后在 PR 内重新通过质量检查。指纹更新不能替代行为审阅。

工作流提供检查，但**不等于已设置仓库强制合并规则**。仓库管理员应将上述两个检查设为 required；本轮不绕过或改写既有分支保护，不自动发版。

## 原始来源

- https://docs.typesafe.ai/introduction
- https://docs.typesafe.ai/api
- https://docs.typesafe.ai/sdk/python/changelog
- https://docs.typesafe.ai/sdk/python/api/clients/sync
- https://docs.typesafe.ai/sdk/python/usage
- https://github.com/typesafe-ai/typesafe-sdk-python/tree/0ffd094c72ed9445223060b24ffd7a56aa781fb4

全部指纹对应 URL 见 `parity-lock.json`。可执行示例见 [usage.zh-CN.md](usage.zh-CN.md) 和 `example_test.go`。
