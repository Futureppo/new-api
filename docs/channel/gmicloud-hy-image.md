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

- `prompt` 必填；`size` 可省略、为空字符串或为 `auto`（转为上游 Auto）。支持 `seed`、`generate_max_pixels`，显式 `seed: 0` 保留。
- 4K 必须显式设置 `size: "4096x4096"`（正方形）、`3840x2160` 或 `2160x3840`。网关原样传递尺寸，不降采样。`generate_max_pixels` **仅用于 Auto**，最高 `4194304`（2K）；不能用它代替 4K 的 `size`，也不能只写 `size: "4096"`。
- `n` 只能缺省或为 `1`；不支持 `stream: true`。
- `response_format` 支持 `url`（默认）、`b64_json`。Base64 转换遵守站点文件下载的 SSRF、端口和大小限制。
- 同步等待采用正值 `RELAY_TIMEOUT`，未配置时为 180 秒；反向代理和客户端也需要足够的读取超时。
- HTTP 504 的错误码为 `image_task_wait_timeout`，错误中包含 `task_id`，响应头也提供 `X-New-Api-Task-Id`。继续查询任务，不要重新提交。

## 参考图改图

`POST /v1/images/edits` 支持 OpenAI JSON 编辑请求的 URL 子集，结果与同步生图相同：

```bash
curl "$NEW_API_URL/v1/images/edits" \
  -H "Authorization: Bearer $NEW_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"model":"hy-image-v3.5-preview","prompt":"把红苹果改成绿苹果，保留构图","images":[{"image_url":"https://your-cdn.example/reference.png"}],"size":"4096x4096","response_format":"url"}'
```

- `images[].image_url` 转为 GMI 的 `payload.image` URL 数组；也接受 GMI 扩展格式 `image: "https://..."` 或 `image: ["https://...", "https://..."]`，不可同时传 `image` 和 `images`。
- 最多 5 张参考图；每张小于 20MB，由上游直接下载，URL 必须无需鉴权且在供应商所在区域可访问。
- 同步 `/v1/images/generations` 也接受上述参考图字段；异步 `/v1/images/tasks` 放在 `payload` 内。带参考图的任务记为 `image_edit`，在任务日志中显示图片编辑和结果预览。
- 编辑入口缺少参考图会返回 400，不会静默变成文生图。超过 5 张、错误 URL、`mask`、`file_id`、Data URL/Base64 和 multipart 文件上传也会明确报错。HY 文档只声明公开 URL 参考图，不支持遮罩局部重绘；网关不会把上传文件擅自公开托管。
- OpenAI JSON 编辑格式参考：[官方图片文档](https://developers.openai.com/api/docs/guides/image-generation#edit-images)。这是其 URL 输入子集，不代表 HY 具备 OpenAI 全部编辑功能。

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

`payload` 保留上游扩展字段及显式 `0/false`；参考图字段统一转换为上游 `image` 数组，`size: "auto"` 转为空字符串。

```bash
curl "$NEW_API_URL/v1/images/tasks/task_xxx" \
  -H "Authorization: Bearer $NEW_API_TOKEN"
```

查询沿用任务响应 `{ "code": "success", "data": { ... } }`，读取 `data.status`、`data.result_url` 和 `data.data.outcome.media_urls`。只允许查询当前用户拥有的 HY 图片任务。

## 生命周期与计费

同步生成、同步编辑和异步入口共用任务表、价格配置和失败退款。任务日志中可查看图片生成/编辑状态、错误、图片预览和下载链接，不会被误识别为音频。

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

4K 和改图验收使用独立开关 `GMICLOUD_HY_4K_EDIT_LIVE_TEST=1`：

```bash
go test ./controller -run '^TestGMICloudHY4KEditLive$' -count=1 -v -timeout 8m
```

最多先生成一张 4096×4096，再用其 URL 提交一次 4096×4096 改图；检查真实返回尺寸、成功状态和任务 action，不下载或公开托管图片。任一步失败后不提交下一步。

2026-09-26 实测：同步生图 HTTP 200 / SUCCESS，4096×4096，约 48 秒；JSON 参考图编辑 HTTP 200 / SUCCESS，4096×4096，约 53 秒，任务 action 为 `image_edit`。未复现显式 `size: "4096x4096"` 被拒绝的问题。这验证了接口和尺寸，不代表已核实账户最终费用。
