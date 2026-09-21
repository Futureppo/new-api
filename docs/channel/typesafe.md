# TypeSafe

TypeSafe 渠道（类型 71）提供同步结构化评估接口 `POST /v1/systemone`。
支持 Noul、Choice 和 Score，保持官方的 `state`、`questions`、`answers` 和 `usage` 格式。

## 渠道配置

1. 新建 **TypeSafe** 渠道，填写 TypeSafe API key。
2. API 地址可留空，默认 `https://api.typesafe.ai`，也接受以 `/v1` 或 `/v1/` 结尾的地址。
3. 点击获取模型，或手动填写 `jev-latest`、`jev-preview`、`jev-1.13.0`。
4. 渠道测试自动使用 `/v1/systemone`，发送一个 Noul 问题并显示上游实际模型。

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
响应中的 `model` 通常为具体版本；开启完整模型映射时遵循站点的模型隐藏策略。
后台从 TypeSafe 的 `/v1/models` 获取 `models[].name`；对外的 new-api `/v1/models` 保持现有 OpenAI 格式。

## 计费与错误

截至 2026-09-21，官方价格为输入 **$0.042/百万 token**，输出免费。
三个预置模型的默认输入倍率为 `0.021`、输出倍率为 `0`，管理员可覆盖。
加载旧配置时仅补齐缺失默认值，不覆盖已有价格（包括零价格）。
预扣估算包含 state 和全部 questions，最终按上游 `usage.input_tokens` 和 `usage.output_tokens` 结算。
上游失败沿用网关重试策略，最终返回原生 JSON 错误和 `Retry-After`，错误详情仍受渠道设置控制。
无效响应或缺失必需 token usage 返回上游响应异常并退款，不使用估算值结算。

## 验证

普通测试不访问网络。真实测试需显式设置 `TYPESAFE_LIVE_TEST=1` 和 `TYPESAFE_API_KEY`，运行：

```bash
go test ./controller -run '^TestTypeSafeLiveGateway$' -count=1 -v
```

真实测试通过本地 HTTP 网关、临时数据库和 new-api 测试令牌调用上游，会产生少量推理用量。密钥不要写入文件。

参考：[API 文档](https://docs.typesafe.ai/api)、[模型与价格](https://docs.typesafe.ai/models)。
