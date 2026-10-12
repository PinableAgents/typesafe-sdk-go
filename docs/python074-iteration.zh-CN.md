# Python v0.7.4 对齐与 Agent 接入迭代

实现日期：2026-10-12。Go 语言基线保持 **1.23**，SDK 运行时仅用标准库。
起点为 Go `93f3a3bd1d9252af857593a67f8a51bf354542c2`；Python 固定在
`e70bf99a89f27102247613bcd5d1aa8923a1cda9`（v0.7.4）。开发版本为 `0.3.0-dev`；没有移动历史标签或发布新版本。

## 1. 迁移说明

Choice 的 criteria 不得为空；Noul 至少需要非 null 的 instructions，或者 true/false 中至少一个非 null 描述。
这些是 Python v0.7.3/0.7.4 的请求校验规则，不把空字符串、空对象、空数组一律禁止。
强类型和 RawQuestion 共享检查；新增 `typesafe.ValidateQuestions` 供宿主在网络调用前校验。
Agent JSON 工具在调用自定义 API 实现之前也做同一检查，工具 Schema 使用对应的 oneOf/anyOf 约束。

以前合法的 `Noul{}`、全 null Noul criteria、空 Choice 现在返回本地 ValidationError。
补充有意义的描述，而不是捕获错误后自动放行。原 14 组成功契约的无描述 Noul 样本已迁入负例，成功样本改为有 instructions / 无 criteria。

Score 比较把浮点运算误差和服务舍入预算分开。仅增加机器精度数量级余量，使 0.495/0.505 相对 0.5 的 ±0.005 边界不被误拒绝。
没有降低概率和、等级集合、有限性或合法分数范围检查，也不把更大的真实偏差当作舍入误差。

## 2. 非有限数：显式兼容，不做隐式全对象重写

默认 `float64` 中 NaN/Inf 仍由 encoding/json 拒绝。需要 Python v0.7.4 的字符串表示时，在允许非有限值的字段使用：

```go
state := map[string]any{
    "latency": typesafe.NonFiniteFloat64(math.NaN()),
    "bounds": []typesafe.NonFiniteFloat64{
        typesafe.NonFiniteFloat64(math.Inf(-1)),
        typesafe.NonFiniteFloat64(math.Inf(1)),
    },
}
```

输出对应 `"NaN"`、`"-Infinity"`、`"Infinity"`；有限数仍是 JSON 数字，绝不转换成零。
它可以作为嵌套 map/slice/struct 字段，也可出现在 instructions、criteria 或 ExtraBody 的嵌套数据里。
不是递归扫描任意对象的自动 Python 模式：未显式包装的 NaN/Inf 仍报错。结构体字段类型可声明为 `NonFiniteFloat64`。

因此标准库的 struct tag/omitempty、自定义 Marshaler、循环引用检查、json.Number 大整数和 RawMessage 语义都不变。
非法 RawMessage 不会被修复，概率与分数响应不会获得非有限值兼容。

## 3. 多 Agent 连接复用

已有 NewClient 默认行为保持不变。突发调用可显式选择独立连接池：

```go
transport := typesafe.NewPooledTransport()
// 配置代理、受信任证书等必须在首次请求前完成；不要关闭 TLS 校验。
client, err := typesafe.NewClient(typesafe.Config{Transport: transport})
if err != nil { return err }
defer client.Close()
```

该配置：最多 100 条总空闲连接，每主机最多 100 条空闲/总连接，空闲期限 30 秒，允许 HTTP/2 协商。
不修改全局 http.DefaultTransport，不修改借入 HTTPClient 的关闭语义。
连接数不是 HTTP/2 在途请求数；宿主仍要设置任务并发和总预算。下面的信号量应是宿主长期共享对象，而不是每次调用都创建：

```go
slots := make(chan struct{}, 8) // 初始化时创建，传给所有任务共享。
// 每个调用：先等待名额，取消时不再发请求。
select {
case slots <- struct{}{}:
    defer func() { <-slots }()
case <-ctx.Done():
    return ctx.Err()
}
// client.SystemOne(ctx, req)；用父 context 同时约束排队与执行。
```

测试用本地服务器确认两轮 20 并发 HTTP/1.1：默认第二轮新建 18 条，池配置第二轮新建 0 条。
本地受信任 TLS 的 HTTP/2 测试确认预热后 20 并发复用 1 条连接，取消一个流之后仍可继续调用。
另测 MaxConnsPerHost=1 的排队取消。以上不是实服提速百分比，不衡量 Jev 推理速度。

## 4. Agent 安全元数据

工具 Result 新增可选 `metadata`，原 ok/data/error 保留：

```json
{
  "ok": true,
  "metadata": {
    "request_id": "example-request",
    "duration_ms": 23,
    "attempts": 2,
    "model": "example-model",
    "protocol": "HTTP/2.0"
  }
}
```

路由结果还提供 rubric_version。缺少的值不伪造成实测值；耗时来自 SDK HTTP 元数据，不是端到端 Agent 工作耗时。
网络前失败、上下文取消没有 HTTP 响应时通常没有这些字段。HTTP 错误可保留 request_id，响应校验错误可保留已有 HTTP 元数据。
只选择这些字段，绝不序列化整个 HTTP 对象、Authorization、原始正文、URL 或自定义错误文本。
HTTPResponse 新增 Protocol，便于宿主识别协商协议。

元数据只改善诊断；不表示模型授权写入，也不自动增加 Honey 的重试/补测/规划环节。

## 5. 质量与维护

```sh
make quality
make examples
go test -count=1 -v -run 'TestBurstConnectionReuse|TestPooledHTTP2AndCancellation' .
go test -run '^$' -bench BenchmarkPooledTransportRequests -benchmem .
```

维持精确全包 100% Go 生产语句覆盖门禁；没有排除 CLI、示例、内部 mock 或新文件。
已有 14 组请求/响应契约继续对照，新增共享 37 组行为样本：强类型/原始非法请求、合法边界、非有限数、HTTP 错误/重试、连接/超时/取消。
跨语言对照使用实际安装的固定 Python SDK + MockTransport，不是用 Python 仿写一个 SDK。Go 特有 context、连接池和 ValidateFor 浮点边界另有本地测试。

CI 从 docs/parity-lock.json 读取唯一 Python commit。scripts/check_release.py 核对 README 与 Version；tag 事件还要求 tag 等于 v + Version。
开发版本不是已发布标签。要发布稳定版本，应先更改 Version/README、通过检查，再另行创建标签；本次没有发布授权操作。

固定基线正确性 CI 不再查询变化中的网站。每周 docs-drift.yml 继续独立检查上游；仍不自动更新锁、合并或发版。
**网站 18 项指纹保留 2026-09-22 的真实记录。** 本环境未能读取新版站点全文，不能只复制新哈希来假装完成网页复核。
lock 明确区分 Python 源码审阅日期和历史网站日期；网站变化仍会触发周检告警，不是忽略错误。

本轮不使用先前聊天中的 API Key，不发起付费推理。实服、账单和真实 Agent 任务完成率不在离线通过结论内。

## 原始依据

- Python 固定版本： https://github.com/typesafe-ai/typesafe-sdk-python/tree/e70bf99a89f27102247613bcd5d1aa8923a1cda9
- 具体行为：该提交的 `_core/questions.py`、`_core/json.py`、`_core/config.py`、`_core/transport.py` 与 `docs/changelog.md`。
- Go net/http Transport： https://go.dev/src/net/http/transport.go
