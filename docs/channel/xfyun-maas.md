# 讯飞星辰 MaaS

渠道类型：**讯飞星辰 MaaS**（74）。它与旧的讯飞星火 WebSocket 渠道（18）相互独立。

## 配置

- 默认代理地址：`https://maas-api.cn-huabei-1.xf-yun.com`。也接受带 `/v1`、`/v2` 的 SDK 基础地址；自定义代理的路径前缀会保留。
- 密钥填写星辰平台 APIKEY，不需要旧渠道的 `APPID|APISecret|APIKey`。
- 一个 APIKEY 可以启用多个模型，同一渠道可填写这些模型的 ID。模型预设只是公开文档中的示例，不代表该密钥已获授权；请选择实际已开通的模型。
- 可以用现有模型映射将客户端别名映射为服务 ID，例如 `{"my-chat":"xopglm53"}`。
- 上游未公布模型列表接口，因此默认不请求 `/models`，也不启用自动模型巡检。可配置自己的兼容模型列表接口后使用巡检。
- 模型价格需在站内自行配置；图片生成使用按次价格。不根据底层模型名字猜测星辰的服务价格。
- Kolors 应另建同类型渠道，将基础地址设为 `https://xingchen-api.cn-huabei-1.xf-yun.com`，不自动切换域名或重复请求。

## 入口

| 客户端入口 | 上游入口 | 说明 |
| --- | --- | --- |
| POST /v1/chat/completions | /v2/chat/completions | 文本、视觉；支持普通和 SSE 流式响应 |
| POST /v1/responses | /v1/responses | 原生 Responses 创建、流式、function 工具 |
| POST /v1/messages | /anthropic/v1/messages | 原生 Anthropic 消息和事件流 |
| POST /v1/images/generations | /v2.1/tti | 一次一张 Base64 图片 |
| POST /v1/embeddings | /v2/embeddings | 文本或文本数组；float 和 base64 输出 |
| POST /v1/rerank | /v2/rerank | 项目已有的重排序格式，并非 OpenAI 官方接口 |

客户端和上游均使用 `model`。2026-09-28 实测：Chat、Responses 和 Messages 发送 `model_id` 都被上游拒绝，错误为 `invalid parameter model_id; use model`，因此以真实接口为准，不按旧参数表重命名。响应兼容处理仍可将 `model_id` 恢复为 `model`。

这些入口始终执行协议转换，不受全局或渠道请求体透传开关影响。渠道参数覆盖针对**转换后的上游请求体**。

### 文本与视觉

```json
{
  "model": "my-chat",
  "messages": [{"role": "user", "content": "你好"}],
  "stream": true,
  "stream_options": {"include_usage": true},
  "temperature": 0,
  "enable_thinking": false
}
```

视觉消息使用标准 `image_url` 内容块，支持 HTTPS 图片地址和 `data:image/...;base64,...`。标准可选参数的显式零值和 false 会保留。

视觉文档及 Spark、Qwen 文本实测均支持 `stream_options.include_usage`，适配器保留该字段，按客户端偏好输出用量并收集完整 usage 用于结算。文本 max_tokens 在未提供 max_completion_tokens 时转换为后者。

### Responses 与 Anthropic

Responses 支持普通及流式创建、文本和 function 工具；不支持 background=true、previous_response_id、conversation、context_management、历史检索或 compact。多轮会话需在 input 中提供历史记录。

Anthropic 的 thinking.type=enabled/adaptive 转为 enable_thinking=true，disabled 转为 false；output_config.effort 支持 low/medium/high。thinking.display=omitted 对应 clear_thinking=true。budget_tokens 无法等价映射，明确报错，不静默忽略。MCP、container 和 context_management 不支持。

流式错误会记录并返回错误事件；已发送部分结果后不重试，不补造成功结束事件。部分生成按已有 usage 结算，未提供 usage 时沿用项目估算。

### 图片生成

```json
{
  "model": "your-image-model-id",
  "prompt": "山间的一只猫",
  "n": 1,
  "size": "768x768",
  "response_format": "b64_json",
  "seed": 0,
  "num_inference_steps": 20,
  "guidance_scale": 5,
  "scheduler": "DPM++ 2M Karras",
  "negative_prompt": "模糊"
}
```

返回 `{"created": ..., "data": [{"b64_json": "..."}]}`。

- n 省略时为 1，其他值报错；response_format 省略时为 b64_json，url 不支持。
- 不支持流式生图、图片编辑或托管图片 URL。
- 默认尺寸 768x768；支持 768x768、1024x1024、576x1024、768x1024、1024x576、1024x768。
- seed 默认随机，范围 0–2147483647；步数默认 20（0–50）；guidance_scale 默认 5（0–20）。
- scheduler 支持 DPM++ 2M Karras、DPM++ SDE Karras、DDIM、Euler a、Euler。
- prompt 和 negative_prompt 上限均为 1024 个字符；可选 user 为至多 32 个字符的字符串。
- 扩展参数可直接放在请求顶层，也可放在 extra_fields；同名时顶层优先。OpenAI SDK 的 extra_body 会将字段展开至请求顶层。
- 公共生图服务也要求 header.patch_id，适配器固定发送 `["0"]`。缺少它时上游返回 HTTP 200、业务错误码 10004，不能作为成功结算。自定义精调 patch_id 不在本次适配范围。

