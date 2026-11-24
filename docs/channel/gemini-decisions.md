# Gemini（AI Studio）Decisions 与模型可用性

核查日期：2026-11-24。渠道类型：Gemini（24）。

`gemini-decision-1` 已接入以下两个同步入口，两者均采用 Google 官方的 `model + input + questions` 请求格式：

| 入口 | 端点类型 | 鉴权 |
| --- | --- | --- |
| `POST /v1beta/decisions` | `gemini-decisions` | `x-goog-api-key`、`key` 查询参数或 Bearer |
| `POST /v1/decisions` | `openai-decisions`，Gemini 变体 | Bearer |

调用方填写 **new-api 令牌**；Google API key 配置在 Gemini 渠道。上游地址默认为 `https://generativelanguage.googleapis.com`，两个入口最终都转发到上游 `/v1beta/decisions`。自定义代理地址可带路径前缀，也接受末尾的 `/v1beta`。

## 请求与管理界面

```bash
curl "$NEW_API_BASE_URL/v1beta/decisions" \
  -H "x-goog-api-key: $NEW_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-decision-1",
    "input": "The order arrived damaged.",
    "questions": {
      "damaged": {"type": "noul", "instructions": "Is the order damaged?"},
      "team": {
        "type": "choice",
        "instructions": "Which team should handle this?",
        "criteria": {"billing": "Payment issues", "support": "Product issues"}
      },
      "severity": {
        "type": "score",
        "instructions": "Rate the severity",
        "criteria": ["Low", "Medium", "High"]
      }
    }
  }'
```

统一入口使用相同请求体，把 URL 改成 `/v1/decisions`，鉴权头改成 `Authorization: Bearer $NEW_API_TOKEN`。配置官方 SDK 的 base URL 指向 new-api，API key 使用 new-api 令牌，即可通过原生入口调用。

- `questions` 是包含 1–1000 个命名问题的对象，支持 `noul`、`choice`、`score`。Choice 的 `criteria` 为 2–255 个字符串描述的选项对象；Score 为 2–255 个非空等级字符串的数组。
- `input` 支持字符串、单个内容块或内容块数组。内容块类型为 `text`、`image`、`audio`、`video`、`document`；PDF 使用 `document`。媒体通过 `data` 或 `uri` 提交，`mime_type` 等原生参数保留。
- `instructions` 支持字符串、文本内容块或文本内容块数组。Noul 的 `criteria.true_description`、`false_description` 可分别省略。
- 扩展字段、大整数、显式 `0/false` 保留；不接受 `stream: true` 或非 `null` 的 `stream_options`。聊天入口调用决策模型会返回指向 Decisions 入口的错误。

渠道支持现有模型映射、参数覆盖、分组、令牌模型权限、限流及重试。每次重试从客户端原请求构建，前一个渠道的覆盖参数不会泄漏到下一个渠道。启用全局或渠道请求体透传时按原请求发送，绕过模型映射及参数覆盖。

管理界面已加入模型预设和端点发现。渠道测试根据实际映射模型自动选择 Gemini Decisions，也可显式选择统一 Decisions；Playground 为两个入口提供 Gemini 请求模板，流式选项关闭，并显示官方输入、输出 token 统计。

## 用量、计费与错误

官方输入价为 **$0.10 / 百万 token**，涵盖文本、图片、音频、视频、PDF。默认输入倍率 `0.05`，输出倍率 `0`。加载旧配置时补齐缺失值，保留管理员已配置的价格及零值。

按**客户端请求模型**计价，别名需配置自己的价格；模型映射、参数覆盖和重试不改变计费模型，不添加 `-openai-decisions` 后缀。预扣估算使用文本和问题内容，媒体 base64 不参与文本估算；最终按上游 `usage.total_input_tokens` 结算。

答案、概率、置信度、`prompt_feedback` 和扩展字段原样返回。正常响应须报告非负整数输入用量和零输出用量；如包含 `total_tokens`，应与输入用量一致。无效用量返回 502，并按现有机制退款。

内容拦截允许空答案：报告用量时按该用量结算；只有拦截反馈且没有用量时按零用量处理。上游 HTTP 错误沿用网关状态码映射、重试与退款逻辑。

