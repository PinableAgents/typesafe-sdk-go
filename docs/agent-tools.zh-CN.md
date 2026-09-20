# Go Agent 与外部 Agent 工具接入

## 范围与当前状态

基线为 Go 1.23。新增 `contrib/agenttool` 和 `cmd/typesafe-tool`，核心 SDK 与
`contrib/agentpolicy` 的既有 API 保持不变。没有第三方 Go 依赖，不需要 Python 服务。

这是一套可以由宿主注册的工具适配层，不是自动安装到任意 Agent 的插件，也不是 MCP Server。
2026-09-20 的离线与独立消费者检查已通过；真实请求被执行环境 DNS/连接失败阻塞，
**没有完成真实 API 验收，也没有验证模型识别准确率**。详见 `agent-tools-validation-2026-09-20.md`。

## 三个工具

| 名称 | 参数 | 返回 data |
|---|---|---|
| `typesafe_list_models` | `{}` | 模型列表 |
| `typesafe_evaluate` | `state`、`questions`、可选 `model` | 校验后的 Choice / Score / Noul 答案与 usage |
| `typesafe_route_task` | `task`、可选 `context_summary` | 路由建议、置信指标、复核要求和 rubric 版本 |

`agenttool.Definitions()` 提供工具名称、描述和 JSON Schema。不同宿主采用不同工具格式，
例如有的把 schema 放在 `parameters` 字段；宿主负责映射并将工具调用交给 `Registry.Call`。
普通 Go 业务可以跳过工具注册，直接调用 `Registry.Call`、原生 SDK 或 `agentpolicy.Router`。

## 在另一个 Go 工程中引用

先将依赖固定到包含本次变更的已审核 commit，不要假定未合并的代码已经在 `main`：

```bash
go get github.com/PinableAgents/typesafe-sdk-go@<reviewed-commit>
```

已有工程不要重新运行 `go mod init`。本地并行开发可使用 `go work` 或 `replace`，
生产构建移除本地路径替换并固定远端版本。

以下是完整 Go 调用示例。API Key 从宿主环境读取，不写进源代码：

```go
package main

import (
    "context"
    "encoding/json"
    "log"
    "os"
    "time"

    typesafe "github.com/PinableAgents/typesafe-sdk-go"
    "github.com/PinableAgents/typesafe-sdk-go/contrib/agenttool"
)

func main() {
    client, err := typesafe.NewClient(typesafe.Config{Retry: &typesafe.RetryPolicy{}})
    if err != nil { log.Fatal(err) }
    defer client.Close()
    tools, err := agenttool.New(client)
    if err != nil { log.Fatal(err) }
    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()
    result := tools.Call(ctx, agenttool.RouteTask,
        json.RawMessage(`{"task":"Explain context cancellation without changing files."}`))
    if err := json.NewEncoder(os.Stdout).Encode(result); err != nil { log.Fatal(err) }
    if !result.OK { os.Exit(1) }
}
```

在长期运行的 Agent 中共享 `client` 和 `tools`；进程退出时关闭客户端。
`Registry` 不取得客户端所有权。任务取消信号传入 `ctx`；不要为每次调用新建客户端。

## 外部 Agent：JSON 命令行

在已检出本次变更的 SDK 目录构建：

```bash
go build -o typesafe-tool ./cmd/typesafe-tool
./typesafe-tool --list
```

Windows 构建时使用 `-o typesafe-tool.exe`。`--list` 不需要 Key，也不联网。
运行调用前由宿主设置 `TYPESAFE_API_KEY`，可选 `TYPESAFE_DEFAULT_MODEL`。
CLI 固定官方 API 根地址，忽略 `TYPESAFE_BASE_URL`；自建兼容服务应由可信 Go 宿主配置客户端。

把下面 JSON 保存到 `request.json`，文件中没有密钥：

