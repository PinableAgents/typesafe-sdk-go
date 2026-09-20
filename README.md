# typesafe-sdk-go

**独立、非官方的 TypeSafe Go SDK。** 本版以 2026-09-20 读取的官方 Python SDK `main`（`pyproject.toml` 标示 `0.7.0`）的公开接口和关键实现为参考，以 Go 重写；不依赖 Python，不包含或本地运行 Jev 模型。

当前交付版本：`0.1.0`。module 路径为 `github.com/PinableAgents/typesafe-sdk-go`，与该仓库地址一致，源码已推送并打上 `v0.1.0` tag，可直接作为远程依赖引用。本 SDK 不是 TypeSafe 官方认证的 SDK。

## 包含什么

核心包 `typesafe` 提供 `SystemOne`、`ListModels` / `Models().List()`，Choice / Score / Noul，结构化 instructions/criteria，RawQuestion，类型化响应、整数 Score 等级键、可空 usage，模型/请求/客户端配置覆盖，重试、取消、错误分类、请求 ID、原始响应、自定义响应与 metadata-only Observer。

`contrib/agentpolicy` 是独立的 Agent 应用策略示例：任务意图、写入意图和范围评估，严格校验后输出建议；网络异常、字段缺失或不确定结果都转为复核。它不执行工具，不授予权限，不调用你的 Agent Runtime。

只有 Go 标准库依赖。`go.mod` 使用 Go 1.23 语言基线，**需要 Go 1.23 及以上工具链**才能构建；实际验证工具链及各项结果见 `docs/validation-report.md`。生产部署请使用组织支持的 Go 工具链。

## 1. 无 Key 跑通

```bash
cd typesafe-sdk-go
go test ./...
go vet ./...
go run ./examples/basic -mock
go run ./examples/models -mock
go run ./examples/agent -mock
```

`-mock` 是本地 HTTP 服务返回的固定测试数据，不访问 TypeSafe，不测量模型准确率。Agent mock 对任何输入都返回同一份只读分类；它只用于验证程序连通性，绝不能作为安全策略。

## 2. 调用真实 API

macOS / Linux：

```bash
export TYPESAFE_API_KEY='你的 API Key'
export TYPESAFE_DEFAULT_MODEL='jev-latest'
go run ./examples/basic -text 'Explain Go context cancellation without changing files.'
go run ./examples/agent -text '请解释 context 取消如何传递，不修改文件。'
```

Windows PowerShell：

```powershell
$env:TYPESAFE_API_KEY = '你的 API Key'
$env:TYPESAFE_DEFAULT_MODEL = 'jev-latest'
go run ./examples/basic -text 'Explain Go context cancellation without changing files.'
go run ./examples/agent -text '请解释 context 取消如何传递，不修改文件。'
```

可选 `TYPESAFE_BASE_URL` 默认是 `https://api.typesafe.ai`，**不要加 `/v1`**，SDK 自己追加接口路径。自建网关必须实现 TypeSafe 的协议；仅支持其他聊天协议不够。

`.env.example` 只是配置样例；本 SDK **不会自动加载 `.env`**。Key 不应提交 Git，也不应打包到公开前端或共享桌面安装包。

## 3. 最小调用

以下片段放在返回 `error` 的函数中；完整可执行版本在 `examples/basic/main.go`。

```go
client, err := typesafe.NewClient(typesafe.Config{})
if err != nil { return err }
defer client.Close()

ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
defer cancel()

request := typesafe.SystemOneRequest{
    State: map[string]string{"task": "解释这个函数，不修改代码。"},
    Questions: typesafe.Questions{
        "intent": typesafe.ChoiceLabels(
            "Which activity is requested?", "explain", "change", "unknown",
        ),
        "scope": typesafe.ScoreLevels(
            "How broad is the requested work?", "One local issue.", "One component.", "Several components.",
        ),
        "write": typesafe.Noul{Instructions: "Does the task request persistent changes?"},
    },
}
result, err := client.SystemOne(ctx, request)
if err != nil { return err }
if err := result.ValidateFor(request.Questions); err != nil { return err }
fmt.Println(result.Choices["intent"].Choice)
fmt.Println(result.Scores["scope"].Score)
fmt.Println(result.Nouls["write"].Noul)
```

`ValidateFor` 很重要：直接读取不存在的 Go map 键会得到零值。SDK 为兼容未来答案类型，会保留未知答案而不令整次解析失败；业务层不能因此把缺失答案解释成“没有风险”。

