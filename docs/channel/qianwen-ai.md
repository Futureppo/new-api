# 千问AI平台渠道

渠道类型为 **千问AI平台（77）**，接入 qianwenai.com 按量付费服务。默认地址为 `https://maas.qianwenaiapi.com`，填写该平台 API Key；与已有的阿里通义千问渠道分别配置，无需数据库迁移。

Base URL 可填根地址、`/compatible-mode/v1`、`/apps/anthropic`、`/apps/anthropic/v1`、`/compatible-api/v1`、`/api/v1`，或下表中的完整生成接口地址。网关移除已知后缀后拼接目标路径，保留反向代理前缀。不要带查询参数、用户名密码或 fragment。

## 接口与模型

| 客户端入口 | 上游接口 | 说明 |
| --- | --- | --- |
| `POST /v1/chat/completions` | `/compatible-mode/v1/chat/completions` | 原生 OpenAI Chat；JSON / SSE |
| `POST /v1/chat/completions`，模型 `tongyi-intent-detect-v3` | 同上 | 意图分类、意图加工具调用、仅工具调用 |
| `POST /v1/messages` | `/apps/anthropic/v1/messages` | 原生 Anthropic Messages；JSON / SSE |
| `POST /v1/responses` | `/compatible-mode/v1/responses` | 原生 Responses；JSON / SSE |
| `GET /v1/responses/{id}` | `/compatible-mode/v1/responses/{id}` | 查询已存储响应 |
| `DELETE /v1/responses/{id}` | 同上 | 删除已存储响应 |
| `GET /v1/responses/{id}/input_items` | 同路径加 `/input_items` | 输入项分页 |
| `POST /v1/embeddings` | `/compatible-mode/v1/embeddings` | `text-embedding-v3/v4`，稠密文本向量 |
| `POST /v1/rerank` | `/compatible-api/v1/reranks` | **仅 `qwen3-rerank` 使用此路径** |
| `POST /v1/rerank` | `/api/v1/services/rerank/text-rerank/text-rerank` | `qwen3.7-text-rerank`、`qwen3-vl-rerank`、`gte-rerank-v2` |
| `POST /v1/images/generations`、`POST /v1/images/edits` | `/compatible-mode/v1/images/generations` | `qwen-image-3.0-pro`、`qwen-image-3.0`、`qwen-image-2.1-pro` |
| 同上 | `/api/v1/services/aigc/multimodal-generation/generation` | 原生 Qwen / Wan 同步图片模型 |
| `POST /v1/audio/speech` | `/api/v1/services/audio/tts/SpeechSynthesizer` | Qwen-Audio TTS、CosyVoice、`qwen-audio-3.1-tts-next` 音频生成 |
| 同上 | `/api/v1/services/aigc/multimodal-generation/generation` | Qwen3-TTS / Qwen-TTS |
| 同上 | `/api/v1/services/audio/music/generation` | `fun-music-v1/preview` 音乐生成 |
| `POST /v1/videos` | `/api/v1/services/aigc/video-generation/video-synthesis` | Wan3.0 / Wan2.7 异步生成 |
| `GET /v1/videos/{id}` | `/api/v1/tasks/{task_id}` | 复用站内任务轮询及状态查询 |

Messages 使用渠道密钥生成 `x-api-key`，默认 `anthropic-version: 2023-06-01`；其余接口使用 `Authorization: Bearer`。客户端使用的是网关令牌。

默认测试模型 `qwen3.7-plus`。对话推荐模型还包括 `qwen3.8-max`、`qwen3.8-flash`、`deepseek-v4.1-flash`、`kimi-k3`、`glm-5.3`、`MiniMax-M2.5`。推荐列表不代表账号授权；支持手动配置其他模型及模型映射。获取模型请求 `/compatible-mode/v1/models`，保留自定义模型列表地址；失败显示上游错误，不以推荐列表替代真实结果。

渠道测试可选择自动、Chat Completions、Messages、Responses，以及对应的图片、音频、视频、向量、重排序接口。自动按模型家族选择，对话模型默认 Chat Completions。流式切换只对支持流式的接口生效。

## 意图理解

