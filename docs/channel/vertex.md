# Vertex 模型推理渠道

本文对应本轮 Vertex 推理适配。官方文档当前以 Gemini Enterprise Agent Platform 展示，实际调用域名仍为 `aiplatform.googleapis.com`。能力分派以 `pkg/vertexmodel` 为准，模型名称可以使用裸 ID、`publisher/model` 或 `publishers/{publisher}/models/{model}`。

## 支持矩阵

| 模型家族 | 兼容入口 | 原生动作 / 上游协议 |
| --- | --- | --- |
| Gemini 对话、多模态 | `/v1/chat/completions` | `generateContent`、`streamGenerateContent` |
| Gemini Token 工具 | 原生入口 | `countTokens`、`computeTokens`，不预扣、不结算生成费用 |
| text-embedding、gemini-embedding-001 | `/v1/embeddings` | 上游 `predict`；网关兼容 `embedContent`、`batchEmbedContents` |
| gemini-embedding-2 | `/v1/embeddings` | `embedContent`；`batchEmbedContents` 在网关逐项聚合，原生 content 可包含模型支持的媒体 |
| multimodalembedding | `/v1/embeddings` 接受文本 | 原生 `predict` 保留 image/video 输入和全部向量结果 |
| Imagen | `/v1/images/generations`、`/v1/images/edits` | `predict`；编辑、放大使用支持该能力的模型 |
| Gemini 图片模型 | `/v1/images/generations`、`/v1/images/edits` | `generateContent`、`streamGenerateContent` |
| Gemini TTS | `/v1/audio/speech` | `generateContent`、`streamGenerateContent`，原生 speechConfig 保留 |
| Gemini 音频输入 / Transcribe | `/v1/audio/transcriptions` | `generateContent`，原生 audioTranscriptionConfig 保留 |
| Veo | `/v1/videos`、`/v1/videos/{id}`、`/v1/videos/{id}/content` | `predictLongRunning`、`fetchPredictOperation` |
| Claude | `/v1/messages`、`/v1/chat/completions` | anthropic `rawPredict`、`streamRawPredict` |
| Mistral、AI21 Jamba | `/v1/chat/completions` | mistralai / ai21 `rawPredict`、`streamRawPredict` |
| Mistral OCR | `/v1/ocr` | `rawPredict`，原生字段保留 |
| Codestral FIM | `/v1/fim/completions` | `rawPredict`、`streamRawPredict`，原生字段保留 |
| 托管开放模型 | `/v1/chat/completions` | `endpoints/openapi/chat/completions`，模型使用 publisher 前缀 |
| Grok | `/v1/chat/completions`、`/v1/responses` | xai OpenAI 兼容入口；Responses 支持 JSON 和 SSE |
| Lyria | 原生入口 | `predict`，保留音频结果 |

原生入口为 `POST /v1/models/{model}:{action}` 或 `POST /v1beta/models/{model}:{action}`。客户端路径里的版本不决定上游版本：适配器按模型协议选择 `v1` 或 `v1beta1`。未适配动作会报错，不会回退为对话请求。原生请求保留 JSON 扩展字段，仍执行模型映射、参数覆盖、鉴权和渠道 RPM 限制。

渠道测试的自动检测使用同一能力表。`Vertex Native` 测试选项使用模型对应的动作和样例；不支持用通用 Gemini 对话样例测试 Lyria、OCR 等专用模型。内置目录是可选模型 ID，不代表账号已开通该模型，实际可用性取决于项目、地区、模型版本及合作方授权。

## 认证与地区

- 服务账号 JSON 沿用现有 OAuth Token 缓存、代理、自定义 Base URL 和渠道地域映射。地域映射使用客户端模型名，以兼容现有别名配置。
- Express API Key 模式仅允许 Google `generateContent`、`streamGenerateContent`、`countTokens`。向量、Veo、Imagen、Lyria、第三方模型和 `computeTokens` 需要服务账号 JSON；不支持的组合在上游请求前拒绝。
- Grok Responses 使用 global 地区及 `v1` OpenAI 兼容路径。MaaS 使用 `v1beta1` OpenAI 兼容路径，默认主机为全局 API 主机，location 按渠道配置。
- 不自动更改模型授权、Google Cloud IAM 或区域设置。需要在渠道里配置该模型实际支持的地区；第三方模型须先在项目中启用。
- GCS 下载只将服务账号 Token 发给固定的 Google Storage API 主机，禁止跳转转发凭据。HTTP 输入素材沿用项目文件获取和 SSRF 设置。

## 向量

```http
POST /v1/embeddings
Authorization: Bearer <网关令牌>
Content-Type: application/json

{"model":"gemini-embedding-001","input":["第一段","第二段"],"dimensions":768,"task_type":"RETRIEVAL_DOCUMENT","auto_truncate":false,"encoding_format":"float"}
```