## 4. 在已有 Agent 工程中本地引用

module 路径是 `github.com/PinableAgents/typesafe-sdk-go`，与该 GitHub 仓库地址一致，已发布 `v0.1.0`。只想稳定引用就直接用远程依赖：

```bash
go get github.com/PinableAgents/typesafe-sdk-go@v0.1.0
```

下面这节留给另一种情况：你要改 SDK 源码、或想脱离版本发布节奏跟进最新提交。建议目录如下：

```text
workspace/
├── typesafe-sdk-go/
└── your-agent/
    └── go.mod
```

在已有 Agent 根目录执行，不要重新 `go mod init`：

```bash
go mod edit -require=github.com/PinableAgents/typesafe-sdk-go@v0.0.0
go mod edit -replace=github.com/PinableAgents/typesafe-sdk-go=../typesafe-sdk-go
```

在 Agent 代码中引用：

```go
import (
    typesafe "github.com/PinableAgents/typesafe-sdk-go"
    "github.com/PinableAgents/typesafe-sdk-go/contrib/agentpolicy"
)
```

添加实际调用代码后，再运行：

```bash
go mod tidy
go test ./...
```

不要先执行 `go mod tidy` 再添加引用，否则 Go 可能移除尚未使用的 require。上述本地 require / replace 会让 Go 直接读取本地目录，不会去 GitHub 拉取本 SDK；原 Agent 的其他依赖仍可能需要网络。

require 里的 `@v0.0.0` 只是满足 Go 的语法要求，实际内容始终取自 replace 指向的本地目录，与 SDK 的真实版本号无关。

**不带 tag 拉取会得到伪版本。** 形如 `v0.0.0-20260920061433-98f2c5d654e7` 的版本号直接绑定某个 commit，内容随分支推进而变，不适合作为生产依赖锁定。生产请固定 `@v0.1.0` 这类正式 tag。

## 5. 重要默认行为

| 项目 | 本 SDK 行为 |
|---|---|
| API Key | Config.APIKey 优先，否则读取 TYPESAFE_API_KEY |
| 模型 | 请求 Model → Config.Model → TYPESAFE_DEFAULT_MODEL → jev-latest |
| Base URL | Config.BaseURL → TYPESAFE_BASE_URL → 官方 API root |
| HTTP 超时 | 默认每次尝试 10 秒，包含读取响应体；调用级 WithTimeout 可覆盖 |
| 默认重试 | 初次之后最多 2 次；408、429、5xx，包括 529；连接和超时错误也重试 |
| 退避 | 500ms 起，指数增长到 5s，减去最多 25% 随机抖动 |
| Retry-After | 支持毫秒、秒、小数秒、HTTP 日期，优先毫秒头；不把长等待截短重试 |
| 重试预算 | 默认 30 秒的“是否继续重试”预算，**不是整个调用的硬超时** |
| 硬超时 | 使用 context.WithTimeout；调用方取消不重试 |
| 响应大小 | 默认最多 8 MiB，可通过 MaxResponseBytes 修改 |
| 重定向 | 不跟随，包括调用方 HTTPClient 提供的重定向策略 |
| HTTP 明文 | 需要显式 AllowInsecureHTTP=true；仅建议本地测试使用 |
| 日志 | 无自动正文日志；Observer 仅收到元数据，不读取 TYPESAFE_LOG_LEVEL |
| Close | 阻止新请求；只关闭 SDK 自有客户端的空闲连接，不关闭借用的 Transport |

**重试并不保证 exactly-once。** 超时或断连后，服务器可能已经完成并计费。Agent 示例把连接和超时重试关闭，仅保留有限 HTTP 状态重试。上层不要再无条件套第二层重试。

## 6. 进一步阅读

- `docs/upstream-compatibility.md`：Python 接口映射、已知差异、来源与未实现项。
- `docs/validation-report.md`：测试、竞态、运行和跨平台编译结果。
- `docs/validation.log`：实际验证输出。

本次交付没有用真实 API Key 调用远端；没有测得真实延迟、准确率或账单。显式联网测试命令：

```bash
TYPESAFE_LIVE_TEST=1 go test -count=1 -run '^TestLiveAPI$' -v .
```

PowerShell：

```powershell
$env:TYPESAFE_LIVE_TEST = '1'
go test -count=1 -run '^TestLiveAPI$' -v .
Remove-Item Env:TYPESAFE_LIVE_TEST
```

这会查询模型列表并执行一次真实评估，可能产生费用。普通 `go test` 不做这一步。