`tongyi-intent-detect-v3` 已加入推荐模型目录。官方的 OpenAI 兼容入口是 **`POST /v1/chat/completions`**，通过 system 消息选择工作模式，不需要另一个意图 API。该模型仅声明 Chat Completions 能力，Messages / Responses 返回明确错误。渠道测试在模型映射后使用意图分类样例，支持直接模型名和链式别名。

仅意图分类的请求示例：

```json
{
  "model": "tongyi-intent-detect-v3",
  "messages": [
    {
      "role": "system",
      "content": "You are Qwen, created by Alibaba Cloud. You are a helpful assistant.\nYou should choose one tag from the tag list:\n{\"alarm_set\":\"设置闹钟\",\"weather\":\"查询天气\"}\nJust reply with the chosen tag."
    },
    {"role": "user", "content": "星期五早上九点叫醒我"}
  ]
}
```

分类结果在 `choices[0].message.content`，例如 `alarm_set`。三种模式使用以下官方提示词：

| 模式 | system 消息要求 | content 输出 |
| --- | --- | --- |
| 仅意图分类 | 意图标签及描述字典，加 `Just reply with the chosen tag.` | 标签文本 |
| 意图加工具调用 | 工具定义列表，加 `Response in INTENT_MODE.` | `<tags>`、`<tool_call>`、`<content>` 文本 |
| 仅工具调用 | 工具定义列表，加 `Response in NORMAL_MODE.` | 工具调用文本或补充参数的追问 |

工具模式按文档把 `[{"name":"工具名","description":"说明","parameters":{...}}]` 写在 system 消息的 `The tools you can use are as follows:` 后面；不要只依赖普通 Chat 的顶层 `tools` 字段替代该模型的提示词协议。官方建议工具数不超过 10 个。

网关原样保留意图标签、工具调用文本及多轮 messages，不把 `<tool_call>` 自动变为 OpenAI `tool_calls`，也不执行其中的工具。发生追问时，将上一轮 assistant 的完整 content 加入 messages，再追加用户补充信息。继续复用模型映射、参数覆盖和真实 Token 用量结算，不自动设置模型价格。

官方也提供 DashScope 的 `/api/v1/services/aigc/text-generation/generation` 示例。本渠道的客户端入口使用上面的 OpenAI 兼容协议；把该完整上游地址填入渠道 Base URL 时会规范化为根地址，再调用兼容路径。

模拟测试覆盖三种模式的 JSON / SSE 转发、多轮历史、别名、参数覆盖及扣费。真实意图模型测试 `TestQianwenIntentLive` 使用 `QIANWEN_API_KEY`，未配置时跳过；该模型的实际流式能力与账号权限仍需上游实测。

## 对话与 Responses 存储

保留模型映射、参数覆盖、工具调用及结果、图片内容、思考内容、结构化输出，以及 `preserve_thinking`、`thinking_budget`、`tool_stream`、`search_options` 等扩展。可选数值和布尔值保留显式 `0`、`false`。

Responses 保留 `previous_response_id`、`conversation`、`store`。官方默认 `store:true`，保存的响应有效期为七天；`store:false` 不创建本地查询路由。网关复用现有任务表，仅记录响应 ID、创建者及路由信息，不存储对话正文。

查询和删除须使用创建时的同一网关用户、令牌；路由使用创建时的渠道地址及密钥，仍检查当前渠道状态和令牌模型权限。只支持经此网关创建并记录的响应，不支持导入外部响应 ID。GET / DELETE 不再次收取生成费用。

`input_items` 支持 `after`、`limit`（1–100）、`order`（`asc` / `desc`）。`previous_response_id` 与 `conversation` 的组合规则及模型能力由上游校验。

**`POST /v1/responses/compact` 没有找到官方支持文档，因此明确拒绝；`background:true` 也被官方标为不支持。** 不把上游可能忽略的参数当作已支持功能。

## 图片

```json
{
  "model": "qwen-image-3.0-pro",
  "prompt": "把猫的背景改成花园",
  "image": ["https://example.com/cat.png"],
  "n": 1,
  "size": "1024x1024",
  "seed": 0,
  "prompt_extend": false,
  "enable_thinking": false,
  "watermark": false,
  "response_format": "url"
}
```

`image` 支持 URL / data URI 字符串或字符串数组；编辑入口还支持 multipart `image` / `image[]` 文件，每个文件最多 10 MB。`response_format` 可为 `url` 或 `b64_json`。下载生成结果复用站内文件获取及 SSRF 设置。

