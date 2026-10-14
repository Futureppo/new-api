# 会话标识采集与客户端兼容性

开启风控中心的“记录会话标识”后，只从新请求的原始请求头、请求体提取标识。关闭时停止新增采集；历史日志不回填。日志中的“未提供”表示已执行采集，但请求没有可识别的有效会话标识；“未采集”表示该条日志没有执行采集。

明确会话 ID 与缓存关联 ID 分开保存、筛选和聚合。`prompt_cache_key`、`x-session-affinity` 等缓存字段不会自动变成会话 ID。仅提供缓存标识的请求，应在日志详情的“缓存关联 ID”或风控中心的“缓存关联”页签查看。

## Kimi Code

[Kimi CLI 的协议适配实现](https://github.com/MoonshotAI/kimi-cli/blob/9ab1286b8fe4e6bcd116949a27ce5e0ac3389c82/src/kimi_cli/llm.py#L349)随 provider 类型采用不同字段：

- `kimi`：通过 `prompt_cache_key` 发送，记录为缓存关联。
- `anthropic`：把会话标识直接放在 `metadata.user_id` 字符串中。仅在请求通过 `KimiCLI/`、`kimi-code-desktop/` User-Agent 或 `X-Msh-Platform: kimi_cli` 明确标识客户端时，按此规则提取。
- `openai_legacy`、`openai_responses` 等路径不一定发送会话标识。

Anthropic 适配器默认可能仅使用 SDK 的通用 User-Agent；这种情况下不能仅凭 `metadata.user_id` 判断它是会话 ID，因为其他客户端可能在这里发送账号 ID。客户端如需可靠采集，可随请求提供明确的 `session-id` 或 `x-session-id`，值应随真实会话变化，不能设置成所有会话共用的常量。

JSON 编码的 `metadata.user_id.session_id` 和 Claude 旧格式仍沿用原有提取规则；明确会话头优先。设备 ID 不参与会话关联。

## DeepSeek Harness

[原生 DeepSeek 适配器](https://github.com/deepseek-ai/deepseek-harness/blob/571902028a75c64c1d767f4ae5fd099485f0ddf0/packages/llm/llm-deepseek/src/adapter.ts#L137)发送 `x-deepseek-harness-session-id`，已支持采集。

[pi-ai 适配器](https://github.com/deepseek-ai/deepseek-harness/blob/639ed015397290b3745d163aafe02ffee4aa3f84/packages/llm/llm-pi-ai/src/adapter.ts#L380)把 `sessionId` 交给 pi-ai，由具体协议、端点兼容配置和缓存设置决定是否发送：

- [`openai-completions`](https://github.com/earendil-works/pi/blob/f07218c4d4bbc12bef056a7058c3dd49dfe41abe/packages/ai/src/api/openai-completions.ts#L770)可能发送 `session_id`、`x-session-id`、缓存亲和头或 `prompt_cache_key`，也可能不发送这些字段。
- [`anthropic-messages`](https://github.com/earendil-works/pi/blob/f07218c4d4bbc12bef056a7058c3dd49dfe41abe/packages/ai/src/api/anthropic-messages.ts#L963)的会话亲和头同样取决于兼容配置。

没有传入标识时，服务端无法还原客户端内部的会话。不会使用 `x-deepseek-harness-user-id`、请求 ID 或消息内容推测会话。代理若默认过滤含下划线的请求头，`session_id` 也可能丢失；客户端优先使用带连字符的明确会话头。

## AstrBot

支持 [`x-astrbot-conversation-id`](https://github.com/AstrBotDevs/AstrBot/blob/db4ce0c4b0ae94f443c641f0f78cc9228ddabbeb/astrbot/core/provider/headers.py#L28)。该头由新版 AstrBot 的 OpenAI、Responses、Anthropic 和 Gemini 适配器在有 conversation ID 时发送，按明确会话标识采集，并优先于通用会话头。

[4.28.2 的请求头实现](https://github.com/AstrBotDevs/AstrBot/blob/3c7adafa1397e182d60b1016bf88759265113c8a/astrbot/core/provider/headers.py)没有这个字段。即使客户端版本较新，没有 conversation ID 的调用也不会发送它。`X-AstrBot-Desktop-Session` 是桌面端认证标识，不作为对话会话 ID 采集。