```json
{
  "tool": "typesafe_evaluate",
  "arguments": {
    "state": {"task": "请只解释这个函数，不修改任何文件。"},
    "questions": {
      "route": {
        "type": "choice",
        "instructions": "Which activity does task request?",
        "criteria": {"explain": "Read-only explanation.", "change": "Modify files.", "unknown": "Unclear."}
      },
      "scope": {
        "type": "score",
        "instructions": "How broad is the requested work?",
        "criteria": ["One function.", "One component.", "Multiple components."]
      },
      "write": {"type": "noul", "instructions": "Does task request persistent changes?"}
    }
  }
}
```

macOS / Linux：

```bash
./typesafe-tool < request.json
```

PowerShell：

```powershell
Get-Content -Raw -Encoding UTF8 request.json | .\typesafe-tool.exe
```

宿主通过 UTF-8 stdin 写入一个 JSON 对象并关闭 stdin，然后解析 stdout 的一个 JSON 对象。
Windows 宿主需保证管道编码为 UTF-8。不要把用户文字拼接成 shell 命令，也不要把 Key 作为参数。
这是每次进程一个请求的协议，不支持 MCP、JSON-RPC 多帧、SSE 或持续会话。
stdout 只有 JSON；退出码 0 表示调用成功，1 表示调用失败，2 表示参数或配置错误。
宿主还应限制整个子进程（包括等待 stdin）的执行时间。

任务路由调用体：

```json
{"tool":"typesafe_route_task","arguments":{"task":"请解释 Go context 取消机制，不执行命令，也不修改文件。"}}
```

## 成功、失败和权限边界

成功结果是 `{"ok":true,"data":...}`。失败结果具有固定错误码和不回显密钥/原始服务端正文的消息：

```json
{"ok":false,"error":{"code":"connection_failed","message":"No usable HTTP response; check DNS, proxy, TLS and connectivity.","retryable":false,"requires_review":true}}
```

路由失败时 `data` 仍保留 `suggested_route: review` 的保守结果；调用方必须检查 `ok`。
API 401/403 是鉴权/权限失败，429/529 可在宿主预算内考虑重试；连接或超时失败不默认标记为可重试，
因为服务端可能已经处理过请求。`typesafe_evaluate` 和模型查询禁用隐式重试，
路由保留既有 agentpolicy 的有限 HTTP 状态重试，不重试模糊的连接/超时错误。

工具调用最长 20 秒，父 context 可以更短。实际单次 HTTP 超时也受客户端配置限制。
参数最多 64 KiB，单次最多 32 个问题；这些是本适配层限制，不是对服务端上限的声明。
路由额外遵守 agentpolicy 的 16000 字节输入限制。CLI 响应上限为 1 MiB。
禁止通过工具参数设置 URL、Header、API Key、Retry 或 ExtraBody；只接受已支持的三种题型。

结构化评估返回前自动执行 `ValidateFor`，缺失答案不能退化成 Go map 零值。
`requires_review:false` 仍不是 shell、写文件、联网、部署或访问私有数据的授权。
实际权限、沙箱、审批和审计始终由宿主执行。

## 验证和真实运行

```bash
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
python3 scripts/validate_typesafe.py --live --cross-build
```

Python 仅用于收集验证证据，不是 SDK 或 CLI 运行依赖。组件套件新增四个 Agent 工具场景，
当前共 29 个共享组件场景，正常成功路径 28 个 HTTP 请求，传输尝试上限 36，最大并发 2。
这替代初版说明中的 25 / 24 / 30 数字；旧报告保留为历史记录。

GitHub Actions 可在不受本地网络限制的 runner 上执行。仓库所有者可在受信任终端运行：

```bash
# 交互输入密钥；不要加带明文 Key 的 --body 参数。
gh secret set TYPESAFE_API_KEY --repo PinableAgents/typesafe-sdk-go
# ref 使用已审核且包含本次变更的分支。
gh workflow run typesafe-validation.yml --repo PinableAgents/typesafe-sdk-go --ref <reviewed-branch> -f live=true
```

使用 GitHub 网页配置同名 Actions Secret 并手动选择 `live=true` 也可以。
仅绿色的 PR 离线 CI 不表示真实测试通过；验收应核对检出 SHA、Go 版本、非跳过的 live 根测试、
模型、用量及脱敏日志。不要用少量样本或覆盖率作安全认证。
