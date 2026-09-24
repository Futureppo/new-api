# Cline

选择 **Cline** 渠道并填写 API Key。默认地址为 `https://api.cline.bot/api`，也支持以 `/v1` 结尾的自定义地址。通过 `/v1/chat/completions` 转发普通、流式对话和工具调用；流式请求支持 `stream_options.include_usage`。普通响应的 `{success, data}` 包装会被转换成标准 OpenAI 响应。

## 免费模型自动更新

“自动更新 Cline 免费模型列表”默认开启，包括通过 API 创建且未填写该设置的渠道。系统读取 Cline 官方 `/v1/ai/cline/recommended-models` 的 `free[].id`，使用完整模型 ID，不根据名称后缀猜测免费资格，也不加入订阅模型。

- 自动加入新免费模型，移除此前受管且已退出免费专区的模型。
- 保留其他手动模型和手动映射，支持现有的精确及 `regex:` 忽略列表。
- 关闭开关后保留当前模型，停止免费模型同步；“获取模型”改为读取 `/v1/models`。
- 自定义模型列表地址在免费同步开启时必须返回 `{"free":[{"id":"model-id"}]}`。地址、代理和自定义请求头均使用渠道配置。
- 请求失败、响应无效或免费列表为空时保留当前模型，记录错误并在后续巡检重试。

主节点复用现有模型巡检，默认每 30 分钟检查。新建、复制、重新开启同步或修改连接配置时检查一次；仍受 `CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_ENABLED` 和主节点限制。周期由 `CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_INTERVAL_MINUTES` 控制。

设置存储在渠道 `settings` JSON：`cline_auto_sync_free_models_enabled` 缺省或 `null` 为开启，`false` 为关闭。`cline_free_model_managed_models` 为后端维护的状态。获取模型接口支持临时 `cline_free_only` 参数，不改变已保存设置。

免费专区说明上游模型免费；站内计费继续使用现有定价，新增模型仍需满足站内计费配置要求。

## 验证

运行 `go test ./controller ./relay/channel/openai ./relay/common ./common ./dto ./model` 和前端 `bun test src/hooks/channels/upstreamUpdateUtils.test.js`。

显式设置环境变量 `CLINE_LIVE_TEST=1`、`CLINE_API_KEY` 后，运行 `go test ./relay/channel/openai -run '^TestClineLiveChat$' -count=1 -v`，会从实时官方免费目录选择模型并执行两次最小对话请求。不要将 Key 写入仓库。
