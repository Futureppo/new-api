# OpenAI-local PPT/PSD API 文档

## 基础信息

服务地址：

```text
https://api.futureppo.top
```

鉴权方式：

```http
Authorization: Bearer <API_KEY>
Content-Type: application/json
```

PPT/PSD 使用专用端点调用，不需要通过 `/v1/chat/completions`。

对应权限模型：

| 功能 | 权限模型 |
| --- | --- |
| PPT 生成 | `gpt-image-2-ppt` |
| PSD 生成 | `gpt-image-2-psd` |

请求体里通常不需要传 `model`。

## 创建 PPT 任务

```http
POST /v1/ppt/generations
```

### 请求参数

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `prompt` | string | 建议填写 | PPT 需求描述，例如主题、页数、风格、内容结构。 |
| `base64_images` | string[] | 否 | 参考图片，支持 data URL 或 base64。 |
| `client_task_id` | string | 否 | 客户端幂等任务 ID，重复提交同 ID 会返回已有任务。 |

### 请求示例

```bash
curl https://api.futureppo.top/v1/ppt/generations \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <API_KEY>" \
  -d '{
    "prompt": "制作一份 5 页以内的产品介绍 PPT，风格简洁商务，包含标题页、产品亮点、应用场景、总结页",
    "base64_images": [],
    "client_task_id": "ppt-demo-001"
  }'
```

### 成功响应示例

```json
{
  "id": "task_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "taskId": "task_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "status": "queued",
  "kind": "ppt",
  "created_at": "2026-06-28 18:59:50",
  "updated_at": "2026-06-28 18:59:50"
}
```

## 创建 PSD 任务

```http
POST /v1/psd/generations
```

### 请求参数

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `prompt` | string | 建议填写 | PSD 拆分与合成要求，例如保留图层、位置、背景、素材等。 |
| `base64_images` | string[] | 是 | 至少一张参考图，支持 data URL 或 base64。 |
| `client_task_id` | string | 否 | 客户端幂等任务 ID。 |

### 请求示例

```bash
curl https://api.futureppo.top/v1/psd/generations \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <API_KEY>" \
  -d '{
    "prompt": "按原图位置拆分海报元素并合成可编辑 PSD，尽量保留文字、图形、背景分层",
    "base64_images": [
      "data:image/png;base64,..."
    ],
    "client_task_id": "psd-demo-001"
  }'
```

### 成功响应示例

```json
{
  "id": "task_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "taskId": "task_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "status": "queued",
  "kind": "psd",
  "created_at": "2026-06-28 19:00:10",
  "updated_at": "2026-06-28 19:00:10"
}
```

## 查询任务状态

```http
GET /v1/editable-file-tasks?ids=<task_id>
```

支持一次查询多个任务：

```http
GET /v1/editable-file-tasks?ids=task_id_1,task_id_2
```

### 请求示例

```bash
curl "https://api.futureppo.top/v1/editable-file-tasks?ids=task_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" \
  -H "Authorization: Bearer <API_KEY>"
```

### 响应示例

```json
{
  "items": [
    {
      "id": "task_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
      "taskId": "task_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
      "status": "success",
      "kind": "ppt",
      "created_at": "2026-06-28T18:59:50Z",
      "updated_at": "2026-06-28T19:03:33Z",
      "result": {
        "primary_url": "https://gptimage.futureppo.top/files/ppt/xxx/result.pptx",
        "zip_url": "https://gptimage.futureppo.top/files/ppt/xxx/assets.zip"
      }
    }
  ],
  "missing_ids": []
}
```

### 状态说明

| 状态 | 说明 |
| --- | --- |
| `queued` | 排队中 |
| `running` | 生成中 |
| `success` | 成功 |
| `error` | 失败 |

### 成功后的下载字段

| 字段 | 说明 |
| --- | --- |
| `result.primary_url` | 主文件下载地址，PPT 为 `.pptx`，PSD 为 `.psd`。 |
| `result.zip_url` | 素材包下载地址，一般为 `.zip`。 |

下载时直接访问返回的 URL 即可，不需要手动拼接。

## 轮询建议

建议每 5 到 15 秒查询一次任务状态，直到 `status` 为 `success` 或 `error`。

```js
async function waitTask(taskId, apiKey) {
  while (true) {
    const res = await fetch(
      `https://api.futureppo.top/v1/editable-file-tasks?ids=${encodeURIComponent(taskId)}`,
      {
        headers: {
          Authorization: `Bearer ${apiKey}`,
        },
      },
    );

    const data = await res.json();
    const task = data.items?.[0];

    if (!task) throw new Error('Task not found');
    if (task.status === 'success') return task.result;
    if (task.status === 'error') throw new Error(task.error || 'Task failed');

    await new Promise((resolve) => setTimeout(resolve, 10000));
  }
}
```

## 常见错误

### 没有模型权限

```json
{
  "error": {
    "message": "This token has no access to model gpt-image-2-ppt",
    "type": "new_api_error"
  }
}
```

表示当前 API Key 没有对应模型权限。

| 功能 | 需要开放的模型 |
| --- | --- |
| PPT | `gpt-image-2-ppt` |
| PSD | `gpt-image-2-psd` |

### PSD 缺少图片

PSD 任务必须提供至少一张图片：

```json
{
  "prompt": "按原图拆分为可编辑 PSD",
  "base64_images": [
    "data:image/png;base64,..."
  ]
}
```
