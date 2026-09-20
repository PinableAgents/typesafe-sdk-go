# typesafe-sdk-go 接入 Go Agent 宿主

版本：0.1.0 · 核对日期：2026-09-20

## 一、目标与边界

目标是在 Go 编写的 Agent 宿主里，直接通过 `typesafe-sdk-go` 调用 TypeSafe，获得可被 Go 代码使用的结构化判断。SDK 调用远程 TypeSafe API，不执行 Python SDK，也不是把 Jev 模型转成 Go 本地模型。

建议把它接在独立的“评估/决策服务”接口后面，而不是替换宿主现有的聊天模型 Provider、Agent Harness，或检索、记忆一类的既有模块。下面是建议结构，不是声称已查看你当前工程后得到的实际目录。

```text
用户任务
    ↓
Go 宿主：输入最小化、脱敏、配置和预算检查
    ↓
Agent 应用策略（例如 contrib/agentpolicy）
    ↓
typesafe-sdk-go → TypeSafe API
    ↓
类型/题目/答案合同校验
    ↓
宿主确定性规则 + 原有鉴权/审批/沙箱
    ↓
宿主现有 Harness / 外部 Runtime（Claude Code、Codex 等）
```

**API SDK、业务策略、执行权限必须分开。** `sdk` 不应了解宿主的 session、workflow、审批数据库；策略层可以依赖 SDK 的小接口；真正执行前仍以原有权限和审批结果为准。

本次只创建了独立 SDK 与通用 Agent 示例，没有访问或修改你的任何本地工程。外部 CLI Runtime 不会因为 Go 宿主新增依赖就自动调用这个 SDK。

## 二、交付目录

```text
typesafe-sdk-go/
├── go.mod                         # module github.com/PinableAgents/typesafe-sdk-go
├── client.go                      # 客户端、请求、生命周期
├── types.go                       # 问题、答案和公共类型
├── request.go                     # 请求 JSON 和形状检查
├── response.go                    # 类型化解析、合同校验
├── retry.go                       # 重试与 Retry-After
├── errors.go                      # API/网络/校验错误
├── *_test.go
├── contrib/agentpolicy/            # 可移入 Agent 的业务适配示例
├── examples/basic/                # 混合三类问题
├── examples/models/               # 模型列表
├── examples/agent/                # 输出建议，不执行工具
├── internal/mockapi/              # 固定响应的本地 HTTP mock
└── docs/
```

核心包尽量保持通用。`contrib/agentpolicy` 是示例而非必须依赖；实际集成时可以复制到宿主的 `internal/decision`，然后逐步演进业务规则。

## 三、先本地验证 SDK

需要 **Go 1.23 及以上**工具链：`go.mod` 声明的语言基线是 `go 1.23`，更低版本的工具链会直接拒绝构建。

```bash
cd typesafe-sdk-go
go test ./...
go vet ./...
go test -race ./...
go run ./examples/basic -mock
go run ./examples/agent -mock
```

Mock 只证明程序链路工作，不证明模型准确率。它固定返回测试数据，不能拿输入“删除生产库”去判断 mock 的安全识别能力。

## 四、加入已有 Go module

```text
workspace/
├── your-agent/
└── typesafe-sdk-go/
```

进入已有 Agent 的 go.mod 所在目录：

```bash
go mod edit -require=github.com/PinableAgents/typesafe-sdk-go@v0.0.0
go mod edit -replace=github.com/PinableAgents/typesafe-sdk-go=../typesafe-sdk-go
```

使用真实目录。Windows 下的相对路径同样适用；目录带空格时对整条 `-replace=...` 参数加引号。

添加 import 和实际调用后运行 `go mod tidy`。不需要重新初始化 Agent module，也不应在正式远程依赖中永久依赖你机器的绝对路径。

module 路径 `github.com/PinableAgents/typesafe-sdk-go` 已经是最终路径，不需要再改，仓库已发布 `v0.1.0`，可直接 `go get github.com/PinableAgents/typesafe-sdk-go@v0.1.0`。不要把它和官方地址 `github.com/typesafe-ai/typesafe-sdk-python` 混淆，两者无关。

## 五、启动时初始化一个共享客户端

以下代码可放到你的启动/依赖注入函数中。它不是针对某个具体宿主工程的已确认接口补丁。

```go
client, err := typesafe.NewClient(typesafe.Config{})
if err != nil {
    return err
}
// 在应用退出时调用 client.Close()，不要每一个任务都创建一个客户端。

policyConfig := agentpolicy.DefaultConfig()
router, err := agentpolicy.NewRouter(client, policyConfig)
if err != nil {
    _ = client.Close()
    return err
}
```

客户端读取 `TYPESAFE_API_KEY`、`TYPESAFE_DEFAULT_MODEL`、`TYPESAFE_BASE_URL`。显式 Config 优先。正式 Agent 应从其现有凭据存储读取 Key 并传入 Config.APIKey，不要把它混入共享 prompt、命令输出或普通日志。

同一个 Client 可以被多个 goroutine 使用。Observer 可能并发调用，须使用线程安全的日志/指标接口；不要在 observer 内再次调用同一个 SDK 造成递归观测。

## 六、在任务入口调用，而非替换 Agent Loop

```go
decision, err := router.Evaluate(ctx, agentpolicy.Task{
    Text: taskText,
    Summary: redactedSummary,
})
```

示例会同时评估任务类型、写入意图、任务范围，然后返回 `Decision`。不会读目录、发 shell 命令、修改文件或替你调起另一个模型。

