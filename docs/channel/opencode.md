# OpenCode 渠道

## 自动更新免费模型列表

OpenCode Zen 的“上游模型管理”中提供“自动更新 OpenCode 免费模型列表”开关，默认关闭，保存于 `settings.opencode_auto_sync_free_models_enabled`。OpenCode Go 不提供此开关。

开启并保存后，渠道参加现有后台巡检，默认每 30 分钟拉取一次模型目录（由 `CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_INTERVAL_MINUTES` 配置）。也可手动检测更新。免费列表来自该渠道配置的地址、密钥、代理及自定义模型列表接口。

- 自动加入新上线的免费对话模型，移除目录中已下线的免费对话模型；按 `-free` 后缀及 `big-pickle` 识别，排除 JEV／SystemOne 等不支持当前流式对话路径的模型。
- 保留其他模型与手动映射，遵守“已忽略模型”的精确匹配及 `regex:` 规则。
- 请求失败、返回空列表或没有符合条件的免费模型时保留当前列表，记录检测失败。
- 免费同步优先于全部模型巡检，避免自动混入付费模型；关闭后停止免费列表自动维护，保留已加入的模型。
- 新建、编辑页的“获取模型列表”跟随当前开关筛选，预览不会修改已保存的开关。模型列表及渠道可用能力一并更新，站内计费仍使用现有定价。

## 客户端标识

OpenCode Zen 和 Go 共用“补齐 OpenCode 客户端标识”开关，保存在渠道 `settings.opencode_client_headers_enabled`。缺省或 `null` 默认开启，显式 `false` 关闭补齐，旧渠道无需迁移。

| 请求头 | 开启时的行为 |
| --- | --- |
| `User-Agent` | 保留以 `opencode/` 开头的值，否则使用 `opencode/1.18.32` |
| `x-opencode-client` | 保留已有值，缺失时使用 `cli` |
| `x-opencode-project` | 保留已有值，缺失时使用 `global` |
| `x-opencode-session` | 保留已有值，缺失时生成 OpenCode 格式的 `ses_` 标识 |
| `x-opencode-request` | 保留已有值，缺失时生成 OpenCode 格式的 `msg_` 标识 |
| `x-parent-session-id` | 仅在客户端提供时透传 |

生成的标识在同一次入口请求的重试中保持不变。需要跨轮会话连续性时，调用方应传入稳定的 `x-opencode-session`。

关闭开关后仅透传已有标识。通配符及正则请求头透传不能把非 OpenCode 的 UA 覆盖回去；显式渠道／运行时自定义请求头最后应用，优先级最高。协议鉴权头沿用各适配器规则。

## 免费对话模型兼容

