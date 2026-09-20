# Validation report / 验证报告

验证日期：2026-09-20。当前 `go.mod` 的语言基线是 `go 1.23`，**构建需要 Go 1.23 及以上工具链**。

下表是 `go version go1.27.0 darwin/arm64` 上的运行记录。该次运行早于 `validation/` 组件套件的加入，所以测试事件数少于下面的复验。基线在当天稍后下调为 `go 1.23`，同一套离线检查已在 `go1.23.12 darwin/arm64` 上重跑，结果见「Go 1.23 基线复验」。

## 已完成

| 验证 | 结果 | 边界 |
|---|---|---|
| go test ./... | PASS | 28 个顶层测试通过；含子测试在内共 104 个 pass 事件；1 个 live test 主动跳过 |
| go vet ./... | PASS | 没有诊断输出 |
| gofmt -l . | PASS | 没有未格式化文件 |
| go test -race ./... | PASS | darwin/arm64；以 -count=1 运行 |
| SDK 核心语句覆盖率 | 94.8% | github.com/PinableAgents/typesafe-sdk-go，582/614 语句 |
| Agent 策略语句覆盖率 | 100.0% | contrib/agentpolicy，59/59 语句 |
| basic / models / agent 三个 mock 示例 | PASS | 本地 HTTP fixture；没有模型推理 |
| darwin/arm64 build | PASS | 当前环境原生编译 |
| Windows/amd64 build | PASS | GOOS=windows GOARCH=amd64 CGO_ENABLED=0；仅交叉编译 |
| macOS/arm64 build | PASS | GOOS=darwin GOARCH=arm64 CGO_ENABLED=0；仅交叉编译 |
| Linux/amd64 build | PASS | GOOS=linux GOARCH=amd64 CGO_ENABLED=0；仅交叉编译 |
| 独立 Go module 引用 | PASS | 通过 require + 本地 replace 引用 SDK、初始化 Client 和 Router；没有 API 请求 |

测试覆盖了混合题型、可选 instructions、嵌套 null、结构化 Score legend、整数等级键、未来 answer type、缺失字段、非法概率、题目/答案不匹配、模型列表、配置优先级、受保护头、extra_body 覆盖、HTTP 错误分类、Retry-After、退避、重试预算、调用方取消、连接失败、超时、重定向禁止、响应大小限制、并发调用和 Agent 失败关闭。

`examples` 与 `internal/mockapi` 未建立单独语句覆盖率测试，因此 go test -cover 中显示 0%。三个示例是额外通过 go run 实际运行的；没有将这些手工 smoke check 伪装成覆盖率数据。100% 的策略包语句覆盖率也不是业务正确性或模型准确率的证明。

## 未完成 / 不应推断

没有用真实 Key 调用 TypeSafe；live API 测试显式跳过。没有测得模型准确率、真实延迟或实际账单。没有在 Windows 或 Linux 上实际运行程序，只做了交叉编译。没有在真实的 Agent 工程里做联调，也没有发布远程仓库。

未运行上游 Python 套件，未取得可验证的上游 Git SHA，不声称 100% Python 行为一致或某个固定 commit 的认证移植。

## 与上一版报告的差异

上一版报告基于 `go1.23.2 linux/amd64` 与旧 module 路径 `example.com/typesafe-sdk-go`。该次改动只涉及 module 路径、`go.mod` 语言基线（当时提升到 `go 1.27`）和一处示例文件的 import 排序，没有改动核心包语句；测试事件数（104 个 pass + 1 个 skip）与上一版完全一致。

核心包覆盖率由 94.7% 变为 94.8%。同期 `coverage.out` 的覆盖块数量由 530 增至 536，源码未变，差异来自 Go 工具链版本导致的插桩口径变化，不代表测试强度变化。

上一版曾记录“没有使用 Go 1.22 本身执行测试；1.22 是 go.mod 的语言基线”。中间有一次把 `go.mod` 提升到 `go 1.27` 并就在 go1.27.0 上验证的改动；该提升随后被回退为 `go 1.23`，见下一节。

## Go 1.23 基线复验

2026-09-20：`go.mod` 的语言基线由 `go 1.27` 下调为 `go 1.23`，目的是让仍停留在 Go 1.23 的组织工具链也能直接构建。该提交只改 `go.mod` 的语言声明和文档，没有改动任何 Go 源码，因此上表的源码级结论不受影响。

复验在干净工作区上进行，提交为 `6d18f3e15cfc62dd253d7fd5c2a8ab071f764003`，工具链为 `go version go1.23.12 darwin/arm64`，命令为 `python3 scripts/validate_typesafe.py --cross-build`：

| 验证 | 结果 | 边界 |
|---|---|---|
| go test ./... | PASS | 122 个叶子测试通过，0 失败，2 个联网测试主动跳过（`TestLiveAPI`、`TestTypeSafeComponentsLive`） |
| go vet ./... | PASS | 没有诊断输出 |
| go test -race ./... | PASS | darwin/arm64，以 -count=1 运行；叶子测试计数与上一行一致 |
| SDK 核心语句覆盖率 | 94.7% | github.com/PinableAgents/typesafe-sdk-go |
| Agent 策略语句覆盖率 | 100.0% | contrib/agentpolicy |
| basic / models / agent 三个 mock 示例 | PASS | 本地 HTTP fixture；没有模型推理 |
| darwin/arm64 build | PASS | 当前环境原生编译 |
| Windows/amd64、macOS/arm64 交叉编译 | PASS | CGO_ENABLED=0；仅交叉编译 |
| 独立 Go module 引用 | PASS | 一个外部 `go 1.23` module 通过 require + 本地 replace 引用 SDK，初始化 Client 和 Router；没有 API 请求 |
| 验证脚本自身 | PASS | 离线状态 `OFFLINE_CHECKS_PASS_LIVE_NOT_RUN`；工作区无修改；manifest 记录 `declared_go_version=1.23` |

「未完成 / 不应推断」一节的限定同样适用于本次复验：没有用真实 Key 调用 TypeSafe，没有在 Windows / Linux 上实机运行，没有与上游 Python 套件做行为对比。

下调基线是对更旧工具链的兼容性声明，不是对更旧工具链的长期承诺：一旦引入 1.23 之后的语言特性或标准库 API，就需要同步提升该声明。反过来，本次是在真实的 go1.23.12 工具链上跑通全部离线检查后才发现下调可行，不是为了让测试通过而改声明。

本次复验的原始输出保存在仓库外的临时目录，没有加入仓库。仓库中的三份原始材料仍是 go1.27.0 那一次运行的产物：`validation.log`（原始验证输出）、`test-results.jsonl`（逐项 Go 测试事件）、`coverage.out`（语句覆盖明细）。
