# vLLM 与 LiteLLM 渠道

直连推理服务选择 **vLLM（75）**；连接 LiteLLM 代理选择 **LiteLLM（76）**。两个渠道支持 Chat Completions、Responses 和模型列表，复用 new-api 现有鉴权、路由及用量统计。

## 配置

- 必须填写部署地址，例如 `https://gateway.example/prefix/v1`。也接受根地址、尾部斜杠及完整的 `/v1/chat/completions`、`/v1/responses` 或 `/v1/models` 地址，保留部署路径前缀。
- 上游 API Key 可选。未启用上游鉴权时，使用单渠道模式并留空密钥。客户端仍须使用 new-api 的令牌。
- 编辑时密钥留空表示保留原密钥。单密钥渠道可开启“清除上游密钥”，保存后删除已存储的密钥。
- 从上游获取模型，或填写上游返回的完整模型 ID。需要简称时配置模型映射。模型名称、部署地址及价格由管理员配置。

## Qwen3.8 兼容行为

使用最终请求中的上游模型名识别 Qwen3.8，支持命名空间前缀。Chat 的 `reasoning_effort` 和 Responses 的 `reasoning.effort` 采用以下兼容映射；未列出的值保留给上游校验，不会自动删除或重试。

| 客户端档位 | 发送的档位 | 行为 |
| --- | --- | --- |
| `none` | `none` | 关闭思考 |
| `minimal` | `low` | 近似映射，保留少量思考 |
| `low` | `low` | 原样发送 |
| `medium` | `medium` | 原样发送 |
| `high` | `medium` | 按本项目已确认的兼容策略转换 |
| `xhigh` | `xhigh` | 原样发送 |
| `max` | `xhigh` | 近似映射到最高可用档位 |
| 不传 / `null` | 保留 | 使用上游默认值；本次部署默认 `xhigh` |

LiteLLM 的 Qwen3.8 Chat 请求携带推理档位时，自动将 `reasoning_effort` 合并到 `allowed_openai_params`。该字段用于让 LiteLLM 向后端转发参数，不会注入 vLLM 直连请求。

LiteLLM 请求中 `extra_body` 内的推理参数会覆盖同名顶层参数。适配器先将这个实际生效的参数提到顶层，再执行映射、参数放行和日志记录。vLLM 直连仍以显式顶层参数优先。

LiteLLM 的 Qwen3.8 Responses 请求会将顶层 `chat_template_kwargs` 转入 `extra_body.chat_template_kwargs`，因为本次部署会忽略前一种写法。如果两处都有值，保留原有的嵌套值。Chat 可直接使用顶层 `chat_template_kwargs`。

Qwen3.8 后端的聊天模板只接受开头的一条 `system`。多条或后置的 `system` 会按原出现顺序合并到开头，字符串之间使用空行分隔，内容数组保留原有内容块。Chat 中的 `developer` 一并合并为 `system`，因为 LiteLLM 会将这类角色转换为 `system`。该后端无法分别表示这些指令角色的优先级。

Responses 同时提供 `instructions` 和 `input` 中的 `system` 时，将 `instructions` 内容放在合并后的系统指令最前面，并移除独立的 `instructions` 字段，避免后端再次生成一条系统消息。只有 `instructions`、没有 `system` 输入时保持原样；Responses 的 `developer` 输入保持原角色和顺序。

普通对话、工具调用与工具结果的相对顺序不变。合并不丢弃指令内容；如果多条指令含有无法同时保留的不同 `name`、`id` 等消息级属性，会明确返回 400。已有服务端会话中未随请求传入的历史消息无法在此处整理。

这些规则在发送前执行，包括参数覆盖和请求体透传模式；日志记录实际发送的推理档位。其他模型不应用上述 Qwen3.8 兼容规则。

已有 OpenAI 渠道不会自动变为新渠道，也不应用以上兼容处理。部署本版本后，连接 LiteLLM 的渠道需由管理员选择 **LiteLLM（76）**；直连 vLLM 则选择 **vLLM（75）**。

vLLM 渠道将 JSON 请求中的 `extra_body` 展开到顶层，显式顶层字段优先。可通过 `chat_template_kwargs` 传递思考开关，`false` 和数值 `0` 会保留。

空工具数组会被移除；没有工具时 `tool_choice: "none"` 或 `"auto"` 会被移除，强制调用工具的请求则返回 400。上游流式错误及异常中断会保留为失败，不补造成功结束事件。

## 思考参数实测范围

2026-10-06，通过 LiteLLM 1.90.0 对 `Qwen3.8-Flash-Next-FP8` 和 `qwen3.8-27b-fp8` 进行测试，以下结果在两个模型上均复现。它们描述的是本次部署，不代表所有 vLLM/LiteLLM 版本。

- 原生档位：`none`、`low`、`medium`、`xhigh` 接受；`minimal`、`high`、`max` 返回 400，因此需要上表的映射。Chat 还需要 LiteLLM 放行 `reasoning_effort`。
- 思考开关：`chat_template_kwargs.enable_thinking` 为 `false` 时，Chat 可以关闭思考；Responses 需要将它放在 `extra_body` 内。上面的适配器已处理这一位置差异。`none` 和显式 `enable_thinking: true` 互相冲突，上游返回 400；请勿同时发送矛盾的设置。
- 精确预算：Chat 的 `extra_body.thinking_token_budget` 有效，测试 `0`、`1`、`32`、`64`、`-1`；32/64 时报告的思考 token 分别为 31/63。`-1` 表示不限预算，小数值返回 400。经 new-api 传递时使用 `extra_body`，以保留扩展字段。
- Responses 的 `thinking_token_budget` 在顶层、`extra_body` 内均返回 200，但没有执行预算；32 时仍生成超过 32 个思考 token。不能将数值预算等同于 `low`、`medium` 等档位，也不能用 `max_output_tokens` 替代，因为后者限制整个输出。
- 顶层 `enable_thinking: false`、`think: false`、`reasoning.enabled: false` 均返回 200，但没有关闭思考。需要关闭时，客户端应使用标准档位 `none` 或上述模板开关；适配器没有将这些不同协议的别名自动转换。
- `thinking: {"type":"enabled","budget_tokens":32}` 在 Chat 被 LiteLLM 拒绝；强制放行 `thinking` 后返回 SDK 参数错误，不能只靠加白名单实现兼容。Responses 接受该字段但未执行预算。Chat 应改用 `extra_body.thinking_token_budget`。
- `reasoning.summary` 的 `auto`、`concise`、`detailed` 均可返回 200，但 Responses 的摘要始终为空，不能据此宣称支持摘要生成。`reasoning.max_tokens` 和模板内的 `thinking_budget`/`thinking_token_budget` 也未观察到预算限制效果。

## 验证

单元测试覆盖地址、鉴权、模型映射、参数覆盖、透传、指令消息合并、工具调用、推理档位、显式零值及流式错误。

真实调用测试默认跳过。设置 `SELFHOST_LIVE_BASE_URL`、逗号分隔的 `SELFHOST_LIVE_MODELS`，以及可选的 `SELFHOST_LIVE_KEY`；`SELFHOST_LIVE_TYPE` 为 `vllm` 或 `litellm`（默认），然后运行：

```text
go test ./relay -run '^TestLiveSelfHosted$' -v -count=1
```

测试会产生真实推理调用，覆盖 Chat/Responses 普通与流式工具调用、工具结果回传、多条/后置系统指令、Chat developer 和 Responses instructions 的组合，以及档位映射、嵌套推理参数和关闭思考。