依据：[Decisions 指南](https://aistudio.google.com/docs/decisions-api)、[REST API](https://aistudio.google.com/api-reference/decisions/create-decision)、[模型与价格](https://aistudio.google.com/docs/models/gemini-decision-1)。

## 仍缺少完整调用链路的模型

以下为 **代码与官方文档核查**，核查对象是 Gemini / AI Studio 渠道。`gemini-decision-1` 已由本次接入补齐；其余项目列为后续适配。

| 模型或系列 | 缺少的调用链路 | 后续适配 |
| --- | --- | --- |
| `gemini-3.8-live`、`gemini-3.8-live-extended-thinking`、`gemini-3.1-flash-live-preview`、`gemini-3.5-live-translate-preview`、`gemini-2.5-flash-native-audio-*` | [Gemini Live WebSocket](https://aistudio.google.com/docs/live-api) | 会话建立、双向音视频、实时事件与结算 |
| `gemini-3.5-transcribe-live`、`gemini-robotics-er-2-streaming-preview` | 实时转写、机器人实时输入协议 | 实时输入及结果事件转发；参见 [Transcribe](https://aistudio.google.com/docs/transcribe) 和 [Robotics](https://aistudio.google.com/docs/robotics-overview) |
| `lyria-realtime-exp` | [实时音乐会话协议](https://aistudio.google.com/docs/realtime-music-generation) | 音乐 WebSocket 会话、控制消息与音频输出 |
| `deep-research-preview-04-2026`、`deep-research-max-preview-04-2026`、`antigravity-preview-09-2026` | Interactions 与任务生命周期 | 创建、查询、取消、结果和用量结算；[Deep Research 要求 Interactions](https://aistudio.google.com/docs/deep-research) |
| `gemini-omni-1.1-flash`、`gemini-omni-1.2-flash` | [Omni 视频 Interactions](https://aistudio.google.com/docs/omni) | Interactions 视频提交及结果处理；现有 Veo `predictLongRunning` 无法承接 |
| `genie-4` | [Interactions 实时世界会话及媒体传输](https://aistudio.google.com/docs/models/genie-4) | 实时会话生命周期、交互输入和媒体传输 |

## 预设中已被上游下线的型号

这些型号的阻塞原因是**上游下线**。本次保留已有预设，批量清理另行处理。日期依据 [官方下线清单](https://aistudio.google.com/docs/deprecations)，验证状态为代码与文档核查。

| 当前预设 | 官方下线日期（2026 年） |
| --- | --- |
| `gemini-2.0-flash`、`gemini-2.0-flash-001`、`gemini-2.0-flash-lite`、`gemini-2.0-flash-lite-001` | 06-01 |
| `gemini-3-pro-preview` | 03-09 |
| `gemini-3.1-flash-lite-preview` | 05-25 |
| `gemini-2.5-flash-lite-preview-09-2025` | 03-31 |
| `gemini-3.1-flash-image-preview`、`gemini-3-pro-image-preview` | 06-25 |
| `gemini-robotics-er-1.5-preview` | 04-30 |
| `gemini-2.5-computer-use-preview-10-2025` | 07-28 |
| `deep-research-pro-preview-12-2025` | 10-23 |
| `imagen-4.0-generate-001`、`imagen-4.0-ultra-generate-001`、`imagen-4.0-fast-generate-001` | 08-17 |
| `veo-2.0-generate-001`、`veo-3.0-generate-001`、`veo-3.0-fast-generate-001` | 06-30 |
| `veo-3.1-generate-preview`、`veo-3.1-fast-generate-preview` | 10-22 |

`gemini-embedding-2-preview` 自 11-06 自动转向正式版 `gemini-embedding-2`，不计入完全不可用。Gemini 2.5 Pro / Flash 在核查日仍向既有用户开放，计划 12-01 自动转向后继版本，不标为全量下线。

## 原生入口可承接、兼容层或参数及计费待完善

普通聊天、图像模型，以及 [TTS](https://aistudio.google.com/docs/speech-generation?generate-content=true)、非实时 [`gemini-3.5-transcribe`](https://aistudio.google.com/docs/transcribe?generate-content=true)、[Lyria 3.x](https://aistudio.google.com/docs/music-generation?generate-content=true)、[Robotics ER 2 非实时模型](https://aistudio.google.com/docs/robotics-overview?generate-content=true)保留 `generateContent` 路径。Gemini 原生入口能够承接该协议，不能因官方默认展示 Interactions 示例就将它们归为完全不可用；具体参数、OpenAI 兼容入口及计费仍需按模型核对。

## 验证状态

| 项目 | 验证方式 |
| --- | --- |
| Gemini Decisions 两个入口、鉴权、权限、映射、覆盖、透传、重试、计费、退款、内容拦截 | 本地模拟上游集成测试 |
| 三类问题、多模态、数量边界、未知字段和显式零值、异常 usage | Go 单元测试 |
| 渠道测试、Playground 端点发现与调用 | 本地模拟上游集成测试 |
| Playground 模板、禁用流式、usage 展示 | Bun 测试与前端构建 |
| 其余模型缺口及上游下线状态 | 代码与官方文档核查 |

本次没有使用 Google API key 发起真实上游调用；真实账户的模型权限和配额需通过渠道测试验证。
