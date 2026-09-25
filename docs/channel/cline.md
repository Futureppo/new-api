# Cline

选择 **Cline** 渠道并填写 API Key。默认地址为 `https://api.cline.bot/api`，也支持以 `/v1` 结尾的自定义地址。通过 `/v1/chat/completions` 转发普通、流式对话和工具调用。上游统一使用 `stream:true` 和 `stream_options.include_usage:true`，包括开启请求体透传或设置参数覆盖的情况。

客户端使用 `stream:false` 或省略 `stream` 时，网关收齐上游 SSE 后返回普通 JSON；客户端使用 `stream:true` 时继续返回 SSE。聚合保留文本、思考内容、按索引拼接的工具调用参数、多候选结果、结束原因和用量。空响应、中途报错、未完成即断流、超时或取消均不会返回部分内容作为成功结果。兼容上游普通 JSON 及其 `{success, data}` 包装。

## 客户端请求兼容

渠道按官方 Cline CLI 的请求格式补齐客户端信息。其他客户端使用 New API 的 OpenAI 兼容地址、New API 令牌和渠道保存后的模型名称，无需自己填写 Cline 请求头。仅发送 Bearer Key 而缺少客户端字段时，部分 `cline-free/*` 模型会返回 `only available via Cline product surfaces`。

实现依据为官方仓库提交 [`0cbfb91ac75c64b7a63535fde6f7f51e670dfa1b`](https://github.com/cline/cline/commit/0cbfb91ac75c64b7a63535fde6f7f51e670dfa1b)：

- [`request-headers.ts`](https://github.com/cline/cline/blob/0cbfb91ac75c64b7a63535fde6f7f51e670dfa1b/sdk/packages/llms/src/providers/request-headers.ts)：客户端请求头。
- [`apps/cli/src/main.ts`](https://github.com/cline/cline/blob/0cbfb91ac75c64b7a63535fde6f7f51e670dfa1b/apps/cli/src/main.ts)：CLI 客户端与平台信息。
- [`vendors/cline.ts`](https://github.com/cline/cline/blob/0cbfb91ac75c64b7a63535fde6f7f51e670dfa1b/sdk/packages/llms/src/providers/vendors/cline.ts)：Bearer 鉴权、Chat Completions 和流式用量。

发送 `HTTP-Referer: https://cline.bot`、`X-Title: Cline`、`User-Agent: Cline/3.0.65`、`X-CLIENT-TYPE: cline-cli`、`X-CLIENT-VERSION: 3.0.65`、`X-PLATFORM: cli`、`X-PLATFORM-VERSION: 3.0.65`、`X-IS-MULTIROOT: false`；对话额外发送 `X-CORE-VERSION: 0.0.86` 和 `X-Task-ID`。模型目录请求复用相同客户端信息。

调用方可通过 `X-Task-ID` 标识同一会话；未提供时自动生成，单次请求的渠道重试保持一致。通配及正则请求头透传不会用其他客户端的身份字段覆盖上述字段。`User-Agent` 强制使用 `Cline/3.0.65`，客户端透传、渠道显式配置及运行时请求头覆盖均不能修改；对话、免费列表及通用模型目录请求都遵循此规则。其他字段仍允许渠道显式请求头配置覆盖。

请求保留用户的消息、工具、完整模型 ID 和显式零值。与官方 SDK 一致，OpenAI o1/o3/o4 和 GPT-5 模型的 `max_tokens` 转为 `max_completion_tokens`，已显式填写的 `max_completion_tokens` 优先。

## 模型名称

Cline 模型写入渠道列表时自动去掉第一个 `/` 及其前缀，并生成“简短名称 → 上游完整 ID”的模型映射。例如 `cline-free/deepseek-v4.1-flash` 保存为 `deepseek-v4.1-flash`，`stealth/space-bunny-alpha` 保存为 `space-bunny-alpha`；上游请求仍使用完整 ID。仅移除提供商前缀，保留 `:free` 等名称后缀。

该行为覆盖新建、编辑、复制、API 保存和免费模型同步；关闭免费同步也不影响保存时的名称处理。已存在的受管完整 ID 会在下一次成功同步时转为短名。获取模型接口仍返回原始 ID，保存时建立映射。

自动同步只新增短名。已存在“短名 → 相同完整 ID”映射时直接复用，不会重复加入带前缀的名称；即使缺少旧版自动映射标记也适用。保存或下一次成功同步会清理与短名重复的完整 ID，保留原映射；获取模型后再次选择已有映射的模型也会合并为短名。

若多个上游 ID 去前缀后同名，优先复用现有精确映射；若无法确定对应关系，或短名被其他手动模型、映射占用，自动同步跳过冲突项，不回退添加带前缀的名称。手动保存的冲突模型及独立映射仍受保护。用户修改或删除自动映射后，系统不再自动删除对应模型。模型退出免费专区时，仅删除受管模型及其未被修改的自动映射。

## 免费模型自动更新

“自动更新 Cline 免费模型列表”默认开启，包括通过 API 创建且未填写该设置的渠道。系统读取 Cline 官方 `/v1/ai/cline/recommended-models` 的 `free[].id`，根据完整 ID 判断免费资格，再按上述规则保存短名与映射；不根据名称后缀猜测免费资格，也不加入订阅模型。

- 自动加入新免费模型，移除此前受管且已退出免费专区的模型。
- 保留其他手动模型和手动映射，支持现有的精确及 `regex:` 忽略列表。
- 关闭开关后保留当前模型，停止免费模型同步；“获取模型”改为读取 `/v1/models`。
- 自定义模型列表地址在免费同步开启时必须返回 `{"free":[{"id":"model-id"}]}`。地址、代理和自定义请求头均使用渠道配置。
- 请求失败、响应无效或免费列表为空时保留当前模型，记录错误并在后续巡检重试。

主节点复用现有模型巡检，默认每 30 分钟检查。新建、复制、重新开启同步或修改连接配置时检查一次；仍受 `CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_ENABLED` 和主节点限制。周期由 `CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_INTERVAL_MINUTES` 控制。

设置存储在渠道 `settings` JSON：`cline_auto_sync_free_models_enabled` 缺省或 `null` 为开启，`false` 为关闭。`cline_free_model_managed_models` 为后端维护的状态。获取模型接口支持临时 `cline_free_only` 参数，不改变已保存设置。

免费专区说明上游模型免费；站内计费继续使用现有定价，新增模型仍需满足站内计费配置要求。

## 验证

运行 `go test ./controller ./relay/channel/openai ./relay/common ./common ./dto ./model` 和前端 `bun test src/hooks/channels/upstreamUpdateUtils.test.js`。

显式设置环境变量 `CLINE_LIVE_TEST=1`、`CLINE_API_KEY` 后，运行 `go test ./relay/channel/openai -run '^TestClineLiveChat$' -count=1 -v`，会逐个测试实时官方免费目录中的普通与流式对话，并测试 DeepSeek 的普通及流式工具调用。测试模拟其他客户端请求，经真实适配器发送和转换，打印上游原始响应。可设置 `CLINE_LIVE_MODEL` 仅测试指定的当前免费模型。不要将 Key 写入仓库。

2026-09-25 实测：`stealth/space-bunny-alpha`、`cline-free/mimo-v2.6-flash`、`cline-free/deepseek-v4.1-flash`、`cline-free/gemini-3.8-flash` 的普通和流式对话，以及 DeepSeek 的两种工具调用共 10 项通过。`cline-free/muse-spark-1.3-contributor` 的两项请求均返回上游地区限制 403（`not available in your region`），实时测试如实报失败。目录同步不代表账户、地区或额度必然允许调用所有模型。