原生同步图片模型转换为 `input.messages[].content` 和 `parameters`；支持直接提供原生 `input` / `parameters` 扩展。兼容图片使用 `1024x1024`，原生接口转为 `1024*1024`。不支持同步图片流、mask 编辑；不包含异步图片任务、图像放大或其他未适配图像操作。

## 音频与音乐

统一通过 `/v1/audio/speech`，返回音频二进制。扩展参数放在 `metadata`，对应上游 **input 对象内部**；指令可用 `instructions`，参考音频可用 `input_references`。示例：

```json
{
  "model": "qwen-audio-3.0-tts-flash",
  "input": "你好，欢迎使用。",
  "voice": "longanhuan_v3.6",
  "response_format": "mp3",
  "metadata": {"seed": 0, "volume": 50, "enable_aigc_tag": false}
}
```

`stream:true` 返回音频字节流；`stream_format:"sse"` 返回 `audio.delta`（Base64）和最终 `audio.done`（包含原生 usage）。使用 `X-DashScope-SSE: enable` 请求上游，累计用量取最终值，不把各块重复相加。流中断或上游错误不发送成功结束事件。

Qwen3-TTS 非流式只提供 WAV，流式为 PCM；不做编码转码。`qwen-audio-3.1-tts-next` 目前仅适配文档中的同步模式，不接受 `voice`，将 `input` 转成 `text_prompt`，可通过 `metadata.references` 传参考音频。

```json
{"model":"qwen-audio-3.1-tts-next","input":"清晨树林中的鸟鸣和轻风","metadata":{"seed":0,"enable_cbr":false}}
```

```json
{"model":"fun-music-v1","input":"轻快的钢琴与弦乐","metadata":{"is_instrumental":true,"watermark":false}}
```

音乐歌词等参数放在 `metadata`。这里覆盖 HTTP 语音合成、音频与音乐生成，不包含 ASR、实时 WebSocket、声音复刻管理接口。`/v1/audio/generations`、`/v1/music/generations` 的其他平台任务协议不用于本渠道。

## 视频

```json
{
  "model": "wan3.0-video",
  "prompt": "猫走过花园，镜头缓慢推进",
  "seconds": "5",
  "size": "1280x720",
  "input_reference": "https://example.com/cat.png",
  "metadata": {"parameters": {"seed": 0, "watermark": false, "audio": false}}
}
```

创建时发送 `X-DashScope-Async: enable`，返回网关任务 ID，用 `/v1/videos/{id}` 查询状态及最终结果 URL。复用任务轮询、失败退款和完成结算，不因重复查询重复扣费。

Wan3.0 / Wan2.7 使用 `resolution` / `ratio`；兼容尺寸如 `1280x720` 会转换为 `720P` / `16:9`。也可直接使用 `size:"720P"`，精细配置放入原生参数。Wan2.7 图生视频的宽高比由输入媒体决定。不同模型对分辨率、时长与参考媒体有不同限制，由上游最终校验。

`metadata.input` / `metadata.parameters` 或顶层 `input` / `parameters` 保留原生视频参数，包括参考图、参考视频、首尾帧、音频和显式零值。Wan3.0 的便捷图片字段转换为 `reference_image`，Wan2.7 转为 `first_frame` / `last_frame`；更复杂的媒体结构使用原生 `input.media`。模型选择使用渠道模型映射，音频和视频参数覆盖不能另行修改模型，以保持协议和价格一致。

## 向量与重排序

```json
{"model":"text-embedding-v4","input":["第一段文本","第二段文本"],"dimensions":1024,"encoding_format":"float"}
```

文档中的 OpenAI 向量兼容接口只支持 float 稠密向量；`base64`、稀疏输出明确拒绝，避免上游返回 200 但空向量。暂未适配 DashScope 原生稀疏、多模态向量接口。

```json
{"model":"qwen3.7-text-rerank","query":"猫","documents":["猫在花园","汽车经过"],"top_n":1,"return_documents":false}
```

原生重排序支持 `instruct`、`fps`。`qwen3-vl-rerank` 可使用对象形式的 `query` 及多模态 `documents`；其他模型要求文本 query。输出统一为 `results` / `usage`，用量只累计一次。

