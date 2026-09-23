# OpenCode 渠道

OpenCode Zen 和 OpenCode Go 共用“补齐 OpenCode 客户端标识”开关，保存在渠道 `settings` 的 `opencode_client_headers_enabled` 字段。缺省或 `null` 默认开启，`false` 关闭补齐。

开启时仅补齐缺失的身份请求头：

| 请求头 | 缺失时的值 |
| --- | --- |
| `User-Agent` | `opencode/1.18.32` |
| `x-opencode-client` | `cli` |
| `x-opencode-project` | `global` |
| `x-opencode-session` | 生成 OpenCode 格式的 `ses_` 标识 |
| `x-opencode-request` | 生成 OpenCode 格式的 `msg_` 标识 |

生成的会话和请求标识在同一次入口请求的重试中保持不变，不同入口请求各自生成。需要跨轮会话连续性时，调用方应传入稳定的 `x-opencode-session`。

无论开关状态如何，客户端已有的上述请求头都会保留。渠道自定义请求头最后应用，优先级最高。此开关不生成会改变模型行为的系统提示词或工具定义。

官方客户端还会在子会话中发送 `x-parent-session-id`，因此调用方提供时透传，缺失时不编造父会话。`x-session-affinity` 和 `X-Session-Id` 在官方代码中属于非 OpenCode 提供商分支，不是 OpenCode 默认身份头。

Zen 的 `muse-spark-*` 模型使用 `/v1/responses`。Chat Completions 转发会使用现有的 Responses 转换流程，后台测试自动选择 Responses。

## 实测

设置环境变量 `OPENCODE_LIVE_TEST=1` 后运行 `go test ./relay/channel/opencode -run '^TestOpenCodeLiveClientIdentifiers$' -count=1 -v`，可用匿名 `public` 凭据测试 MiMo 与 Muse Spark 免费模型。普通单元测试不会访问上游；可用 `OPENCODE_LIVE_MODEL` 指定一个以 `-free` 结尾的模型。

2026-09-24，在完整补齐上述身份头后，`mimo-v2.5-free` 仍返回 403 `FreeTierError`，`muse-spark-1.3-contributor-free` 返回 403 `RegionError`。这些是当时测试出口的上游结果；补齐请求头不保证免费模型访问成功。

对 MiMo 使用相同请求体、相同会话/请求标识，仅修改 header 的对照结果：

| 请求头组合 | 结果 |
| --- | --- |
| 上表的五个身份头 | 403 `FreeTierError` |
| 五个身份头，UA 追加 `ai-sdk/provider-utils/4.0.23 runtime/bun/1.3.14`，添加 `Accept: */*` | 403 `FreeTierError` |
| 在上一组基础上添加同值的 `X-Session-Id` 和 `x-session-affinity` | 403 `FreeTierError` |

对照中的 SDK 后缀和会话别名未改变实测结果，因此没有将它们作为默认补齐项。请求体未添加系统提示词或工具。以上结果只能证明这些 header 组合没有解决该测试请求，不能据此确定上游内部的全部校验规则。

## 官方源码依据

- [OpenCode v1.18.32 请求头组装](https://github.com/anomalyco/opencode/blob/v1.18.32/packages/opencode/src/session/llm/request.ts)
- [会话及消息 ID 格式](https://github.com/anomalyco/opencode/blob/v1.18.32/packages/schema/src/identifier.ts)
- [SDK 依赖锁定版本](https://github.com/anomalyco/opencode/blob/v1.18.32/bun.lock)