`SuggestedRoute` 当前只有 `explain` 与 `review` 两种结果；`Candidate` 会保留 explain / code_change / test_change / operations / unknown 的模型候选。默认仅明确的只读解释建议绕过“语义复核”，其余统一要求复核。**这不是一个自动选择 Claude 或 Codex 的完成版路由器。** 选择具体 Runtime 属于你的宿主策略。

Decision 中的 `RequiresReview=false` 也不等于授予 shell/文件/网络权限。保持现有工具白名单、工作区限制、审批、沙箱等独立控制。

## 七、第一阶段用影子模式

建议先把返回结果写入任务观测元数据，不改变现有执行路线。下面是集成逻辑说明，不是实际仓库已有 API：

```text
existingRoute = 原有路由决策
advisory, err = TypeSafe 评估
记录 advisory 的版本/模型/判断值/错误类型
继续执行 existingRoute，并保留原来的权限与审批
```

“影子模式”不意味着接口错误时自动批准某个新动作。它的含义是：原来的安全边界从未被替换，新 SDK 目前只提供观测。

推荐观测字段是 request ID、模型实际版本、rubric version、候选类型、建议路线、confidence、写入概率、scope、token 数、耗时、失败原因。默认不保存完整任务文本、源码或服务端错误正文。错误正文可能回显输入；APIError.Error() 因此刻意不直接显示 Message/Body。

当评估错误导致宿主改用原模型继续工作时，也必须经过原来的审批。父 context 被取消时，应终止任务，不应把取消当成普通可忽略的降级。

## 八、明确错误与缺失字段

SDK 支持：

```go
var apiErr *typesafe.APIError
if errors.As(err, &apiErr) {
    switch apiErr.Kind {
    case typesafe.KindAuthentication:
        // 配置问题：报告/禁用本次评估，不在业务层无限重试。
    case typesafe.KindRateLimit, typesafe.KindServer:
        // SDK 已按所选策略尝试重试；回到应用级降级策略。
    }
}
if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
    // 停止当前评估，服从宿主任务生命周期。
}
```

直接使用核心 SDK 时，执行 `result.ValidateFor(request.Questions)` 后再读取各答案。否则 Go map 的缺省零值可能让未返回的 Noul 看起来像 0，造成错误业务判断。

Go 版会检查必须字段、答案类型、概率范围。`ValidateFor` 进一步检查选项与等级集合、概率和、选择最大值以及 Score 加权值。它验证协议一致性，不验证模型对业务事实的判断是否正确。

使用 ExtraBody 重写 `questions` 时，合同校验必须使用最终有效的问题集合。最简单的 Agent 入口不要开放对 core fields 的任意覆盖。

## 九、超时、限流与费用控制

SDK 的 Python 风格默认策略是初次后最多 2 次重试，且连接/超时错误默认重试。模糊失败可能导致服务器处理多次，并非 exactly-once。

提供的 Agent 策略改为：4 秒父 context 硬预算；最多 1 次 HTTP 状态重试；不自动重试连接/HTTP 尝试超时；遵守 Retry-After，超过可用预算时停止而不是提前重发。

阈值 0.85 / 0.20、4 秒预算和 16000 字节输入上限只是示例配置，需依据自己的业务样本、交互目标和服务表现确定。没有保证远端能在这个预算内回答，也没有实测账单。

通过父 context 绑定 Agent Run 的取消信号，任务取消后不继续请求。SDK 默认不提供持久缓存、并发队列、熔断器或计费预算管理；这些应复用宿主机制，而不是声称本版本已有。

## 十、与不同 Harness 的关系

Go 原生 Agent：可以在任务入口调用，也可以在你真正掌握的工具调用前置钩子或交付评估阶段使用 SDK。钩子必须真实存在，不能假定当前工程已经实现。

Claude Code / Codex 等外部 CLI Runtime：Go 宿主可以在启动前和收集结果后调用 SDK。要让外部 Agent 自行请求评估，必须额外设计并暴露其支持的工具协议，例如 MCP、HTTP 服务或 CLI 桥。**本包未实现这些桥接层，也没有修改任何全局 Agent 配置。**

宿主的检索 / 记忆模块：将来可对检索候选或经验候选做语义评分，但不应因为一个 Score 就直接改写长期记忆或替换当前检索链路。本次实现只涉及任务入口的演示评估，不包含这些模块的补丁。

## 十一、从 SDK 到正式产品的验收

先跑本地 mock 和测试，再设置自己的 Key 运行一次真实模型列表与评估。确认请求与响应后，用实际中英文任务形成标注集，记录误判、复核率、延迟和 token。

让新判断只影响低风险、可逆的流程之前，应先证明加入它确实改善了该流程；不要仅凭几个演示例子或较高的 confidence 开启写入自动化。confidence 不是编译器、权限系统或人工审批的替代品。

发布前还需要对照实际 Agent 的 module 路径、生命周期、配置系统、任务上下文、事件协议和日志接口。当前包没有读取你的本地源码，所以这些宿主细节没有被伪装成已完成的集成。

## 十二、源码依据与版本管理

Python 公开源代码/文档的核对清单、支持能力和有意差异都在 `upstream-compatibility.md`。重点包括：可选 instructions、Score 整数等级键、structured legend、usage 可空计数、未来 answer type、extra_body 以及重试预算语义。

建议固定两种版本：SDK 版本和实际采用的模型版本；rubric 也单独版本化。开发可使用 jev-latest，生产应基于账户实际可用模型和评测选择版本，不能把别名视为永远不变。

原始依据：官方 Python SDK 与 TypeSafe API 文档，见 `upstream-compatibility.md` 内实际读取的来源。本文中的宿主架构与默认阈值属于本交付的设计建议，不是 TypeSafe 的官方集成保证。
