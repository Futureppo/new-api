# Bilibili Web 渠道

渠道类型 `79` 将 Index 网站的翻译功能转换为 OpenAI 格式。原有 Bilibili 渠道 `78` 继续提供 `Index-Translate-35B-A3B` 的文本、图片翻译接口。

## 模型与能力

| 模型 ID | 功能 | Chat Completions | Responses |
| --- | --- | --- | --- |
| `index-mt-2b-syllable` | Homura 文本翻译 | 普通、流式 | 普通、流式 |
| `index-mt-9b-syllable` | Homura 文本翻译，默认测试模型 | 普通、流式 | 普通、流式 |
| `index-nailong-9b` | Nailong 文本翻译 | 普通、流式 | 普通、流式 |
| `index-echo-2b-s2st` | Echo 2B 语音翻译 | 非流式音频 | 不支持 |
| `index-echo-9b-s2st` | Echo 9B 语音翻译 | 非流式音频 | 不支持 |

Echo 的两个 ID 是本网关定义的别名，上游使用 `2b` 和 `9b`。获取／同步模型返回维护的预设目录，不调用只列出 35B 模型的 `/v1/models`，也不表示模型当前在线。网站模型可能出现排队、限流或不可用，需通过渠道测试确认。

不支持工具调用、JSON Schema 输出、思考输出、Responses 存储会话／后台任务、`/audio/speech`、实时音频或语音识别 S2TT。文本模型只接收文本；图片翻译使用原有 Bilibili 渠道。

## 管理员配置

1. 新建 **Bilibili Web** 渠道。上游 Key 可留空，默认地址为 `https://index-translate.bilibili.com`。
2. 点击获取模型，选择需要的模型。可使用模型映射向客户端提供自定义别名。
3. 为模型配置本站价格。文本模型沿用现有计费流程；Echo 必须配置**按次价格**，因为上游不提供音频 token 用量。本渠道不预置免费价格。
4. 根据需要设置 Echo 默认源语言和目标语言，默认 `zh → en`。源语言支持 `zh/en/ja`，目标语言支持 `zh/en/ja/es/ko`，两者必须不同。
5. 执行渠道测试。文本测试发送简短翻译指令；Echo 测试发送内置英语语音，固定译为中文，并检查最终译文和 WAV 音频。

自定义地址可带反向代理前缀、末尾 `/v1` 或完整 Chat／Responses 路径。请填写 API 地址，不填写带 `?p=` 的网页链接。代理需要转发 URL 查询参数，接口路径位于 `p` 参数内。

空 Key 时不发送 Authorization，填入后发送 Bearer。默认 User-Agent 为 `Index-Translate-Client/1.0`，管理员仍可覆盖请求头。编辑时 Key 留空保留旧值；使用“清除上游密钥”显式删除。无 Key 仅支持创建单渠道，禁止清空多密钥渠道。

## 文本调用

向本站 `/v1/chat/completions` 发送：

```json
{
  "model": "index-mt-9b-syllable",
  "messages": [
    {"role": "system", "content": "将用户文本翻译为英文，只输出译文。"},
    {"role": "user", "content": "你好，世界。"}
  ],
  "stream": true,
  "stream_options": {"include_usage": true}
}
```

也可向本站 `/v1/responses` 发送：

```json
{
  "model": "index-nailong-9b",
  "instructions": "将用户文本翻译为英文，只输出译文。",
  "input": "你好，世界。",
  "stream": false,
  "store": false
}
```

翻译指令由调用方提供，适配器保留文本消息顺序。未提供时补充 `temperature=0`、`chat_template_kwargs.enable_thinking=false`；显式零值和 false 会保留，管理员参数覆盖仍生效。此渠道不支持启用思考。

文本请求统一从上游 SSE 获取输出及 usage，再按客户端要求返回 Chat 或 Responses。若上游没有提供 usage，使用现有文本估算逻辑；明确返回的零用量不会被估算覆盖。Responses 是无状态的，后续请求需重新传入所需消息历史。

## Echo 音频调用

仅接受一条 user 消息中的一个 `input_audio`，格式为 WAV 或 MP3，时长不超过 10 秒，解码后不超过 6 MiB。输出为保留原音色的 WAV。`audio.voice` 可省略或填写 `source`；不支持选择其他声音，也不支持用文字提示改变音频任务。

Python OpenAI SDK 示例：

```python
import base64
import os
from pathlib import Path
from openai import OpenAI

client = OpenAI(
    base_url=os.environ["NEW_API_BASE_URL"],  # 例如 https://your-gateway.example/v1
    api_key=os.environ["NEW_API_KEY"],       # 本站令牌，和可选的上游 Key 不同
    timeout=180,
)

result = client.chat.completions.create(
    model="index-echo-2b-s2st",
    stream=False,
    modalities=["text", "audio"],
    audio={"format": "wav"},
    messages=[{
        "role": "user",
        "content": [{
            "type": "input_audio",
            "input_audio": {
                "format": "wav",
                "data": base64.b64encode(Path("source.wav").read_bytes()).decode(),
            },
        }],
    }],
    extra_body={
        "bilibili_web": {
            "source_language": "en",
            "target_language": "zh",
        },
    },
)

audio = result.choices[0].message.audio
print(audio.transcript)
Path("translation.wav").write_bytes(base64.b64decode(audio.data))
```

SDK 的 `extra_body` 会合并到 JSON 顶层；直接发送 HTTP JSON 时应使用顶层 `bilibili_web` 字段。

适配器上传后每 2 秒轮询一次，总超时 150 秒；反向代理和客户端应允许足够等待时间。任务失败、无效音频或空译文会报错，不计为成功。上游任务提交后不会自动重试整次请求，避免重复创建任务。返回音频 ID 不保存在服务器上，不能作为后续会话引用；客户端应直接读取音频数据。响应不虚构 token usage，成功后按管理员配置的单次价格结算。

## 本地验证

```sh
go test ./relay/channel/bilibili_web ./controller ./relay ./service ./common ./model ./relay/common ./relay/channel/bilibili ./relay/channel/openai
cd web
bun run build
```

新增测试使用本地模拟上游，不依赖公网。音频样例位于 `relay/channel/bilibili_web/testdata/hello.wav`，由 Windows Speech 在本地生成，内容为 “Hello. Good morning.”，16 kHz 单声道 PCM，约 2.77 秒。

上游页面：[Homura](https://index-translate.bilibili.com/?p=/site/syllable.html)、[Nailong](https://index-translate.bilibili.com/?p=/site/doc.html)、[Echo](https://index-translate.bilibili.com/?p=/site/index.html)。
