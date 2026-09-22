# TypeSafe Go SDK 使用样例

适用基线：Go 1.23；本轮开发分支/PR，参考官方 Python SDK v0.7.1。旧 `v0.2.0` tag 不包含本轮新增的 Transport、APIKeySet、质量门禁等改动。SDK 运行只依赖 Go 标准库；Python 仅用于可选的开发质量检查和跨语言对照。

## 1. 先跑无密钥样例

```bash
git clone https://github.com/PinableAgents/typesafe-sdk-go.git
cd typesafe-sdk-go
git switch test/docs-parity-100-coverage-20260922
make quality
make examples
```

没有 make（例如 Windows PowerShell）时执行：

```text
go test -race -count=1 -shuffle=on -coverpkg=./... -covermode=atomic -coverprofile=coverage.out ./...
python scripts/check_coverage.py coverage.out
go vet ./...
go build ./...
go run ./examples/basic --mock
go run ./examples/models --mock
go run ./examples/agent --mock
go test -count=1 -run ^Example .
```

`--mock` 完全使用本地固定数据，不需要 Key、不访问模型、不产生推理账单。它不测模型准确率；Agent mock 对任何输入都返回固定只读建议，不能拿它授权真实操作。

| 入口 | 演示内容 |
|---|---|
| examples/basic | 一次请求同时使用 Choice、Score、Noul，输出 JSON |
| examples/models | Models().List() 查询模型元数据 |
| examples/agent | 任务路由、风险/不确定性转复核，不执行任务 |
| ExampleClient_SystemOne | 三原语混合调用和 ValidateFor |
| ExampleClient_SystemOneInto | 嵌套自定义响应与 Validate() |
| ExampleClient_SystemOne_concurrent | 同一客户端的两个并发请求 |

后面三个 `Example...` 在 `example_test.go` 中，有固定 Output 断言，可由 `go test` 执行，不是未编译的伪代码。

## 2. 在其他项目引用

在消费者工程中使用已验收的提交或发布 tag；试用本轮 PR 可以先取该分支，Go 会解析并在 go.mod 中固定对应伪版本：

```bash
go get github.com/PinableAgents/typesafe-sdk-go@test/docs-parity-100-coverage-20260922
```

已有本地 SDK 源码时：

```bash
go mod edit -require=github.com/PinableAgents/typesafe-sdk-go@v0.0.0
go mod edit -replace=github.com/PinableAgents/typesafe-sdk-go=../typesafe-sdk-go
```

添加实际 import 后再执行 `go mod tidy`。不要在已有工程重复 `go mod init`。锁定的伪版本绑定 commit，不会自行变化。

## 3. 真实调用：完整 main.go

由宿主的密钥管理或进程环境注入 `TYPESAFE_API_KEY`，不要写进源码、命令行参数或日志。程序不会自动读取 `.env`。以下调用会访问官方 API，可能产生费用；本轮交付没有把它标记为已完成的实服测试。

```go
package main

import (
    "context"
    "fmt"
    "os"
    "time"

    typesafe "github.com/PinableAgents/typesafe-sdk-go"
)

func main() {
    if err := run(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}

func run() error {
    client, err := typesafe.NewClient(typesafe.Config{
        BaseURL: typesafe.DefaultBaseURL,
        Retry:   &typesafe.RetryPolicy{}, // 首次接入禁用隐式重试，便于核对调用次数。
    })
    if err != nil {
        return err
    }
    defer client.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
    defer cancel()

    request := typesafe.SystemOneRequest{
        State: map[string]string{"task": "解释这个 Go 函数，不修改任何文件。"},
        Questions: typesafe.Questions{
            "intent": typesafe.ChoiceLabels(
                "Which activity is requested?", "explain", "change", "review",
            ),
            "scope": typesafe.ScoreLevels(
                "How broad is the work?", "One function.", "One component.", "Several components.",
            ),
            "write": typesafe.Noul{Instructions: "Does the user request persistent changes?"},
        },
    }
    result, err := client.SystemOne(ctx, request)
    if err != nil {
        return err
    }
    if err := result.ValidateFor(request.Questions); err != nil {
        return err
    }

    intent := result.Choices["intent"]
    fmt.Printf("intent=%s confidence=%.3f\n", intent.Choice, intent.Confidence)
    fmt.Printf("scope=%.3f write_probability=%.3f\n",
        result.Scores["scope"].Score, result.Nouls["write"].Noul)
    // 只展示结果。低置信度、失败和写操作仍交由宿主授权/复核。
    return nil
}
```

也可直接运行仓库现有程序（已通过 mock 和异常路径测试）：

```bash
go run ./examples/models
go run ./examples/basic --text 'Explain this function without editing files.'
go run ./examples/agent --text '解释这个函数，不修改文件。'
```

不要把固定 mock 输出当成以上真实输入的预期推理结果。

## 4. 参数、模型和错误处理

