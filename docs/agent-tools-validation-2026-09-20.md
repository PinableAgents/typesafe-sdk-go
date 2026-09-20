# Agent tools 验证记录 — 2026-09-20

## 版本边界

上游基线：`9808f63c280f4cf09045ee1738c62e1217aa8a11`，`go.mod` 为 Go 1.23。
本地工具链为 `go1.23.2 linux/amd64`，没有使用修改语言基线的 modfile。
因运行容器不能解析 GitHub，测试工作区由历史交付包恢复；既有 Go 文件、模型示例、
fixture 和验证脚本已与连接器返回的 Git blob SHA 核对，再应用本次 Agent 工具变更。
这不是一次 `git clone`，运行器的 git_sha 因此保留为空，不伪造 Git 提交记录。
远端 PR 的 checkout/CI 应以其实际检出 SHA 单独核对。

## 本地结果

完整命令：`GOTOOLCHAIN=local python3 scripts/validate_typesafe.py --live --cross-build`。

| 检查 | 结果 |
|---|---|
| 单元/组件/独立消费者测试 | 171 个叶子通过，0 失败，2 个显式联网根测试跳过 |
| 竞态检测 | 通过，同样 171 个叶子通过 |
| go vet / go build | 通过 |
| 原有三个 Mock 示例 | 通过，不是模型推理 |
| CLI 工具列表（无 Key） | 通过 |
| 独立 Go 1.23 module | 通过本地 replace 引入公开 SDK 和 agenttool，执行本地 HTTP fixture |
| SDK 核心覆盖率 | 94.7% |
| agentpolicy 覆盖率 | 100.0% |
| agenttool 覆盖率 | 99.0% |
| CLI 包覆盖率 | 76.0% |
| Windows amd64 / macOS arm64 | 交叉编译通过，未做实机运行 |
| 显式真实 API 组件验证 | **失败：模型列表预检无可用 HTTP 响应** |

## 真实调用结果

已在进程环境中提供用户授权的测试 Key；没有把它提交或写进报告。
日志关键内容：

```text
TestTypeSafeComponentsLive/Models_ListModels
ConnectionError: no successful HTTP exchange; check DNS, proxy, TLS, and connectivity
preflight failed; evaluation calls were NOT attempted
live_transport_attempts=1 hard_cap=36
NOT_FULLY_VALIDATED
```

独立 DNS 诊断对 `api.typesafe.ai` 返回 `Temporary failure in name resolution`。
最终这一次套件只尝试了模型列表请求，没有成功收到 HTTP 响应；后续付费评估未执行。
本轮较早也在未加适配层的基线做过一次相同预检，同样失败；不把两次失败当作成功请求。

无法由这些结果判断 Key 有效性、模型权限、真实延迟、token 消耗或模型准确率。
CLI/工具调用协议和错误路径已离线验收，但**真实 TypeSafe 联调仍是未关闭的验收项**。
当前连接器不提供设置 Actions Secret 或 workflow_dispatch 的接口；没有把 Key 写入公开
workflow 输入、代码、Issue、PR 或其他替代位置来绕过该限制，也没有更改仓库 Secret。

## 覆盖的新增边界

三个工具、混合题型、JSON stdin 协议、无 Key 的工具发现、参数限制、未知字段/工具拒绝、
结构化大整数保持精度、响应契约校验、缺失/空响应、调用方取消、错误脱敏、禁止隐式重试、
共享客户端并发、失败保持人工复核，以及独立模块导入。
新增四个场景也已加入显式 live 套件，但本轮均被预检阻塞；Mock 通过不能替代这些 live 结果。
