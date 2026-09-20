# Python SDK 对照与实现边界

核对日期：2026-09-20。参考仓库：`typesafe-ai/typesafe-sdk-python`；本次读取的 `main` 中 `pyproject.toml` 版本为 `0.7.0`。这里的 `0.7.0` 是源码版本字段，不代表已经确认同名 Git tag / PyPI release。

本次通过网页逐文件核对公开源码和官方文档。环境不能直接克隆仓库，未取得可验证的 Git commit SHA，也没有运行上游 Python 测试套件。因此，这是有接口/行为对照的独立 Go 实现，不是已经证明与某个 commit 完全等价的机械移植。发布前应补记实际选定的 upstream commit，并做实服与跨语言契约测试。

## 对照范围

| Python | Go | 状态 / 说明 |
|---|---|---|
| TypeSafeClient | NewClient(Config) / Client | 已实现 |
| AsyncTypeSafeClient | 共享 Client + goroutine + context | Go 原生并发，不另造 AsyncClient 类 |
| system_one | SystemOne(ctx, SystemOneRequest, ...CallOption) | 已实现 |
| client.models.list | ListModels / Models().List | 已实现 GET /v1/models |
| Choice / Score / Noul | 同名结构体 | 已实现 |
| 字典问题 | RawQuestion | 已实现；允许未来 type |
| JSONContent | any + 编码时形状校验 | 支持文本、对象、数组；嵌套允许普通 JSON 值 |
| 可空 Choice 描述 | map[string]any 中 nil | 保留嵌套 null |
| 可选 instructions | nil 时省略 | 同源码行为，实际业务仍建议明确说明 |
| 可选 Noul criteria | NoulCriteria 为 nil 时省略 | true / false 可显式为 nil |
| response.answers | Answers | 已实现按 type 分派 |
| response.choices / scores / nouls | Choices / Scores / Nouls | 已实现类型化 map |
| Score 整数键 | map[int]float64 / map[int]any | JSON 上保持字符串键，Go 中使用 int |
| Score structured legend | map[int]any | 支持对象/数组，不错误收窄为 string |
| usage 缺失计数 | *int64 | nil 表示未报告，0 表示报告为零 |
| raw_http_response | HTTP 字段 | 保留 body / header / request ID / attempts / duration |
| 未知答案类型 | UnknownAnswers + HTTP.Body | 不进入已知类型 map；可显式检查 |
| RetryPolicy | RetryPolicy / WithRetry | 核心默认值和退避语义对照实现 |
| timeout 参数 | Config.Timeout / WithTimeout / context | Go 完整 HTTP 尝试超时，不等同 Python 各 I/O 阶段超时 |
| extra_headers | WithHeaders | 已实现；鉴权和 SDK 标识等受保护 |
| extra_body | SystemOneRequest.ExtraBody | 顶层浅合并，重名字段后写覆盖 |
| response_model | SystemOneInto + 可选 Validate() | 支持自定义目标；不是 Pydantic 的完整等价物 |
| 异常子类 | APIError.Kind + errors.As / errors.Is | Go 风格，不逐一建立 Python 异常继承树 |
| 日志系统 | Observer | 只记录元数据；刻意不复制正文 debug 日志 |

## 需要明确的差异

**输入检查。** Go 版会在本地检查 State 和已知问题字段的 JSON 形状。Python 原始字典分支部分验证更宽松。Go 不硬编码当前文档的 Choice 最大 255 / Score 最大 10 限制，避免把可变服务限制固定到客户端；服务器仍可返回 422。一般业务应该使用较小的选项集。

**Score 最小级数。** Python 源码只拒绝空列表，生成 schema 的 min_length 也是 1，而 HTTP 说明建议至少两个等级。Go 版允许一个等级以靠近 SDK 行为；示例使用三个有意义的等级。不能据此保证实服接受所有单级问题。

**响应校验。** 除必须字段和类型，Go 版还检查概率范围、非负 token 计数和 Score 等级键的规范形式。Python 对某些数值更宽松。额外的 `ValidateFor` 验证题目/答案匹配、选项集合、概率和及 Score 加权值，属于 Go 版新增的应用保护，不是上游解码器的原样行为。

**Score 加权容差。** 实测远端（`jev-1.13.0`）返回的 probabilities 按两位小数舍入，而 `score` 是连续估计，二者独立给出，加权期望与 `score` 常有约 0.01 的偏差。`ValidateFor` 因此按等级数放大容差：`0.005 * Σ等级索引`（3 等级为 0.015，2 等级为 0.005），下限 1e-3。修复前用的绝对 1e-3 会在约 80% 的真实评估上误判响应损坏。这不放宽对概率和（仍 1e-3）或选项集合的检查。