支持字符串和字符串数组；不把 Token ID 数组猜测为文本。`base64` 输出为小端 float32 数组编码。旧模型转换为 `instances` / `parameters`；新模型使用 `content` / `embedContentConfig`。`input_type=search_query/search_document` 可映射为检索任务类型。

数组逐项请求，保持输出顺序并合计真实 usage。所有输入先转换校验；中途失败不返回部分向量，退还本次客户端预扣，并禁止整批重试已执行部分。成功子请求可能已由 Google 向渠道运营方计费。

```http
POST /v1/models/gemini-embedding-2:batchEmbedContents
Content-Type: application/json

{"requests":[{"content":{"parts":[{"text":"one"}]},"embedContentConfig":{"autoTruncate":false}},{"content":{"parts":[{"text":"two"}]}}]}
```

这是网关聚合入口，不要求 Vertex 提供同名批量方法。图片、音频、视频向量请用支持相应模态的模型及原生请求结构。

## 图片

默认返回 `b64_json`；`response_format=url` 返回可直接使用的 data URL。支持 JSON 的 `image` / `images` / `mask`（HTTP URL、data URL 或含 url 的对象）及 multipart 图片、遮罩上传。

Imagen 扩展参数放在 `extra_fields.parameters`；Gemini 扩展配置放在 `extra_fields.generationConfig`。保留显式 `0` 和 `false`。Imagen `n` 与 `sampleCount` 不得冲突。Gemini 图片兼容入口要求 `n=1`，原生流式生成保留 Google 事件。Gemini 编辑使用指令和参考图，不接受 Imagen 遮罩语义。

```json
{
  "model":"imagen-3.0-capability-001",
  "prompt":"把背景换成草地",
  "image":"data:image/png;base64,...",
  "mask":"data:image/png;base64,...",
  "n":1,
  "extra_fields":{"parameters":{"editMode":"EDIT_MODE_INPAINT_INSERTION","seed":0,"addWatermark":false}}
}
```

放大使用支持该操作的 Imagen 模型、单张 image 以及 `extra_fields.parameters.mode="upscale"` / `upscaleConfig`。更复杂的参考图配置、虚拟试穿和存储输出直接使用原生 `predict`，避免兼容字段限制。全部被过滤或没有有效图片时返回错误；成功结果按实际张数结算，不构造图片 Token 数。

## 音频

```json
{"model":"gemini-2.5-flash-tts","input":"你好，欢迎使用。","voice":"Kore","response_format":"wav"}
```

`/v1/audio/speech` 默认 WAV，支持 `pcm` 和 `native`。旧版 TTS 返回的 24 kHz、16-bit 单声道 PCM 会加 WAV 容器头；新版模型原生 WAV 可直接返回或取出 PCM。不引入 FFmpeg，不进行重采样或有损转码。请求 MP3、变速、SSE 兼容音频流会明确报错。

兼容流式音频使用 `stream=true`、`response_format=pcm`，可指定 `stream_format=audio`，响应是连续 PCM 字节。需要 Google SSE 事件、多说话人或模型专用配置时使用原生 `streamGenerateContent` / `generateContent`。

`/v1/audio/transcriptions` 接受 multipart `file`、`model`、`response_format=json|text`；也接受 JSON `source_url` 或 `input_audio:{data,format}`。通用 Gemini 音频模型通过转录指令生成文本，Transcribe 模型使用原生转录配置。兼容入口不生成虚构时间戳；详细词级时间信息使用原生音频转录请求与上游真实返回字段。

## 视频与任务归属

兼容入口保持单视频语义，`n` / `sampleCount` 必须为 1。`seconds`、`duration` 和 `metadata.durationSeconds` 如同时出现必须一致；最终发送的 durationSeconds 同时用于计费。原生 `parameters.sampleCount` 支持模型允许的范围，网关上限 4，最终限制由模型决定。

兼容视频扩展输入放在 `metadata`：`referenceImages`、`lastFrame`、`video`、`mask`、`cameraControl`；其他生成参数也可放入 metadata。原生入口按官方 instances / parameters 结构使用。图片、视频支持 `bytesBase64Encoded` 或 `gcsUri`。image、video、referenceImages 互斥，lastFrame 需要 image；支持最多三张 asset 参考图或一张 style 参考图。具体模型版本对引用、编辑、续写、时长、分辨率的限制以官方文档为准。

```http
POST /v1/models/veo-3.1-generate-preview:predictLongRunning
Content-Type: application/json

{"instances":[{"prompt":"平缓移动的镜头","image":{"gcsUri":"gs://my-bucket/start.png","mimeType":"image/png"},"lastFrame":{"gcsUri":"gs://my-bucket/end.png","mimeType":"image/png"}}],"parameters":{"durationSeconds":8,"sampleCount":1,"generateAudio":true}}
```

返回 `{"name":"operations/task_...","done":false}`。查询：

```http
POST /v1/models/veo-3.1-generate-preview:fetchPredictOperation
Content-Type: application/json

{"operationName":"operations/task_..."}
```

