# vLLM 与 LiteLLM 渠道

直连推理服务选择 **vLLM（75）**；连接 LiteLLM 代理选择 **LiteLLM（76）**。两个渠道支持 Chat Completions、Responses 和模型列表，复用 new-api 现有鉴权、路由及用量统计。

## 配置

- 必须填写部署地址，例如 `https://gateway.example/prefix/v1`。也接受根地址、尾部斜杠及完整的 `/v1/chat/completions`、`/v1/responses` 或 `/v1/models` 地址，保留部署路径前缀。
- 上游 API Key 可选。未启用上游鉴权时，使用单渠道模式并留空密钥。客户端仍须使用 new-api 的令牌。
- 编辑时密钥留空表示保留原密钥。单密钥渠道可开启“清除上游密钥”，保存后删除已存储的密钥。
- 从上游获取模型，或填写上游返回的完整模型 ID。需要简称时配置模型映射。模型名称、部署地址及价格由管理员配置。

## Qwen3.8 兼容行为

使用最终请求中的上游模型名识别 Qwen3.8，支持命名空间前缀。Chat 的 `reasoning_effort: "high"` 和 Responses 的 `reasoning.effort: "high"` 转换为 `medium`，其他档位保留。

LiteLLM 的 Qwen3.8 Chat 请求携带推理档位时，自动将 `reasoning_effort` 合并到 `allowed_openai_params`。该字段用于让 LiteLLM 向后端转发参数，不会注入 vLLM 直连请求。

Qwen3.8 后端的聊天模板只接受开头的一条 `system`。多条或后置的 `system` 会按原出现顺序合并到开头，字符串之间使用空行分隔，内容数组保留原有内容块。Chat 中的 `developer` 一并合并为 `system`，因为 LiteLLM 会将这类角色转换为 `system`。该后端无法分别表示这些指令角色的优先级。

Responses 同时提供 `instructions` 和 `input` 中的 `system` 时，将 `instructions` 内容放在合并后的系统指令最前面，并移除独立的 `instructions` 字段，避免后端再次生成一条系统消息。只有 `instructions`、没有 `system` 输入时保持原样；Responses 的 `developer` 输入保持原角色和顺序。

普通对话、工具调用与工具结果的相对顺序不变。合并不丢弃指令内容；如果多条指令含有无法同时保留的不同 `name`、`id` 等消息级属性，会明确返回 400。已有服务端会话中未随请求传入的历史消息无法在此处整理。

这些规则在发送前执行，包括参数覆盖和请求体透传模式；日志记录实际发送的推理档位。其他模型不应用 Qwen3.8 档位转换。

已有 OpenAI 渠道不会自动变为新渠道，也不应用以上兼容处理。部署本版本后，连接 LiteLLM 的渠道需由管理员选择 **LiteLLM（76）**；直连 vLLM 则选择 **vLLM（75）**。

vLLM 渠道将 JSON 请求中的 `extra_body` 展开到顶层，显式顶层字段优先。可通过 `chat_template_kwargs` 传递思考开关，`false` 和数值 `0` 会保留。

空工具数组会被移除；没有工具时 `tool_choice: "none"` 或 `"auto"` 会被移除，强制调用工具的请求则返回 400。上游流式错误及异常中断会保留为失败，不补造成功结束事件。

## 验证

单元测试覆盖地址、鉴权、模型映射、参数覆盖、透传、指令消息合并、工具调用、推理档位、显式零值及流式错误。

真实调用测试默认跳过。设置 `SELFHOST_LIVE_BASE_URL`、逗号分隔的 `SELFHOST_LIVE_MODELS`，以及可选的 `SELFHOST_LIVE_KEY`；`SELFHOST_LIVE_TYPE` 为 `vllm` 或 `litellm`（默认），然后运行：

```text
go test ./relay -run '^TestLiveSelfHosted$' -v -count=1
```

测试会产生真实推理调用，覆盖 Chat/Responses 普通与流式工具调用、工具结果回传，以及多条/后置系统指令、Chat developer 和 Responses instructions 的组合。