## 价格与验证

沿用站内价格配置，不自动写入全局价格。非 Token 模型在模型价格中配置**每一个单位**的价格：

| 模型 | 价格单位 | 实际结算 |
| --- | --- | --- |
| Qwen 图片 | 每张 | 实际返回图片数，不用请求 n 冒充成功张数 |
| Wan2.7 图片 | 可用 Token 定价或每张定价 | 真实 input/output tokens 或实际张数 |
| Qwen3-TTS、Qwen-Audio 3.0 TTS、CosyVoice | 每字符 | `usage.characters` |
| Qwen-Audio 3.1 TTS flash、Qwen-TTS | Token | 真实输入 / 输出 usage |
| Qwen-Audio 3.1 TTS next、Fun-Music | 每秒 | `usage.duration` |
| Wan 视频 | 每秒 | 最终 `usage.duration`，保留小数，不再加输入/输出时长明细 |

如果官方价格为每万字符，须先除以 10000 后填写每字符价格。不同分辨率或模式价格不同，可配置不同模型别名、价格和渠道参数覆盖。视频按请求时长预扣，默认 5 秒、智能时长预留 30 秒，完成后按实际用量补扣或退款；参考视频计费以最终总时长为准。

模拟测试覆盖对话三协议、图片、音频、视频、文本向量、重排序、模型映射、显式零值、用量结算、错误和 Responses 权限隔离。真实连通测试通过 `QIANWEN_API_KEY`（可选 `QIANWEN_TEST_MODEL`）启用 `TestQianwenLiveGateway`；该用例仅验证三种对话协议。没有密钥时跳过，媒体接口仍需专用测试模型与额度做真实验收。模拟通过不代表上游实际可用。

## 官方文档

核对日期：2026-10-14。

- [Chat](https://platform.qianwenai.com/docs/api-reference/chat/openai-chat)、[Messages](https://platform.qianwenai.com/docs/api-reference/chat/anthropic)、[Responses](https://platform.qianwenai.com/docs/api-reference/chat/openai-responses)
- [意图理解（Tongyi-Intent-Detect）](https://platform.qianwenai.com/docs/developer-guides/text-generation/intent-detect)
- [查询响应](https://platform.qianwenai.com/docs/api-reference/chat/retrieve-response)、[删除响应](https://platform.qianwenai.com/docs/api-reference/chat/delete-response)、[输入项](https://platform.qianwenai.com/docs/api-reference/chat/list-input-items)
- [兼容图片](https://platform.qianwenai.com/docs/api-reference/image-generation/qwen-text-to-image-openai)、[原生 Qwen 图片](https://platform.qianwenai.com/docs/api-reference/image-generation/qwen-text-to-image)、[Wan2.7 同步图片](https://platform.qianwenai.com/docs/api-reference/image-generation/wan27-image-gen-edit/synchronous)
- [文本向量](https://platform.qianwenai.com/docs/api-reference/text-embedding/openai-embedding)、[兼容重排序](https://platform.qianwenai.com/docs/api-reference/rerank/openai-rerank)、[原生重排序](https://platform.qianwenai.com/docs/api-reference/rerank/dashscope-rerank)
- [Qwen-Audio TTS](https://platform.qianwenai.com/docs/api-reference/speech-synthesis/qwen-audio-tts/http-api)、[Qwen3-TTS](https://platform.qianwenai.com/docs/api-reference/speech-synthesis/qwen-tts)、[CosyVoice HTTP](https://platform.qianwenai.com/docs/api-reference/speech-synthesis/cosyvoice-nrt/http-api)、[音频生成](https://platform.qianwenai.com/docs/api-reference/audio-generation)、[音乐生成](https://platform.qianwenai.com/docs/api-reference/music-generation/fun-music)
- [Wan3.0 创建](https://platform.qianwenai.com/docs/api-reference/video-generation/wan30-video/create-task)、[查询](https://platform.qianwenai.com/docs/api-reference/video-generation/wan30-video/query-result)、[Wan2.7 文生视频](https://platform.qianwenai.com/docs/api-reference/video-generation/wan27-text-to-video/create-task)、[图生视频](https://platform.qianwenai.com/docs/api-reference/video-generation/wan27-image-to-video/create-task)