**未来类型。** 默认解析保留未知答案，不导致整批失败。需要作业务决定时必须显式校验所需问题。RawQuestion 允许未来请求类型，但不能保证未知协议服务端可接受。

**自定义响应。** `SystemOneInto` 接收非 nil 指针，按普通 Go JSON 解码；结构体零值不代表服务端真实返回。可在目标类型实现 `Validate() error`。没有实现 Pydantic 模型继承后将 answers 子字段自动提升到顶层的机制；使用显式嵌套 `Answers` 结构体替代。

**错误。** Go 的 APIError 使用 Kind 区分 400/401/403/404/422/429/5xx。Message、Body、Header 可显式查看，但 Error() 不输出服务端正文，减少误日志泄露。TimeoutError / ConnectionError 保留 Unwrap；调用者 context 取消优先。

**超时。** SDK 默认每次尝试 10 秒；Python 的 HTTP timeout 是 I/O 操作超时配置，两者不逐阶段等价。RetryPolicy.TotalTimeout=30s 对应“继续重试的预算”，不强行终止已进行的尝试。Agent 的硬性总时间限制通过父 context 实现。

**重试。** 默认状态集合和连接/超时行为对齐；零值 RetryPolicy 表示不重试，修改默认项应先调用 DefaultRetryPolicy。Python 的 exceptions 集合用 Go 的 Predicate + errors.As 表达。纳秒/毫秒舍入和抖动随机序列不保证逐次完全相同。

**HTTP 生命周期与安全。** Go 不接管借用的 http.Client / Transport 的关闭；只浅拷贝 client 配置，不变更原对象。SDK 统一拒绝重定向，以免带 Key 的请求被转发。HTTP 明文要显式启用。BaseURL 必须为 root，不接受末尾 /v1。

**可变性。** Python 的回答对象标记 frozen，Go 返回普通可变值。客户端可并发共享；应用不得在正在编码时并发改写请求 map / slice，不得无同步地修改并发使用的回调和 transport。响应结构由调用方管理。

## 原始来源

以下都是本次实际读取过的来源；文档和源码出现差异时，前文明确记录，没有将任一页面视为未经验证的实服事实。

- https://github.com/typesafe-ai/typesafe-sdk-python
- https://raw.githubusercontent.com/typesafe-ai/typesafe-sdk-python/main/pyproject.toml
- https://raw.githubusercontent.com/typesafe-ai/typesafe-sdk-python/main/src/typesafe_sdk/_core/client/sync/client.py
- https://raw.githubusercontent.com/typesafe-ai/typesafe-sdk-python/main/src/typesafe_sdk/_core/question_types.py
- https://raw.githubusercontent.com/typesafe-ai/typesafe-sdk-python/main/src/typesafe_sdk/_core/response_types.py
- https://raw.githubusercontent.com/typesafe-ai/typesafe-sdk-python/main/src/typesafe_sdk/_schemas/models.py
- https://raw.githubusercontent.com/typesafe-ai/typesafe-sdk-python/main/src/typesafe_sdk/_core/questions.py
- https://raw.githubusercontent.com/typesafe-ai/typesafe-sdk-python/main/src/typesafe_sdk/_core/retry.py
- https://raw.githubusercontent.com/typesafe-ai/typesafe-sdk-python/main/src/typesafe_sdk/_core/errors.py
- https://raw.githubusercontent.com/typesafe-ai/typesafe-sdk-python/main/src/typesafe_sdk/_core/transport.py
- https://raw.githubusercontent.com/typesafe-ai/typesafe-sdk-python/main/src/typesafe_sdk/_core/endpoints.py
- https://raw.githubusercontent.com/typesafe-ai/typesafe-sdk-python/main/src/typesafe_sdk/_core/config.py
- https://raw.githubusercontent.com/typesafe-ai/typesafe-sdk-python/main/src/typesafe_sdk/_core/constants.py
- https://raw.githubusercontent.com/typesafe-ai/typesafe-sdk-python/main/src/typesafe_sdk/constants.py
- https://raw.githubusercontent.com/typesafe-ai/typesafe-sdk-python/main/LICENSE
- https://docs.typesafe.ai/api
- https://docs.typesafe.ai/sdk/python/api/clients/sync
- https://docs.typesafe.ai/sdk/python/api/retries

## 后续发布验收，不属于已完成项

选定并记录上游 commit；在同一份样本上对照 Python/Go 请求和响应；用自己的真实 Key 运行 live test；在实际 Agent 工程中做编译和运行验收；用中文/英文真实业务样本标注并校准阈值；选定维护者并确定后续版本节奏（`v0.1.0` 已发布并推送）。

本包不包含 MCP server、HTTP 决策代理、自动计费管理、熔断器、持久化缓存、任务编排引擎，也不包括对任何宿主工程的实际补丁。这些应按真实宿主接口另行集成，不能宣称安装 SDK 就已获得。
