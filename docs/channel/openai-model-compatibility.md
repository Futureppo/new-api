# OpenAI 模型参数兼容

文档核对日期：2026-09-24。

## 识别方式与适用范围

`pkg/openaimodel` 集中维护官方模型 ID、明确的快照和模型能力。只做区分大小写的完整名称匹配，不使用 `o*`、`gpt-5*`、任意日期后缀或提供商前缀推断模型能力。新模型需要核对官方文档后显式登记。

渠道模型映射先执行，因此自定义别名可以通过映射使用这些规则。完整模型 ID 优先于推理后缀，例如 `gpt-5.1-codex-max` 不会被拆分；`gpt-6-astra-max` 则解析为模型 `gpt-6-astra` 和推理档位 `max`。只有后缀前的名称是已登记的推理模型才进行解析。

新的参数校验、采样清理和自动 Responses 路由只用于 OpenAI 类型渠道。其他渠道保留专用适配，共享的历史模型判断改为精确匹配。请求体透传模式跳过上述处理。

## 参数与端点

- 在模型映射、推理后缀和管理员参数覆盖之后，根据最终请求的模型和推理档位进行兼容处理。参数覆盖仅执行一次，原始模型名称继续用于现有日志和计费流程。
- 已登记的新式 Chat 模型使用 `max_completion_tokens`；缺省时才从 `max_tokens` 迁移，随后删除旧字段。两个字段同时存在时，新字段优先，即使值为 `0`。Chat 转 Responses 使用相同优先级生成 `max_output_tokens`，不提高调用方设置的上限。
- 已确认不受支持的采样参数从请求中移除。对于支持非推理模式的模型，按有效推理档位决定是否清理，而不是根据一个家族前缀统一删除。缺省档位只用于能力判断，不主动写入请求。
- GPT-6 Astra 不支持 `none` 或 `minimal`。GPT-6 Sol/Luna 支持 `none`，不支持 `minimal`。已登记且明确的无效推理档位返回不可重试的 HTTP 400，错误包含模型、字段及支持值。
- Astra 的工具请求必须使用 Responses；Sol/Luna 在推理档位不是 `none` 时使用 Responses。工具定义、历史调用及工具结果均触发检测。已确认仅支持 Responses 的 Pro/Codex/o 系列型号也自动选用该端点。
- 自动转换复用现有 Responses→Chat JSON/SSE 转换，保留工具调用 ID、工具参数和 usage。`n>1`、旧式 `functions/function_call`、音频等现有转换器无法表达的输入返回 400。工具历史格式错误、结果缺少 `tool_call_id`、非 assistant 消息携带工具调用，以及无法转换的工具类型或选择方式也返回 400，避免静默丢失工具内容。
- 原生 Responses 的推理模式同时清理 `temperature`、`top_p`、`top_logprobs` 及 `include` 中的 `message.output_text.logprobs`；其余 `include` 项和未知的覆盖字段保留。

## 已核对的能力与保守处理

| 模型 | 推理档位 / 默认值 | 采样处理 |
| --- | --- | --- |
| GPT-5、mini、nano | minimal / low / medium / high；默认 medium | 移除不支持的采样参数 |
| GPT-5.1 | none / low / medium / high；默认 none | 仅 none 时保留 |
| GPT-5.2、GPT-5.4 | none / low / medium / high / xhigh；默认 none | 仅 none 时保留 |
| GPT-5.5 | none / low / medium / high / xhigh；默认 medium | 已读取的型号指南未明确采样规则，保留参数供上游判断 |
| GPT-5.6、Sol、Terra、Luna | none / low / medium / high / xhigh / max；默认 medium | 已读取的型号指南未明确采样规则，保留参数供上游判断 |
| GPT-6 Astra | low / medium / high / xhigh / max | 移除不支持的采样参数 |
| GPT-6 Sol、Luna | none / low / medium / high / xhigh / max；默认 medium | 仅 none 时保留 |

Pro、Codex、Chat、搜索和 o 系列是单独的能力记录，不继承普通型号的推理档位。对于来源没有明确说明的档位集合，不增加本地拒绝规则。不存在的模型名、第三方名称和未登记的未来快照保留原有请求内容。

## 官方来源

- [最新模型参数兼容与工具调用](https://developers.openai.com/api/docs/guides/latest-model#gpt-6-astra-update-api-and-model-parameters)
- [Chat Completions 参数](https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create)
- [推理模型](https://developers.openai.com/api/docs/guides/reasoning)
- [GPT-5.2 兼容指南](https://developers.openai.com/api/docs/guides/latest-model?model=gpt-5.2)
- [GPT-5.4 兼容指南](https://developers.openai.com/api/docs/guides/latest-model?model=gpt-5.4)
- [GPT-5.5](https://developers.openai.com/api/docs/models/gpt-5.5)、[GPT-5.5 Pro](https://developers.openai.com/api/docs/models/gpt-5.5-pro)
- [GPT-5.6 Sol](https://developers.openai.com/api/docs/models/gpt-5.6-sol)、[Terra](https://developers.openai.com/api/docs/models/gpt-5.6-terra)、[Luna](https://developers.openai.com/api/docs/models/gpt-5.6-luna)
- [GPT-6 Astra](https://developers.openai.com/api/docs/models/gpt-6-astra)、[Sol](https://developers.openai.com/api/docs/models/gpt-6-sol)、[Luna](https://developers.openai.com/api/docs/models/gpt-6-luna)
- [GPT-5.1-Codex-Max](https://developers.openai.com/api/docs/models/gpt-5.1-codex-max)、[GPT-5.2-Codex](https://developers.openai.com/api/docs/models/gpt-5.2-codex)、[GPT-5.3-Codex](https://developers.openai.com/api/docs/models/gpt-5.3-codex)

验证使用本地模拟上游，不依赖 API 密钥，也不产生真实上游调用费用。
