# GMICLOUD HY 图片生成

模型 ID：`hy-image-v3.5-preview`，渠道类型：`GMI Cloud`（67）。

## 模型发现与上游协议

后台“获取模型”合并 LLM `/v1/models` 与 Request Queue `/api/v1/ie/requestqueue/apikey/models` 的结果，并仅保留已实现的任务模型。HY 已加入支持列表和默认候选；上游列表未返回时不会伪造实时可用性。

2026-09-26 查询模型详情确认：HY 虽然使用 Request Queue 地址，实际为 **同步提交**，通常等待 10–60 秒直接返回最终图片；GET 查询仍然可用。应以专属模型详情为准，而非通用队列文档。

默认 LLM 地址 `https://api.gmi-serving.com` 自动切换任务地址至 `https://console.gmicloud.ai`。自定义地址保留，用于反向代理或测试上游。

## 同步调用

```bash
curl "$NEW_API_URL/v1/images/generations" \
  -H "Authorization: Bearer $NEW_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"model":"hy-image-v3.5-preview","prompt":"A misty mountain village at sunrise","size":"1920x1080","n":1,"response_format":"url"}'
```

成功返回 OpenAI 图片格式：

```json
{"created":1772184500,"data":[{"url":"https://example.com/image.png","b64_json":"","revised_prompt":""}]}
```

- `prompt` 必填；`size` 可省略或为空字符串（上游 Auto）。支持 `seed`、`generate_max_pixels`，显式 `seed: 0` 保留。
- `n` 只能缺省或为 `1`；不支持 `stream: true` 或 `/v1/images/edits`。
- `response_format` 支持 `url`（默认）、`b64_json`。Base64 转换遵守站点文件下载的 SSRF、端口和大小限制。
- 同步等待采用正值 `RELAY_TIMEOUT`，未配置时为 180 秒；反向代理和客户端也需要足够的读取超时。
- HTTP 504 的错误码为 `image_task_wait_timeout`，错误中包含 `task_id`，响应头也提供 `X-New-Api-Task-Id`。继续查询任务，不要重新提交。

## 异步调用

```bash
curl "$NEW_API_URL/v1/images/tasks" \
  -H "Authorization: Bearer $NEW_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"model":"hy-image-v3.5-preview","payload":{"prompt":"A misty mountain village at sunrise","size":"1920x1080","seed":0}}'
```

立即返回本地公开任务 ID：

```json
{"id":"task_xxx","task_id":"task_xxx","model":"hy-image-v3.5-preview","status":"queued"}
```

`payload` 按上游协议透传，保留显式 `0/false`。可使用上游支持的参考图等扩展参数，但不将它声明为 OpenAI 图片编辑协议。

```bash
curl "$NEW_API_URL/v1/images/tasks/task_xxx" \
  -H "Authorization: Bearer $NEW_API_TOKEN"
```

查询沿用任务响应 `{ "code": "success", "data": { ... } }`，读取 `data.status`、`data.result_url` 和 `data.data.outcome.media_urls`。只允许查询当前用户拥有的 HY 图片任务。

## 生命周期与计费

两种入口共用任务表、价格配置和失败退款。任务日志中可查看图片生成状态、错误、图片预览和下载链接，不会被误识别为音频。

请求先持久化为本地任务，再由后台提交上游。客户端断开或同步等待超时不取消生成。未开始提交的任务可在重启后恢复；已经领取、但因崩溃或网络错误无法确认上游结果的任务不会自动重发，以免重复生成。此类任务需核对上游控制台，失联的处理中任务沿用 `TASK_TIMEOUT_MINUTES` 超时处理。

后台只使用顶层 `request_id` 查询上游，`outcome.request_id` 仅为供应商追踪标识。提交成功即完成、返回排队后轮询完成，以及失败均使用同一套并发安全的状态更新和退款流程。

**不内置“永久免费”的零价或倍率。** 2026-09-26 的模型详情标价为 ≤2K 每张 $0.024、>2K 每张 $0.032，`percentage_discount: 0`。账户活动优惠可能不同，调用成功本身不能证明未扣费。管理员应在后台设置该模型的按次价格；上游费用与本站向用户收取的额度是两回事。

## 测试

默认 Go 测试全部使用模拟上游，不生成真实图片。

真实验收需在进程环境中设置 `GMICLOUD_HY_LIVE_TEST=1`、`GMICLOUD_API_KEY`，再运行：

```bash
go test ./controller -run '^TestGMICloudHYLive$' -count=1 -v -timeout 8m
```

此测试经本地网关分别调用同步和异步入口，最多生成两张 1024×1024 图片；第一次失败即停止，不自动重试创建。需先确认当前价格与预算，测试后清除环境变量。不要把密钥写入源码、测试文件或提交。