### 向量和重排序

向量 input 仅接受非空字符串或字符串数组，不接受 Token ID。上游统一返回 float；客户端请求 encoding_format=base64 时，网关转换为 float32 小端字节序的 Base64。dimensions 原样传递，实际可用维度取决于模型。

`xop3qwen8bembedding` 的上游校验明确列出 9 档维度：**32、64、128、256、512、768、1024、2048、4096**。2026-09-28 已逐档验证单条／批量输入及 float／Base64 输出，返回长度均符合请求；省略 dimensions 时实测默认 **768**。1536、3072 等非列表值会返回 HTTP 400，不能直接套用其他厂商模型的维度。

维度专项测试覆盖 40 个成功请求（9 档加默认值，各验证两种输入形式和两种编码）及 9 个非法维度请求；每个输出均与**同一次请求**的上游原始向量逐项比较，Base64 解码后的 float32 数值完全一致。重复推理的向量数值可能小幅变化，因此跨请求的逐位相等不能作为编码转换正确性的判断依据。非法维度不会产生成功消费记录，余额退还检查通过。

重排序 documents 必须为字符串数组。网关对完整结果稳定降序排序，再应用正整数 top_n；return_documents=true 时按原始索引补回 `document: {"text": "..."}`。不支持 max_chunk_per_doc、overlap_tokens。计费使用上游完整 usage。

## 测试及已知文档差异

渠道测试请明确选择对应接口，尤其是 ID 不含 embedding/rerank 关键词的自定义服务。测试不同能力时选择已启用的对应模型，并先配置模型价格。

2026-09-28 使用**同一把 APIKEY**在官方域名完成以下真实联调，未使用 curl 示例中的另一把密钥：

| 模型 ID | 实测结果 |
| --- | --- |
| spark-x2.5-1.7b | Chat、Responses、Messages 普通／流式；三种协议的函数工具、思考内容和用量；缓存命中信息 |
| xop35qwen2b | Chat、Responses、Messages 普通／流式及用量 |
| xophunyuanocr | HTTPS /v2/chat/completions；Base64 和 HTTPS 图片 URL；普通／流式 OCR |
| xopzimageturbo | 标准生图请求转换；768x768、seed=0、负面提示；返回可解码 PNG |
| xopqwentti20b | 同上；默认 20 步、guidance=5、DPM++ 2M Karras 可用 |
| xop3qwen8bembedding | 全部 9 档维度及默认 768；单条／批量文本；float／Base64 均与各自上游响应逐项一致 |
| xop3qwen8breranker | 三份文档排序、top_n=1、文档回填；按完整 254 Token 结算 |

完整网关已通过 31 项真实测试，包括鉴权、渠道分发、模型别名、响应转换、用量日志以及用户／令牌余额一致性。测试使用内存数据库和人工测试价格，不修改正式渠道、账户或模型定价。另有模拟上游错误、连接中断、显式零值和退款测试；生图业务错误或空结果不会产生成功消费记录。脱敏上游响应保存在 `relay/channel/xfyun_maas/testdata/`，生图样例中的图片内容替换为测试 PNG。

实测纠正或确认的文档差异：

1. 三种文本协议均使用 model；视觉 HTTPS v2 已验证，不自动回退版本。
2. Responses 实际输出标准 output_text.delta、function_call_arguments.delta、reasoning_summary_text.delta 和 completed 等事件，包含必要索引。兼容旧 content_part.delta 时仍严格校验结构。
3. 公共生图必须带 patch_id=["0"]；768x768 和 payload.negative_prompts.text 可用。未验证有冲突的 512x512，因此不开放该尺寸。
4. 免费服务存在 QPS 限制，连续快速请求曾返回 429 / 11202；降低频率后同一模型成功。生产请求不会因协议猜测自动再次生成。

边界：上述验证不代表其他模型已开通。Qwen 的工具／思考能力、FLUX.1-dev 参数、Kolors 独立域名和其他向量模型尚未实测；服务价格与免费额度仍需按平台及站内配置管理。Responses 历史检索、后台任务等未列入支持范围。

可选复测（从环境变量读取密钥，不将密钥写入仓库）：

```powershell
$env:XFYUN_MAAS_LIVE_TEST = '1'
# 提前在当前环境中设置 XFYUN_MAAS_API_KEY
go test ./controller -run '^TestXfyunMaasLiveGateway$' -count=1 -v
# 仅复测向量的全部维度、编码及非法值退款
go test ./controller -run '^TestXfyunMaasEmbeddingDimensionsLive$' -count=1 -v
```

参考：[星辰 MaaS 官方文档](https://maas.xfyun.cn/doc/)，开源模型 API 3.2.1–3.2.5；[OpenAI Responses 流事件](https://developers.openai.com/api/docs/guides/migrate-to-responses#7-update-streaming-consumers)。