只允许当前用户查询自己的公共任务 ID。渠道、服务账号 Key 和上游 operation 从任务记录恢复，客户端不能提供其他 Google operation 路径。原生响应保存全部结果；单视频兼容下载使用 `/v1/videos/{公共任务ID}/content`，支持 Base64 和 GCS；原生多结果任务可用 `?index=1` 选择第二个视频（从 0 开始）。任务完成采用现有状态条件更新保护，重复轮询不会重复结算。全部过滤或空结果记为失败并退款，部分成功按实际成功数量调整。

## 第三方、专用预测与计费

Mistral / AI21 使用各自 publisher，不能将模型直接当作 Gemini。OCR、FIM 通过已有兼容路由保留原生字段；也可使用模型路径的 rawPredict。Claude 支持旧模型别名映射、原生 SSE event 名称及实际缓存 usage。

Grok Responses 每次明确发送 `store:false`。拒绝 `store:true`、`previous_response_id`、后台模式、context_management 及 compact；不向其他 Vertex 模型宣称支持 Responses。

Lyria 使用 `POST /v1/models/lyria-002:predict`：

```json
{"instances":[{"prompt":"轻柔的钢琴音乐"}],"parameters":{"sample_count":1}}
```

有 Token usage 的接口按真实 Token 和模态明细结算。Imagen、Lyria、multimodalembedding、OCR 等无 Token 计费依据的专用预测，必须配置按次价格（免费也应显式配置价格 0）；否则在上游调用前报错。不能依赖虚构固定 Token 数给这些接口定价。图片和视频按成功结果数量应用现有价格配置。

## 未覆盖范围与验证

本轮不实现缓存管理、Google 异步批处理作业、Live、Interactions、训练、部署、模型导入、RAG 或其他资源管理；不代理任意 Google Cloud 路径。独立 Cloud Speech 和 Vertex AI Search 排序服务不属于此渠道。没有新增数据库表或迁移。

本轮自动化验证使用本地模拟上游和测试服务账号签名材料，覆盖 URL、认证、映射、向量聚合、媒体转换、错误、取消、退款、任务归属和重复结算。未使用真实 Google 项目凭据做云端验证；模型开通、区域及实际效果需要在已配置的测试项目中验证。

本地检查记录（2026-10-14）：

- Vertex、Vertex task、Gemini、relay/common、relay/helper、common、DTO、能力表及 controller 包测试通过；Vertex 计费与文本计费专项测试通过。
- `go test ./... -run '^$'` 全项目编译检查通过。
- 前端渠道测试 5 项通过，`bun run build` 通过。
- 扩大到整个 service / relay 包时，当前工作区仍有 3 项非 Vertex 用例失败：`TestRecalculate_ActualQuotaZero`（零额度任务结算预期）、`TestMistralGatewayPipeline`（multipart temperature 字符串解析）、`TestXfyunMaasImagePipeline`（显式 seed=0 丢失）。这些失败未计入 Vertex 专项通过结论。
- `bun run i18n:lint` 未通过，诊断来自其他页面的硬编码文本；本轮渠道端点文件无对应诊断。

## 官方来源

- [文本向量与旧版 predict](https://docs.cloud.google.com/gemini-enterprise-agent-platform/reference/models/text-embeddings-api)
- [embedContent REST](https://docs.cloud.google.com/gemini-enterprise-agent-platform/reference/rest/v1/projects.locations.publishers.models/embedContent)
- [countTokens](https://docs.cloud.google.com/gemini-enterprise-agent-platform/reference/rest/v1/projects.locations.publishers.models/countTokens)、[computeTokens](https://docs.cloud.google.com/gemini-enterprise-agent-platform/reference/rest/v1/projects.locations.publishers.models/computeTokens)
- [TTS](https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/text-to-speech/overview)、[Gemini Transcribe](https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/gemini/3-5-transcribe)
- [Imagen 编辑](https://docs.cloud.google.com/vertex-ai/generative-ai/docs/image/edit-outpainting?hl=es)、[Imagen 放大](https://docs.cloud.google.com/vertex-ai/generative-ai/docs/image/upscale-image?hl=es)
- [Veo instances](https://docs.cloud.google.com/gemini-enterprise-agent-platform/reference/rest/Shared.Types/VideoGenerationModelInstance)、[Veo parameters](https://docs.cloud.google.com/gemini-enterprise-agent-platform/reference/rest/Shared.Types/VideoGenerationModelParams)
- [Mistral on Vertex](https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/partner-models/mistral)、[Mistral 部署协议](https://docs.mistral.ai/inference/deployment/cloud-deployments/vertex)
- [Grok Responses](https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/partner-models/grok/responses)
- [Lyria](https://docs.cloud.google.com/gemini-enterprise-agent-platform/reference/models/lyria-music-generation)
- [Express 方法](https://docs.cloud.google.com/gemini-enterprise-agent-platform/reference/express-mode/rest/v1/publishers.models)
