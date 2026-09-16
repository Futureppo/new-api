# Kilo

渠道类型：`70`。默认网关地址：`https://api.kilo.ai/api/gateway`。

支持 OpenAI Chat Completions 普通和 SSE 流式请求，工具调用与推理字段按客户端请求传递。本渠道仅声明聊天端点，不提供原生 Responses、图片或音频接口。

## 地址和鉴权

- 上游聊天：`POST /chat/completions`，模型目录：`GET /models`，路径不追加 `/v1`。
- 自定义 API 地址填写网关前缀，可另行设置自定义模型列表 URL。
- 密钥模式使用 `Authorization: Bearer <Kilo API Key>`。
- 匿名模式不发送 Authorization，包括渠道的自定义请求头覆盖。该模式仅适用于 Kilo 允许匿名调用的免费模型，限额以上游实际响应为准；不会在密钥失败时自动切换匿名。
- 新建界面默认使用密钥模式，手动开启“匿名调用”后可使用空密钥；匿名模式只支持单渠道创建。编辑已有渠道时用该开关切换，保存的密钥保留，关闭匿名模式后可继续使用。匿名模式不改变本站用户的令牌鉴权。
- 创建及模型拉取接口会尊重显式的 `kilo_anonymous_enabled: false`，此时空密钥会报错；仅在未指定该选项时兼容旧版空密钥匿名调用。

## 免费模型管理

“自动维护 Kilo 免费模型”和“简化 Kilo 免费模型名称”默认关闭。

开启自动维护后，仅拉取 `isFree: true` 的模型，并沿用现有巡检任务（默认每 30 分钟）。手动检测先生成待应用变更，可通过现有应用操作立即更新。模型下架或转收费时删除受管模型，其他手动配置模型保留。输入/输出单价均为零不代表免费，`-alpha` 名称也不作为免费判据。

启用名称简化后，`provider/model:free` 展示为 `model`，通过现有模型映射恢复上游完整 ID。`kilo-auto/free`、`openrouter/free` 和其他格式的模型保持原名。名称冲突、手动映射优先于自动简化；忽略规则同时匹配原始 ID 与简化名称。

关闭自动维护停止后续免费模型同步；保持自动维护开启但关闭简化，下次同步恢复完整 ID。仅删除由系统生成且未被手动修改的映射。异常模型列表、缺少 `isFree` 标记或空免费列表会报告失败并保留当前配置。

免费标记只用于模型管理。站内扣费仍由现有模型与分组定价决定，不会自动设置零价格。

## 设置和验证

设置保存在渠道的 `settings` JSON：

- `kilo_anonymous_enabled`
- `kilo_auto_sync_free_models_enabled`
- `kilo_free_model_name_simplification_enabled`

受管模型、生成映射和待应用映射由后端维护。模型拉取接口支持可选 `kilo_free_only`（POST 请求字段 / GET 查询参数），不改变 `data: string[]` 返回结构，也不修改已保存开关。

官方流式接口支持 `stream_options.include_usage`，末尾 usage 块通过现有用量处理链处理。

参考：[接口文档](https://kilo.ai/docs/gateway/api-reference)、[鉴权](https://kilo.ai/docs/gateway/authentication)、[流式响应](https://kilo.ai/docs/gateway/streaming)。