`Config{}` 从环境继承 key、model 和可选 base URL。只连接官方服务的宿主可以像完整例子一样明确 `BaseURL: typesafe.DefaultBaseURL`，避免环境中残留的网关地址。自定义网关必须实现 TypeSafe 协议；BaseURL 是 API root，不加末尾 `/v1`。真实凭据仅通过 HTTPS 发送。

密钥首尾空白被裁剪；内部空白、控制字符、非 ASCII 或空值立即失败。`Config{APIKey: "", APIKeySet: true}` 表示“明确空值”，不会静默继承环境。`Config{}` 则保留环境继承行为。

在上面的 `client` 和 `ctx` 范围内，模型查询为：

```go
models, err := client.Models().List(ctx)
if err != nil { return err }
for _, model := range models.Models {
    fmt.Println(model.Name, model.ReleaseDate)
}
```

模型选择的顺序为 Request.Model → Config.Model → TYPESAFE_DEFAULT_MODEL → SDK 默认别名。实服可用模型以 ListModels 返回为准，不把 fixture-model/mock-model 用于生产请求。

单次调用可以用 `typesafe.WithTimeout(5*time.Second)`、`typesafe.WithRetry(policy)`、`typesafe.WithHeaders(headers)` 覆盖客户端配置。要修改默认重试先取 `typesafe.DefaultRetryPolicy()`；零值 RetryPolicy 表示不重试。context 是总调用时限，WithTimeout 是每次 HTTP 尝试时限。超时或断连可能发生在服务已经完成之后，盲目重试可能重复计费。

用 `errors.As(err, &apiErr)` 识别 `*typesafe.APIError` 并检查 Kind/StatusCode；用 `errors.Is(err, context.Canceled)` 检查取消。不要直接输出 APIError.Body/Header/Message、请求 state 或自定义 transport 错误中的敏感诊断。业务失败必须返回错误/复核，而不是合成正常答案。

## 5. 结构化字段与自定义响应

Choice.Criteria 支持 `map[string]any`，描述可为字符串、对象、数组或 nil。Score.Criteria 是按等级顺序排列的 `[]any`；Noul.Criteria 使用 `typesafe.NoulCriteria{"true": ..., "false": ...}`。可选字段 nil 表示省略，嵌套 null 会保留。RawQuestion 支持额外协议字段；调用方解码动态 JSON 时用 Decoder.UseNumber 避免先转换 float64 丢失大整数精度。

`SystemOneInto(ctx, request, &response)` 使用普通 Go JSON 解码。自定义结构应使用指针区分字段缺失和零值，并实现 `Validate() error`；完整可执行实现见 `ExampleClient_SystemOneInto`。不要假设未返回的 bool/数字零值等于模型的真实回答。

`ExtraBody` 是最后写入的顶层浅合并，能覆盖 state/model/questions，属于可信宿主的高级功能。不要接受不可信工具参数注入 ExtraBody。使用它覆盖 questions 后，业务校验必须针对实际发送的题目，而不是原 request.Questions。

共享 Client 可用于 goroutine；每个请求使用独立的可变 map/slice，并限制并发。传入 HTTPClient 时生命周期归宿主；传入 Config.Transport 时 SDK 可关闭其空闲连接，两者不能同时设置。

## 6. Agent 工具和 JSON CLI

```bash
go run ./cmd/typesafe-tool --list
```

`--list` 不需要 key，也不调用远端。宿主完成密钥配置后，可把下面一个 JSON 对象通过 stdin 交给 `go run ./cmd/typesafe-tool`：

```json
{"tool":"typesafe_evaluate","arguments":{"state":"Explain this code without edits.","questions":{"write":{"type":"noul","instructions":"Does this request persistent changes?"}}}}
```

CLI 是单次 JSON 调用，不是常驻 MCP server。宿主注册可用 `agenttool.Definitions()` 与 `agenttool.New(client)`；可信宿主调整路由阈值可用 `agenttool.NewWithConfig(client, cfg)`。工具参数不能改 API 地址、密钥、headers 或 ExtraBody。

宿主同时检查进程退出码、`ok`、`error.requires_review` 和路由的 `requires_review`。建议不是授权，任何返回都不能绕过审批、沙箱或执行权限。

## 7. 验收与持续对齐

`make quality` 必须得到精确 100% Go 生产语句覆盖率；支持的构建源文件全部进入分母，包括 CLI、三个示例和内部 mock。门禁会拒绝报告删块/漏文件以及四舍五入后的伪 100%。测试通过仍不代表完整路径穷举或模型准确率。

CI 安装固定 commit 的官方 Python SDK，使用 14 组同源 fixture 比较 Go/Python 的请求和响应，不使用真实 Key。每周文档检查只报告变化，不自动接受新基线或合并；维护步骤见 [对齐矩阵](upstream-compatibility.md)。管理员还需将质量检查设为 required，才能强制阻止未通过检查的合并。

实服组件验收仍采用现有的显式授权流程，见 `component-validation.zh-CN.md`。不要把 SKIP、mock 或离线 CI 成功写成 LIVE PASS。