仅补请求头不足以解决本次 `FreeTierError`。实现参考 [piexian/new-api 的免费请求兼容代码](https://github.com/piexian/new-api/tree/afd0be51b8e70b922cbd6cd1cc94e1c3a43cf55f/relay/channel/opencode)，按 AGPL-3.0 保留来源说明，适配本项目现有转发路径。

Zen 的 Chat Completions／Responses 免费模型（`-free` 后缀及 `big-pickle`）默认执行以下处理：

- 上游始终使用 `stream: true`；后台测试或非流式调用方的响应由网关汇总为 JSON，保留用量。
- 保留原有工具及其参数，只补缺少的 `bash`、`edit`、`glob`、`grep`、`read` 函数声明，不添加系统提示词。
- Chat 请求开启流式用量；没有客户端工具且未指定 `tool_choice` 时设为 `none`。Responses 保留客户端原有选择。
- 模型若调用仅为兼容而补充的工具，返回明确错误；调用方原本声明的同名工具正常透传。
- 校验 SSE 错误、结束事件、工具名分片、超时和取消；错误或截断后不补成功结束标记。单事件限 8 MiB，非流式汇总限 32 MiB。
- Zen 付费模型、Go、Messages／Gemini 上游以及开启渠道或全局请求体透传的请求不应用此兼容逻辑。

这部分处理独立于请求头开关。使用免费模型时，保持请求头补齐开启、请求体透传关闭。既有 Chat／Messages → Responses 转换也经过同一响应校验；`muse-spark-*` 使用 `/v1/responses`。

`jev-1.13-free` 属于非流式 SystemOne 原生模型，不属于上述 Chat／Responses 兼容范围。本次不把它映射为普通对话模型。上游的权限、地域、额度及服务故障仍可能导致失败，非 2xx 原始错误会保留。

## 流式实测：2026-09-24

使用用户提供的 key，从当前出口调用官方模型目录，并通过本项目 Go 适配器发送请求。模拟第三方客户端 `User-Agent: claude-code/2.0`，同时启用通配符请求头透传。出站使用上表默认标识，无需额外账号组织头或 SDK UA 后缀。所有上游生成请求均为流式。

| 模型 | HTTP | 返回文本 | 正常结束 | 总 tokens |
| --- | --- | --- | --- | --- |
| big-pickle | 200 | OK | 是 | 487 |
| ling-3.0-flash-fin-free | 200 | OK | 是 | 218 |
| mimo-v2.5-free | 200 | OK | 是 | 638 |
| mimo-v2.6-flash-free | 200 | OK | 是 | 236 |
| muse-spark-1.2-contributor-free | 200 | OK | 是 | 703 |
| muse-spark-1.3-contributor-free | 200 | OK | 是 | 691 |
| nemotron-3-ultra-free | 200 | OK | 是 | 58 |
| nemotron-3.5-lightning-free | 200 | OK | 是 | 187 |
| space-bunny-free | 200 | OK | 是 | 518 |

目录中的另一个免费模型 `jev-1.13-free` 在先前 SDK 流式 Chat 尝试中返回 503 `Endpoint is unavailable`；适配器流式套件将其列为不适用，未发送非流式请求。匿名目录曾包含、但本次 key 目录不包含的模型不计入这 9 个成功结果。

另外通过 Go 适配器对 MiMo 2.5 和 Muse 1.3 发起客户端自带 `read(path)` 工具的流式请求，均返回 HTTP 200、`read({"path":"opencode-test.txt"})` 和正常结束事件。测试只验证工具调用传递，不执行工具或读取文件。

对照结果：官方 OpenCode 1.18.32 使用同一 key 调用 MiMo 成功；仅补五个标识头、SDK UA 后缀及组织头的流式请求仍为 403；加入参考项目的免费兼容请求形状后成功。此前只补 header 的修改确实没有解决这次问题。

## 复现测试

普通单元测试不访问上游：

```powershell
go test ./relay/channel/opencode ./relay/channel/openai ./relay/channel ./relay ./service ./dto
```

实测需要从环境注入凭据，禁止将 key 写入源码或提交：

```powershell
$env:OPENCODE_LIVE_TEST = '1'
$env:OPENCODE_LIVE_ALL_FREE = '1'
# OPENCODE_LIVE_API_KEY 由测试环境注入；缺省为 public。
go test ./relay/channel/opencode -run '^TestOpenCodeLiveClientIdentifiers$' -count=1 -v
```

`OPENCODE_LIVE_MODEL` 可指定单个免费模型；`OPENCODE_LIVE_TOOL=1` 验证客户端声明的 read 工具；`OPENCODE_LIVE_REPORT` 可指定脱敏 JSON 报告路径。报告不记录鉴权头。账号目录、出口、上游模型及限额变化可能改变结果。

## 官方源码依据

- [OpenCode v1.18.32 请求头组装](https://github.com/anomalyco/opencode/blob/v1.18.32/packages/opencode/src/session/llm/request.ts)
- [会话及消息 ID 格式](https://github.com/anomalyco/opencode/blob/v1.18.32/packages/schema/src/identifier.ts)
- [参考项目固定版本](https://github.com/piexian/new-api/commit/afd0be51b8e70b922cbd6cd1cc94e1c3a43cf55f)
