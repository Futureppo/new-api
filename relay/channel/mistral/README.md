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

转写传递语言、温度和时间戳粒度。每次只支持一种粒度：`segment` 或 `word`；verbose_json/srt/vtt 默认请求 segment。字幕和词时间戳使用上游数据，缺少字幕必需时间戳时返回错误。verbose_json 的 duration 在能够读取上传文件真实时长时提供；不会补造语言、概率或 token 明细。不支持流式转写及非空 prompt。

音频 usage 按 OpenAI 语义归一化：独立音频 token 纳入 `prompt_tokens`，保留文本、音频和缓存明细，使用现有计费流程。上游权限、限额及参数校验错误保留 HTTP 状态并转换为 OpenAI error 结构。

本适配层不支持 Responses、旧式 Completions/FIM、OCR、Agents、语音合成、音频翻译及实时音频。此修复不修改 Mistral Console、数据库结构或模型价格。

## 验证

依据 [Mistral OpenAPI 规范](https://docs.mistral.ai/openapi.yaml)；请求转换、流式终结/错误/断连、Embedding 编码、转写格式及音频计费语义均有本地回归测试。

```sh
go test ./relay/channel/mistral ./relay/channel/openai ./relay/common ./relay/helper ./dto ./relay ./controller ./service
go test -race ./relay/channel/mistral ./relay/channel/openai ./relay/helper
```

真实接口测试为显式启用，使用临时数据库和生产 relay 处理链路，不依赖已配置渠道。通过进程环境设置 `MISTRAL_LIVE_TEST=1` 和 `MISTRAL_API_KEY`，再运行：

```sh
go test ./relay -run TestMistralLiveGateway -count=1 -v
```

可选 `MISTRAL_TEST_WAV` 指向内容含 “Paris” 的英文语音 WAV，用于校验识别内容；缺省使用合成静音样本。如运行环境需要代理，请显式设置 Go 使用的 `HTTPS_PROXY`/`HTTP_PROXY` 环境变量。不要将凭证写入源码或夹具。

已通过网关实测 Ministral、Codestral、mistral-embed、codestral-embed 和 Voxtral。真实推理模型验收受测试账号权限限制，thinking 格式使用官方样例回归；账号 Small/Medium 的零请求限额及 Large 的套餐限制保留为上游错误。
