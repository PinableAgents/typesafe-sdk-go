# typesafe-sdk-go

<!-- sdk-version: 0.3.0 -->

**独立、非官方 TypeSafe Go SDK。** Go **1.23**，运行时仅依赖标准库，不包含 Jev 模型，也不需要 Python 服务。

当前版本 **0.3.0**，对应标签 **`v0.3.0`**。历史 `v0.2.1` 标签保持不变。
Python 源码对齐基线：**v0.7.4 / `e70bf99a89f27102247613bcd5d1aa8923a1cda9`**（2026-10-12 复核）。
网站指纹另保留 2026-09-22 历史快照，尚未宣称全面复核新版网站；漂移仍由独立周检告警。

## 本轮改进

Choice 拒绝空选项，Noul 要求 instructions 或至少一个非 null 标准；新 `ValidateQuestions` 使自定义 Agent API 也可在请求前检查。
Score 修复浮点临界值误拒绝。新 `NonFiniteFloat64` 显式提供 Python 的 NaN/Infinity 字符串表示；普通 float64 默认仍严格拒绝非有限值。
`NewPooledTransport` 提供独立的高并发连接池配置；默认 Client 不变。Agent 工具增加安全 metadata（请求 ID、耗时、尝试次数、模型、协议/判定规则版本），不暴露原始 HTTP 内容。

[迁移、并发配置、测试边界](docs/python074-iteration.zh-CN.md) · [完整中文使用](docs/usage.zh-CN.md) · [Agent 接入](docs/agent-tools.zh-CN.md)

## 接入

```sh
go get github.com/PinableAgents/typesafe-sdk-go@v0.3.0
# 需要单次 JSON CLI 时安装：
go install github.com/PinableAgents/typesafe-sdk-go/cmd/typesafe-tool@v0.3.0
```

```go
import typesafe "github.com/PinableAgents/typesafe-sdk-go"

client, err := typesafe.NewClient(typesafe.Config{
    Transport: typesafe.NewPooledTransport(), // 可选；适合突发调用
})
if err != nil { return err }
defer client.Close()

request := typesafe.SystemOneRequest{
    State: "请解释函数，不修改文件。",
    Questions: typesafe.Questions{
        "write": typesafe.Noul{Instructions: "Does the task request changing files?"},
    },
}
response, err := client.SystemOne(ctx, request)
if err != nil { return err }
if err := response.ValidateFor(request.Questions); err != nil { return err }
```

以上是放入应用函数体的片段；`ctx` 和 API Key 由宿主提供。密钥通过 `TYPESAFE_API_KEY` 注入，不提交源码、不写日志、不自动加载 .env。
依赖固定 `v0.3.0` 或通过验收的 commit；旧 `v0.2.1` 不含本轮新增入口。

## 功能与边界

SystemOne、ListModels / Models().List、Choice / Score / Noul、RawQuestion、结构化 state/instructions/criteria、ExtraBody、类型化响应、可空 usage、原始 HTTP 元数据、自定义响应、超时/重试/headers 覆盖和并发共享均保留。

`contrib/agentpolicy` 提供保守路由建议；`contrib/agenttool` 提供三个 JSON 工具；`cmd/typesafe-tool` 是单次 stdin/stdout CLI，**不是 MCP server**，不自动向外部 Agent 注册，不执行任务或授予权限。
错误、低置信度、写操作继续受宿主审批和沙箱约束；缺失答案不能当作 Go map 的正常零值。

| 项目 | 行为 |
|---|---|
| API key | Config 优先；默认空字符串继承环境；APIKeySet=true 明确拒绝继承；裁剪首尾空白，拒绝内部空白/控制字符/非 ASCII |
| 模型 | 请求 Model → Config.Model → TYPESAFE_DEFAULT_MODEL → jev-latest；可用模型查询 ListModels |
| Base URL | Config.BaseURL → TYPESAFE_BASE_URL → 官方根地址；不追加 /v1 |
| 超时 | 默认单次完整 HTTP 尝试 10 秒；总时限使用 context |
| 重试 | 默认最多重试 2 次；零值 RetryPolicy 不重试；断连重试不保证 exactly-once |
| Transport | 与 HTTPClient 互斥；显式 Transport 的空闲连接可由 SDK 关闭；HTTPClient 借用不接管 |
| 安全 | 不跟随重定向；默认响应体上限 8 MiB；HTTP 明文需显式开启，仅用于受控测试 |
| 日志 | Observer 仅元数据；不自动记录请求/响应正文 |

ExtraBody 仍最后浅合并，可覆盖原始问题；ValidateFor 必须接收实际发送的题目。Agent 工具不允许此覆盖通道。

## 测试与维护

```sh
make quality
make examples
go run ./cmd/typesafe-tool --list
```

`make quality` 运行版本元数据检查、vet/build、QA 脚本测试、race 和精确全包 **100% Go 生产语句覆盖率**。包括 CLI/示例/mock，不删文件或覆盖块以制造 100%。
14 组成功协议样本与新增 37 组行为样本对照固定的官方 Python SDK，均使用 mock HTTP；Python 仅是开发/测试依赖。
本地 HTTP/1.1 burst、受信任 TLS HTTP/2、多路复用、排队取消和独立 Go 消费者有测试。测试覆盖不是模型准确率或全路径正确性的证明。

`--mock`、SKIP、离线 CI 都不等于 LIVE PASS。本轮不进行付费推理。历史 `docs/validation-report.md` 和旧日志不代表本次验证。

正确性 CI 从单一 lock 安装固定 Python commit。周检独立跟踪动态网站/上游变化，不自动接受新基线或发版；不会因为新网页改变而悄悄改变固定契约。
版本门禁检查 README/Version，tag 事件检查两者与标签一致。添加 workflow 不等于修改 required checks 或分支保护。

本次授权的 `v0.3.0` 标签由 `tag-v0.3.0.yml` 在 PR #4 合并提交的完整质量和官方 Python 差分检查通过后创建。该入口仅处理此版本，不自动发布其他版本或创建 GitHub Release，也不会移动已有标签。
