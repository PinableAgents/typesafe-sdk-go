# SDK 组件验证

这套测试验证公开 SDK 接口及 Agent 策略适配层。Mock 是固定协议数据，不是模型推理；真实 API 小样本也不构成准确率或安全认证。

## 本地运行

需要仓库 `go.mod` 声明的 Go 工具链、Python 3.9+，以及支持 `go test -race` 的环境。不要为了通过测试降低仓库的 Go 版本声明。

在仓库根目录执行：

```bash
python3 scripts/validate_typesafe.py
```

Windows 使用 `py -3 scripts/validate_typesafe.py`。竞态检测需要相应的 C 编译器环境；缺失时执行器会失败，不会静默跳过。

默认依次执行单元测试、`go vet`、竞态检测、覆盖率、构建，以及 basic/models/agent 三个 Mock 示例。默认运行不向 TypeSafe 发送请求，但 Go 可能需要下载工具链或依赖。

```bash
# 另外进行交叉编译，不代表目标平台实机运行。
python3 scripts/validate_typesafe.py --cross-build

# 仅运行本地组件场景。
go test -count=1 -v -run '^TestTypeSafeComponentsLocal$' ./validation
```

## 显式真实 API 测试

```bash
python3 scripts/validate_typesafe.py --live
```

执行器先完成离线检查；全部成功后才进入真实 API 测试。Key 优先从 `TYPESAFE_API_KEY` 环境变量读取；交互终端未设置时，使用隐藏输入。非交互环境缺少 Key 会明确失败。`.env` 不会自动加载。不要将 Key 放进源码、命令参数、PR 或日志。

可通过 `TYPESAFE_DEFAULT_MODEL` 选择模型，默认 `jev-latest`。评测生产模型时应设置实际要验证的固定版本。执行器刻意忽略 `TYPESAFE_BASE_URL`，真实测试只允许 HTTPS 请求到 `api.typesafe.ai` 的 `/v1/models` 和 `/v1/systemone`，不会把 Key 转发到自定义主机。

正常路径共 24 次 HTTP 请求，传输尝试硬上限为 30，包含重试；最大并发为 2。模型列表预检失败后停止后续评估。API 测试可能产生费用，重试也可能重复计费。

真实测试只使用合成任务文本。关于删除文件或修改部署的样本只是待分类文本，测试器不执行 Agent 工具、不删除文件，也不上传你的工程代码。

## 组件范围

本地模式和真实模式共用 25 个场景：

| 范围 | 检查内容 |
|---|---|
| 模型接口 | `ListModels`、`Models().List` |
| 问题类型 | Choice、Score、Noul、三类型混合 |
| 输入 | 字符串、对象、数组、预编码 JSON、中文问题 ID 与选项 |
| 复杂结构 | 结构化 instructions/criteria、Noul true/false criteria、已知类型 RawQuestion、ExtraBody |
| 响应与配置 | `ValidateFor`、原始 HTTP 元数据、usage、Observer、自定义响应、请求级模型覆盖、调用级配置 |
| 客户端 | 共享客户端并发、context 取消、Close 幂等和关闭后拒绝调用 |
| Agent | 英文只读、中文只读、修改代码、修改测试、破坏性运维、诱导绕过分类规则 |

修改类与诱导样本必须进入复核；只读样本允许因不确定性进入复核。该测试不声称分类器能识别所有危险输入，也不将 `RequiresReview=false` 当成执行权限。

429、529、连接错误和重试分支由现有单元测试与本地服务覆盖；不会为触发错误而向真实服务发起压力测试。

## GitHub Actions

工作流：`.github/workflows/typesafe-validation.yml`。

PR 自动执行不使用 Secret 的离线检查。只有手动 `workflow_dispatch` 并勾选 `live` 时，才把仓库 Secret `TYPESAFE_API_KEY` 传给真实 API 测试步骤。请只对已审阅、可信的分支开启真实测试。工作流不使用 `pull_request_target`，不自动合并，也不修改仓库 Secret。

工具链从 `go.mod` 读取；三个第三方 Actions 固定到已核对的完整提交 SHA。工作流只声明 `contents: read`，checkout 不持久化 Git 凭证，测试报告作为 Actions artifact 保留 7 天。

## 报告与验收边界

报告默认保存在 `validation-results/<UTC 时间>/`，包括 `manifest.json`、`summary.md`、每一步日志及覆盖率数据。该目录已加入 `.gitignore`。

`manifest.json` 记录实际 Git SHA、工作区是否有修改、仓库声明的 Go 版本、测试文件哈希、步骤退出码与测试数。缺少 Git SHA 会明确记录，不会用手工标签伪装提交版本。

真实测试全部跳过，即使 `go test` 返回 0，也不能被计为真实联调通过。API Key、Authorization 内容在日志落盘前脱敏；API 错误不打印响应正文。仍应在主动分享报告前检查内容。

状态含义：

- `OFFLINE_CHECKS_PASS_LIVE_NOT_RUN`：离线通过，真实 API 未运行。
- `LOCAL_AND_LIVE_CHECKS_PASS`：离线和本次真实样本均通过，不等于模型质量认证。
- `NOT_FULLY_VALIDATED` / `TOOLCHAIN_BLOCKED`：存在未完成或失败项，查看日志处理。

### 本次 PR 的本地核对

基线提交为 `a35f434554bd6a03cce0a8b5c4813fadc37c960e`。执行环境不能通过 DNS 克隆 GitHub，采用 GitHub 连接器读取版本和文件哈希，再逐项核对已有材料：17 个 Go 文件及 JSON fixture 均与基线 Git blob SHA 一致，差异文件已读取仓库版本替换。

本机只有 Go 1.23.2 / Linux amd64，因此使用仓库外的临时 `-modfile` 进行兼容性检查，**没有修改或提交 `go.mod` 的 1.27 声明**。结果为 122 个叶子测试通过、0 失败、2 个联网测试跳过；竞态检测、静态检查、构建和三个 Mock 示例通过。SDK 核心覆盖率为 94.7%，Agent 策略包为 100%。Python 日志脱敏与测试计数也做了本地断言检查。

这些结果不是 Go 1.27 验收，也不包含真实 API 结果。Go 1.27 结果以本 PR 的 GitHub Actions 实际记录为准；真实 API 结果以手动开启后的报告为准。未将旧参考报告或任何 API Key 加入仓库。

### 后续：语言基线下调到 go 1.23

2026-09-20 稍后的改动把 `go.mod` 的语言基线由 `go 1.27` 下调为 `go 1.23`。这不是“为了通过测试降低声明”（见本文开头的原则），而是让停留在 Go 1.23 的组织工具链也能构建：同一套离线检查已在 `go1.23.12 darwin/arm64`、干净工作区、提交 `6d18f3e15cfc62dd253d7fd5c2a8ab071f764003` 上重跑，122 个叶子测试通过、0 失败、2 个联网测试跳过，竞态检测、静态检查、构建、三个 Mock 示例和交叉编译通过，核心覆盖率 94.7%、Agent 策略包 100%，另有一个外部 `go 1.23` module 通过 require + 本地 replace 成功引用 SDK。详见 `docs/validation-report.md` 的「Go 1.23 基线复验」。

上面这段 Go 1.23.2 本机核对仍是当时 PR 的历史记录：当时仓库声明确实是 `go 1.27`，改用临时 `-modfile` 的做法在当时是必要的。
