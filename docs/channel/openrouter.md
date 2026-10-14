# OpenRouter 渠道

使用现有 OpenRouter 渠道类型（20），默认 Base URL 为 `https://openrouter.ai/api`。
也接受以 `/api/v1` 结尾的地址；自定义代理前缀会保留，例如
`https://proxy.example/openrouter/v1` 会请求 `/openrouter/v1/messages`。

## 接口

| 客户端路径 | 上游路径（相对 Base URL） | 说明 |
| --- | --- | --- |
| `/v1/chat/completions` | `/v1/chat/completions` | JSON、SSE、工具、推理、多模态 |
| `/v1/messages` | `/v1/messages` | 普通渠道默认使用原生 Anthropic 格式 |
| `/v1/responses` | `/v1/responses` | 无状态 JSON、SSE |
| `/v1/embeddings` | `/v1/embeddings` | 文本、token 数组、多模态；浮点或 base64 向量 |
| `/v1/rerank` | `/v1/rerank` | 文本或结构化文档；网关处理 `return_documents` |
| `/v1/images`、`/v1/images/generations`、`/v1/images/edits` | `/v1/images` | 生成、参考图片编辑；JSON 或 SSE |
| `/v1/audio/speech` | `/v1/audio/speech` | 字符串或多说话人 turns，二进制 PCM/MP3 |
| `/v1/audio/transcriptions` | `/v1/audio/transcriptions` | JSON 音频输入或 multipart 上传 |

`/v1/images` 原生入口只支持 OpenRouter，使用现有鉴权、限流、渠道选择与计费。
Enterprise 模式保留原有转换和响应解包行为。显式启用的 Chat/Messages → Responses 策略仍然有效。

## 请求与响应

各接口保留文档支持的 `provider`、`session_id`、`trace`；聊天、Messages、Responses
还支持相应的 `models`、`plugins`、缓存控制和工具配置。
显式的 `0`、`false` 和 `reasoning_effort: "none"` 会保留。
`reasoning_details`、Messages thinking/signature 和工具回传内容不会在普通转发时被删去。
显式推理配置优先于模型后缀默认值；顶层与嵌套 effort 冲突返回 400。

渠道模型映射、参数覆盖和禁用字段规则继续生效；例如 `speed`、`service_tier`
仍受渠道原有开关控制。需要改变这些字段时，按现有渠道设置配置。

Responses 只支持当前请求：`store: true`、非 null 的 `previous_response_id`
（包括空字符串）、`conversation`、后台执行及 `/responses/compact` 不受支持。
请在 `input` 中提供完整历史。限制会在参数覆盖后的最终请求体上检查。

图片默认返回 `b64_json`。URL 托管和 mask 编辑返回 400。编辑可使用 JSON：

```json
{
  "model": "openai/gpt-image-1",
  "prompt": "改成水彩画",
  "input_references": [
    {"type": "image_url", "image_url": {"url": "https://example.com/photo.png"}}
  ],
  "resolution": "1K",
  "aspect_ratio": "1:1",
  "seed": 0,
  "output_format": "png",
  "background": "transparent"
}
```

`/v1/images/edits` 也接受 `image` URL、`images` 数组或 multipart 的
`image` / `image[]` 文件。上传文件转换为 data URL；每个文件上限 25 MiB。
图片流按上游实际 Content-Type 处理；请求 `stream: true` 而上游返回 JSON 时仍正常返回。

TTS 多说话人输入示例：

```json
{
  "model": "支持多说话人的模型",
  "input": [{"text": "你好", "voice": "speaker_1"}, {"text": "欢迎", "voice": "speaker_2"}],
  "response_format": "mp3"
}
```

声音、参考内容及 turns 的可用性取决于具体模型。保留上游媒体类型和 `X-Generation-Id`。
STT 接受 `input_audio: {"data":"<base64>","format":"wav"}` 或
`input_audio: {"url":"https://example.com/audio.wav"}`，也接受 multipart 的 `file`
或 `source_url`。时间戳、说话人和用量字段保留。音频翻译和音频 SSE 不在本次范围。

## 计费与错误

继续使用站内价格、倍率或计费表达式，不使用上游 `usage.cost` 作为新的扣费依据。
原生 Messages 的缓存 token 与普通输入分别归一化，避免重复扣除。
图片按实际完成数量结算固定单价；token 计费不会再次乘请求的 `n`。
只有预览、错误或未正常结束的图片流退款。

音频优先采用上游 token 用量；仅报告秒数时使用既有音频换算。
缺失用量采用本地估算，显式零用量保留为零。消费日志的 `usage_source`
区分 `upstream`、`openrouter_native_messages`、`duration` 和 `local_estimate`。

HTTP 200 中的业务错误、流中错误和异常断流会记录为失败。响应提交后不再重试，
不在 SSE 尾部追加普通 JSON 错误。已产生文本且有实际消费的失败流按已有计费流程结算；
仅有预估输入、没有上游用量或输出的失败流退款。

普通模型获取使用本次支持的输出模态过滤；自定义模型列表 URL 优先。
免费和 Alpha 同步继续使用 `/models/user` 的既有过滤规则。
未加入视频、Batch、账户管理或 Responses 状态存储。

## 验证

本地 mock 测试覆盖适配器、网关鉴权、模型映射、覆盖与过滤、缓存计费、流式用量、错误、断流和退款：

```sh
go test ./relay/channel/openai ./controller ./service ./dto ./relay/helper -run OpenRouter -count=1
cd web
bun test src/helpers/channelTestEndpoints.test.js
```

真实联调默认跳过。仅在显式设置 `OPENROUTER_LIVE_TEST=1`、`OPENROUTER_API_KEY`
和 `OPENROUTER_TEST_MODEL` 后执行 `go test ./controller -run '^TestOpenRouterLiveChat$' -count=1`。
该测试会调用真实上游，可能产生费用。

文档依据（2026-10-14 核对）：[OpenAPI](https://openrouter.ai/openapi.json)、
[Messages](https://openrouter.ai/docs/api/api-reference/anthropic-messages/create-a-message)、
[Responses](https://openrouter.ai/docs/api_reference/responses/overview)、
[Streaming](https://openrouter.ai/docs/api_reference/streaming)、
[Reasoning](https://openrouter.ai/docs/guides/best-practices/reasoning-tokens)、
[图片](https://openrouter.ai/docs/guides/overview/multimodal/image-generation)、
[TTS](https://openrouter.ai/docs/guides/overview/multimodal/tts)、
[STT](https://openrouter.ai/docs/guides/overview/multimodal/stt)。
