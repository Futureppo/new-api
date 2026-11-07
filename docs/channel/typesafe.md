# TypeSafe

TypeSafe 渠道（类型 71）提供同步结构化评估接口 `POST /v1/systemone`，也支持 `POST /v1/decisions`。
支持 Noul、Choice 和 Score，保持官方的 `state`、`questions`、`answers` 和 `usage` 格式。

## 渠道配置

1. 新建 **TypeSafe** 渠道，填写 TypeSafe API key。
2. API 地址可留空，默认 `https://api.typesafe.ai`，也接受以 `/v1` 或 `/v1/` 结尾的地址。
3. 点击获取模型，或手动填写 `jev-latest`、`jev-preview`、`jev-1.13.0`。
4. 渠道测试默认使用 `/v1/systemone`，也可手动选择 Decisions；发送一个 Noul 问题并显示上游实际模型。

调用方使用 **new-api 令牌**，上游密钥只配置在渠道中。支持现有模型映射、参数覆盖、代理、分组及限流设置。
此渠道不支持聊天、Responses、图片、音频、`stream=true` 或 `stream_options`。

## 请求示例

```bash
curl "$NEW_API_BASE_URL/v1/systemone" \
  -H "Authorization: Bearer $NEW_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "jev-latest",
    "state": "My integration has failed for three days. Please help urgently.",
    "questions": {
      "urgent": {"type": "noul", "instructions": "Does this message express urgency?"},
      "team": {
        "type": "choice",
        "instructions": "Which team should handle this?",
        "criteria": {"technical": "Integration failures", "billing": "Payment issues"}
      },
      "frustration": {
        "type": "score",
        "instructions": "How frustrated is the customer?",
        "criteria": ["Calm", "Frustrated", "Very angry"]
      }
    }
  }'
```

`state` 可以是字符串、对象或数组。问题的详细校验由上游完成。
两个入口使用相同的 `state` 和命名 `questions` 请求，TypeSafe 渠道均转发到上游 `/v1/systemone`。
同样的请求也能通过这两个入口调用普通 OpenRouter 或千问决策渠道，模型名称按渠道配置映射。
这不包含 OpenAI 的 `input` 和问题数组格式转换，也不支持 OpenRouter Enterprise。

发往 TypeSafe、千问的请求会在参数覆盖后删除顶层 `provider`、`trace`、`session_id`，
即使启用透传也会清理；这些 OpenRouter 专有字段中的路由、追踪及隐私设置不生效。
其余未知字段、零值及答案保持原样。发往 OpenRouter 时保留上述专有字段。
响应中的 `model` 通常为具体版本；开启完整模型映射时遵循站点的模型隐藏策略。
后台从 TypeSafe 的 `/v1/models` 获取 `models[].name`；对外的 new-api `/v1/models` 保持现有 OpenAI 格式。

## 计费与错误

截至 2026-09-21，官方价格为输入 **$0.042/百万 token**，输出免费。
三个预置模型的默认输入倍率为 `0.021`、输出倍率为 `0`，管理员可覆盖。
加载旧配置时仅补齐缺失默认值，不覆盖已有价格（包括零价格）。
预扣估算包含 state 和全部 questions，最终按上游 `usage.input_tokens` 和 `usage.output_tokens` 结算。
两个入口均使用原请求模型的站内价格，不附加 `-openai-decisions` 计费后缀。
上游失败沿用网关重试策略，最终返回原生 JSON 错误和 `Retry-After`，错误详情仍受渠道设置控制。
无效响应或缺失必需 token usage 返回上游响应异常并退款，不使用估算值结算。

## 验证

普通测试不访问网络。真实测试需显式设置 `TYPESAFE_LIVE_TEST=1` 和 `TYPESAFE_API_KEY`，运行：

```bash
go test ./controller -run '^TestTypeSafeLiveGateway$' -count=1 -v
```

真实测试通过本地 HTTP 网关、临时数据库和 new-api 测试令牌调用上游，会产生少量推理用量。密钥不要写入文件。

参考：[API 文档](https://docs.typesafe.ai/api)、[模型与价格](https://docs.typesafe.ai/models)。
