# Mistral AI 渠道

适用于官方 Mistral AI 渠道（类型 42）。Base URL 可填写 `https://api.mistral.ai` 或 `https://api.mistral.ai/v1`，允许尾部 `/`；模型列表通过 `/v1/models` 获取。

| OpenAI 接口 | 支持范围 |
| --- | --- |
| `/v1/chat/completions` | 文本、原生流式、工具调用、JSON Schema、推理、模型支持的图片和音频输入 |
| `/v1/embeddings` | 单条及批量文本、float/base64、模型支持的维度调整 |
| `/v1/audio/transcriptions` | multipart 文件上传；json、text、verbose_json、srt、vtt 输出 |

聊天请求保留通用 DTO，因此模型映射、渠道系统提示和参数覆盖继续生效。`developer` 转为 `system`，`seed` 转为 `random_seed`，`max_completion_tokens` 优先映射为 `max_tokens`（包括显式零值）。工具调用 ID 转为九位字母数字并保持历史关联。最终答案返回 `content`，thinking 返回 `reasoning_content`。

Mistral 原生流式响应总是包含 usage，网关按现有默认策略及客户端 `stream_options.include_usage` 控制输出：最终正文/工具事件、可选独立 usage 事件、一次 `[DONE]`。异常中断或上游错误不会输出正常完成标记。

Embedding 的 `dimensions` 映射为 `output_dimension`；上游固定请求浮点向量，base64 输出由网关编码为 float32 小端字节。模型不支持维度调整时保留上游错误，不截断向量；不接受 token ID 输入。

兼容模式转写传递语言、温度和时间戳粒度。每次只支持一种粒度：`segment` 或 `word`；verbose_json/srt/vtt 默认请求 segment。字幕和词时间戳使用上游数据，缺少字幕必需时间戳时返回错误。verbose_json 的 duration 在能够读取上传文件真实时长时提供；不会补造语言、概率或 token 明细。不支持非空 prompt。流式转写使用下述官方透传模式。

音频 usage 按 OpenAI 语义归一化：独立音频 token 纳入 `prompt_tokens`，保留文本、音频和缓存明细，使用现有计费流程。上游权限、限额及参数校验错误保留 HTTP 状态并转换为 OpenAI error 结构。

## 官方格式透传

以下入口沿用网关令牌鉴权、模型权限、渠道选择、限流及结算；请求字段和响应使用 Mistral 官方格式。

| 方法与路径 | 用途 |
| --- | --- |
| `POST /v1/ocr` | OCR，包括 document、pages、annotations 等官方字段 |
| `POST /v1/fim/completions` | Codestral 前后缀补全，支持原生 SSE |
| `POST /v1/agents/completions` | 已有 Agent 的推理调用，支持原生工具与 SSE |
| `POST /v1/audio/speech` | TTS，使用官方 `voice_id` / `ref_audio`；保留音频、JSON 或 SSE 输出 |
| `POST /v1/audio/transcriptions` | `stream=true` 的 multipart 请求，以及 JSON 的 `file_url` / `file_id` 请求 |
| `GET /v1/audio/transcriptions/realtime?model=...` | 原生实时转写 WebSocket |

没有模型映射或 JSON 参数覆盖时，请求体原样发送；映射时只替换 `model`，Agents 只替换 `agent_id`，保留未知字段及显式 `0/false/null`。原生入口不注入聊天系统提示，也不应用聊天强制格式化。SSE 保留 `event`、`id`、`data` 和上游结束事件，不额外生成 `[DONE]` 或 usage 事件。渠道已有的错误详情显示及完整模型映射设置仍生效。

Agents 请求使用 `agent_id` 选择渠道：将实际 Agent ID 或其映射别名加入渠道模型列表，并为该 ID/别名配置价格。这里只支持推理，不提供 Agent 创建/删除或 Conversations 持久会话管理。

后台通用模型测试暂未提供这些原生入口的专用请求样本；请使用官方格式请求或下方原生接口测试验证，避免将仅支持 OCR/TTS/实时转写的模型当作聊天模型测试。

OCR 和部分 TTS 响应不返回 token usage，因此需配置**按次价格**（也允许显式免费模型）；沿用管理员设置的每请求价格，不自动改成按页/秒计费。OCR 的 `pages_processed` 保留在原始响应和消费日志，缺失 token 保持为零。其他原生调用按上游 usage 和已有价格结算，音频 token 只在内部归一化，不改写原生响应。

示例请求（Authorization 使用网关令牌）：

```jsonc
// POST /v1/fim/completions
{"model":"codestral-latest","prompt":"def add(a, b):\n    ","suffix":"\nprint(add(1, 2))","max_tokens":64,"stream":true}

// POST /v1/audio/speech
{"model":"voxtral-mini-tts-latest","input":"Hello, Paris.","voice_id":"en_paul_neutral","response_format":"wav"}

// POST /v1/agents/completions
{"agent_id":"YOUR_AGENT_ID","messages":[{"role":"user","content":"Hello"}],"stream":true}
```

实时转写使用 Mistral 的 `session.update`、`input_audio.append`、`input_audio.flush`、`input_audio.end` 事件，音频通常为 `pcm_s16le`；协议详见[官方 Python SDK](https://github.com/mistralai/client-python/tree/main/src/mistralai/extra/realtime)。这不是 OpenAI `/v1/realtime` 的事件协议。

官方规范未提供 `/v1/responses`；它和旧式 `/v1/completions`、音频翻译仍不支持。此修复不修改 Mistral Console、数据库结构或模型价格。

## 验证

依据 [Mistral OpenAPI 规范](https://docs.mistral.ai/openapi.yaml)；请求转换、流式终结/错误/断连、Embedding 编码、转写格式及音频计费语义均有本地回归测试。

```sh
go test ./relay/channel/mistral ./relay/channel/openai ./relay/channel ./relay/common ./relay/helper ./dto ./relay ./controller ./middleware ./router ./service
go test -race ./relay/channel/mistral ./relay/channel/openai ./relay/helper
```

真实接口测试为显式启用，使用临时数据库和生产 relay 处理链路，不依赖已配置渠道。通过进程环境设置 `MISTRAL_LIVE_TEST=1` 和 `MISTRAL_API_KEY`，再运行：

```sh
go test ./relay -run 'TestMistral(LiveGateway|NativeLiveGateway)' -count=1 -v
```

可选 `MISTRAL_TEST_WAV` 指向内容含 “Paris” 的英文语音 WAV，用于校验识别内容；缺省使用合成静音样本。如运行环境需要代理，请显式设置 Go 使用的 `HTTPS_PROXY`/`HTTP_PROXY` 环境变量。不要将凭证写入源码或夹具。

已通过网关实测 Ministral、Codestral、mistral-embed、codestral-embed 和 Voxtral。真实推理模型验收受测试账号权限限制，thinking 格式使用官方样例回归；账号 Small/Medium 的零请求限额及 Large 的套餐限制保留为上游错误。

原生透传已实测 OCR、FIM 普通/流式、TTS 普通/流式、SSE 文件转写和 WebSocket 实时转写，共 7 个场景。Agents 通过模拟上游回归，真实成功调用需要已有且可访问的 Agent ID。
