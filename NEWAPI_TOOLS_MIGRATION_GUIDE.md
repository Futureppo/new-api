# NewAPI-Tool 全量迁移到 NewAPI「增强功能」实施文档

本文档用于指导另一个开发对话或开发者，将 `new_api_tools` 的能力迁移进 `new-api` 本体，并在 NewAPI 默认前端管理员侧边栏中新增一级入口 `增强功能`。

目标不是保留一个独立中间件，而是把 `new_api_tools` 中有价值的统计、审计、批处理、风控、模型状态和系统工具能力，按 NewAPI 现有认证、数据库、配置、路由、前端组件体系重新实现。

## 1. 迁移结论

- 迁移目标入口：NewAPI 管理员侧边栏新增 `增强功能`，默认进入 `/enhancements/dashboard`。
- 后端目标前缀：`/api/enhancements/*`。
- 默认权限：`middleware.AdminAuth()`。
- 敏感配置与系统级操作权限：优先 `middleware.RootAuth()`；若产品上必须允许普通管理员访问，则仅返回脱敏字段，并把危险操作隐藏或二次确认。
- 公开模型状态嵌入接口：保留无鉴权接口 `/api/enhancements/model-status/embed/*`，只返回非敏感状态数据。
- 后端基准：以 `new_api_tools/backend` Go 实现为准；`backend-py` 只用于核对接口完整性，不作为运行时依赖。
- 前端基准：重写到 `new-api/web/default`，使用 TanStack Router、React Query、shadcn/Radix、lucide、VChart；不要搬运独立 Vite 应用，不要引入 ECharts。
- 不迁移内容：独立登录/JWT、独立 Docker、独立端口、独立数据库连接配置、独立前端构建、浏览器本地历史表。

## 2. 当前项目结构与关键文件

工作区内两个项目：

- `new-api/`：目标项目。
- `new_api_tools/`：待迁移能力来源。

NewAPI 后端关键落点：

- `new-api/router/api-router.go`：注册 `/api` 路由组。
- `new-api/controller/`：HTTP handler 层。
- `new-api/service/`：业务服务层，建议新增增强功能聚合服务。
- `new-api/model/`：GORM 模型与 `model.DB`、`model.LOG_DB`。
- `new-api/setting/`：全局配置结构，基于 `setting/config.GlobalConfig`。
- `new-api/common/`：`ApiSuccess`、`ApiError`、Redis、主节点判断等通用能力。
- `new-api/middleware/auth.go`：`UserAuth`、`AdminAuth`、`RootAuth`。
- `new-api/main.go`：后台任务入口。

NewAPI 前端关键落点：

- `new-api/web/default/src/routes/_authenticated/`：认证后路由。
- `new-api/web/default/src/features/`：功能模块。
- `new-api/web/default/src/hooks/use-sidebar-data.ts`：侧边栏菜单项。
- `new-api/web/default/src/hooks/use-sidebar-config.ts`：侧边栏模块开关映射。
- `new-api/web/default/src/lib/api.ts`：统一 axios 实例。
- `new-api/web/default/src/lib/roles.ts`：前端角色常量。
- `new-api/web/default/src/features/system-settings/maintenance/`：系统设置中的侧边栏模块开关 UI。
- `new-api/web/default/src/i18n/locales/`：多语言文案。

new_api_tools 后端关键来源：

- `new_api_tools/backend/cmd/server/main.go`：当前独立服务的路由注册和后台任务。
- `new_api_tools/backend/internal/handler/`：接口层。
- `new_api_tools/backend/internal/service/`：核心业务逻辑。
- `new_api_tools/backend/internal/cache/`：Redis + 本地缓存包装。
- `new_api_tools/backend/internal/database/`：sqlx 数据访问、索引维护、MySQL/PostgreSQL 兼容逻辑。
- `new_api_tools/backend/internal/config/`：独立服务配置，迁移后不应保留。

new_api_tools 前端关键来源：

- `new_api_tools/frontend/src/App.tsx`：当前一级 Tab 定义。
- `new_api_tools/frontend/src/components/`：各功能 UI 来源。
- `new_api_tools/frontend/public/china.json`、`world.json`：地图资源，可选择性迁移。

## 3. 功能边界

`增强功能` 应承载 new_api_tools 额外提供的能力，不重复 NewAPI 已有 CRUD 页面。

需要迁入的二级 Tab：

1. 增强仪表盘
2. 充值审计
3. 兑换码增强
4. 用户增强
5. 令牌审计
6. 风控中心
7. IP 分析
8. 日志分析
9. 模型状态
10. 自动分组
11. AI 封禁
12. 系统工具

能力边界：

- NewAPI 已有页面继续负责基础管理，例如用户、兑换码、模型、日志、充值记录。
- `增强功能` 只做额外的统计、审计、批量处理、风险分析、可视化与自动化。
- `History.tsx` 是浏览器本地历史，不是后端能力。迁移时只做“兑换码生成结果临时面板”，不要新增数据库表。
- AI 自动封禁默认关闭，且默认 dry-run 开启；真实封禁必须由管理员显式配置模型和 Key 后开启。

## 4. 后端总体架构

建议新增后端结构：

```text
new-api/
  controller/
    enhancement_dashboard.go
    enhancement_topup.go
    enhancement_redemption.go
    enhancement_user.go
    enhancement_token.go
    enhancement_risk.go
    enhancement_ip.go
    enhancement_analytics.go
    enhancement_model_status.go
    enhancement_auto_group.go
    enhancement_ai_ban.go
    enhancement_system.go
    enhancement_linuxdo.go
  service/
    enhancement/
      dashboard.go
      topup.go
      redemption.go
      user.go
      token.go
      risk.go
      ip.go
      analytics.go
      model_status.go
      auto_group.go
      ai_ban.go
      system.go
      linuxdo.go
      query.go
      types.go
  setting/
    enhancement_setting.go
```

如果团队更偏好少文件，也可以先使用 `controller/enhancements.go` 和 `service/enhancements.go` 集中实现，再在稳定后拆分。迁移期间推荐按功能拆分，减少后续维护成本。

路由注册建议放在 `router/api-router.go`：

```go
// Public embed route, no auth.
enhancementEmbedRoute := apiRouter.Group("/enhancements/model-status/embed")
{
    controller.RegisterEnhancementModelStatusEmbedRoutes(enhancementEmbedRoute)
}

enhancementRoute := apiRouter.Group("/enhancements")
enhancementRoute.Use(middleware.AdminAuth())
{
    controller.RegisterEnhancementRoutes(enhancementRoute)
}
```

敏感子路由可以在 handler 内额外包 `RootAuth()`，或拆成单独 group：

```go
enhancementRootRoute := apiRouter.Group("/enhancements")
enhancementRootRoute.Use(middleware.RootAuth())
{
    enhancementRootRoute.POST("/system/indexes/ensure", controller.EnsureEnhancementIndexes)
    enhancementRootRoute.POST("/ai-ban/config", controller.SaveEnhancementAIBanConfig)
}
```

注意：公开 embed 路由必须在产品设计上保证不返回 API Key、渠道 Key、内部错误栈、用户隐私字段、后台任务配置等敏感数据。

公开 embed 的额外安全基线：

- 必须受 `public_embed_enabled` 控制，默认关闭；关闭时所有公开 embed 接口返回 404 或 403，不返回配置细节。
- 公开接口必须使用专用 response DTO，不得复用管理员接口 DTO 后再删除字段。
- `GET /models` 和 `GET /status/all` 只能返回已配置为公开展示的 `selected_models`，不能返回全站模型、渠道、abilities 或 token group 的完整内部列表。
- `POST /status/multiple` 和 `POST /status/batch` 必须限制 body 大小、模型数量、时间窗口和刷新频率；建议公开接口单次最多 50 个模型，最大时间窗口 24 小时。
- 公开接口必须接入全局限流或单独限流；不要因为“只读”而放开高成本聚合查询。
- 公开接口只能读取缓存或受限时间窗口的聚合结果，不应触发实时大表全量扫描。
- 不要在 `RegisterEnhancementRoutes` 中重复注册 `/model-status/embed/*` 管理员路由，避免公开路由和鉴权路由路径重叠导致误挂鉴权或重复注册。

## 5. API 迁移映射

所有迁移后的接口统一挂到 `/api/enhancements` 下。下表中的旧接口来自 `new_api_tools/backend/internal/handler/*.go`。

注意：本节表格只表示路径迁移关系，不表示最终权限。最终权限以第 15 节安全矩阵为准；写操作、批量操作、外部请求、索引、清缓存、AI、公开 embed 配置等必须在后端单独校验。

### 5.1 认证与健康检查

| new_api_tools 旧接口 | 迁移策略 |
|---|---|
| `POST /api/auth/login` | 不迁移，使用 NewAPI 原生登录态和 `AdminAuth` |
| `POST /api/auth/logout` | 不迁移 |
| `GET /api/health` | 不迁移或合并到 NewAPI 现有 `/api/status` |
| `GET /api/health/db` | 可在系统工具中展示 DB 状态，不单独暴露旧路径 |

### 5.2 增强仪表盘

| 旧接口 | 新接口 |
|---|---|
| `GET /api/dashboard/overview` | `GET /api/enhancements/dashboard/overview` |
| `GET /api/dashboard/usage` | `GET /api/enhancements/dashboard/usage` |
| `GET /api/dashboard/models` | `GET /api/enhancements/dashboard/models` |
| `GET /api/dashboard/trends/daily` | `GET /api/enhancements/dashboard/trends/daily` |
| `GET /api/dashboard/trends/hourly` | `GET /api/enhancements/dashboard/trends/hourly` |
| `GET /api/dashboard/top-users` | `GET /api/enhancements/dashboard/top-users` |
| `GET /api/dashboard/channels` | `GET /api/enhancements/dashboard/channels` |
| `POST /api/dashboard/cache/invalidate` | `POST /api/enhancements/dashboard/cache/invalidate` |
| `GET /api/dashboard/refresh-estimate` | `GET /api/enhancements/dashboard/refresh-estimate` |
| `GET /api/dashboard/system-info` | `GET /api/enhancements/dashboard/system-info` |
| `GET /api/dashboard/ip-distribution` | `GET /api/enhancements/dashboard/ip-distribution` |

### 5.3 充值审计

| 旧接口 | 新接口 |
|---|---|
| `GET /api/top-ups` | `GET /api/enhancements/top-ups` |
| `GET /api/top-ups/statistics` | `GET /api/enhancements/top-ups/statistics` |
| `GET /api/top-ups/payment-methods` | `GET /api/enhancements/top-ups/payment-methods` |
| `GET /api/top-ups/:id` | `GET /api/enhancements/top-ups/:id` |

### 5.4 兑换码增强

| 旧接口 | 新接口 |
|---|---|
| `POST /api/redemptions/generate` | `POST /api/enhancements/redemptions/generate` |
| `GET /api/redemptions` | `GET /api/enhancements/redemptions` |
| `GET /api/redemptions/statistics` | `GET /api/enhancements/redemptions/statistics` |
| `POST /api/redemptions/batch-delete` | `POST /api/enhancements/redemptions/batch-delete` |
| `POST /api/redemptions/batch` | `POST /api/enhancements/redemptions/batch-delete` |
| `DELETE /api/redemptions/batch` | `DELETE /api/enhancements/redemptions/batch`，可保留兼容 |
| `DELETE /api/redemptions/:id` | `DELETE /api/enhancements/redemptions/:id` |

### 5.5 用户增强

| 旧接口 | 新接口 |
|---|---|
| `GET /api/users/activity-stats` | `GET /api/enhancements/users/activity-stats` |
| `GET /api/users/stats` | `GET /api/enhancements/users/activity-stats`，兼容别名可保留 |
| `GET /api/users/banned` | `GET /api/enhancements/users/banned` |
| `GET /api/users` | `GET /api/enhancements/users` |
| `DELETE /api/users/:user_id` | `DELETE /api/enhancements/users/:user_id` |
| `POST /api/users/batch-delete` | `POST /api/enhancements/users/batch-delete` |
| `GET /api/users/soft-deleted/count` | `GET /api/enhancements/users/soft-deleted/count` |
| `POST /api/users/soft-deleted/purge` | `POST /api/enhancements/users/soft-deleted/purge` |
| `POST /api/users/:user_id/ban` | `POST /api/enhancements/users/:user_id/ban` |
| `POST /api/users/:user_id/unban` | `POST /api/enhancements/users/:user_id/unban` |
| `GET /api/users/:user_id/invited` | `GET /api/enhancements/users/:user_id/invited` |
| `POST /api/users/tokens/:token_id/disable` | `POST /api/enhancements/users/tokens/:token_id/disable` |

### 5.6 令牌审计

| 旧接口 | 新接口 |
|---|---|
| `GET /api/tokens` | `GET /api/enhancements/tokens` |
| `GET /api/tokens/statistics` | `GET /api/enhancements/tokens/statistics` |
| `GET /api/tokens/groups` | `GET /api/enhancements/tokens/groups` |

### 5.7 风控中心

| 旧接口 | 新接口 |
|---|---|
| `GET /api/risk/leaderboards` | `GET /api/enhancements/risk/leaderboards` |
| `GET /api/risk/users/:user_id/analysis` | `GET /api/enhancements/risk/users/:user_id/analysis` |
| `GET /api/risk/ban-records` | `GET /api/enhancements/risk/ban-records` |
| `GET /api/risk/token-rotation` | `GET /api/enhancements/risk/token-rotation` |
| `GET /api/risk/affiliated-accounts` | `GET /api/enhancements/risk/affiliated-accounts` |
| `GET /api/risk/same-ip-registrations` | `GET /api/enhancements/risk/same-ip-registrations` |

### 5.8 IP 分析

| 旧接口 | 新接口 |
|---|---|
| `GET /api/ip/stats` | `GET /api/enhancements/ip/stats` |
| `GET /api/ip/shared` | `GET /api/enhancements/ip/shared` |
| `GET /api/ip/shared-ips` | `GET /api/enhancements/ip/shared`，兼容别名可保留 |
| `GET /api/ip/multi-ip-tokens` | `GET /api/enhancements/ip/multi-ip-tokens` |
| `GET /api/ip/multi-ip-users` | `GET /api/enhancements/ip/multi-ip-users` |
| `POST /api/ip/enable-all-recording` | `POST /api/enhancements/ip/enable-all-recording` |
| `POST /api/ip/enable-all` | `POST /api/enhancements/ip/enable-all-recording`，兼容别名可保留 |
| `GET /api/ip/lookup/:ip` | `GET /api/enhancements/ip/lookup/:ip` |
| `GET /api/ip/users/:user_id/ips` | `GET /api/enhancements/ip/users/:user_id/ips` |
| `GET /api/ip/indexes` | `GET /api/enhancements/ip/indexes` |
| `POST /api/ip/indexes/ensure` | `POST /api/enhancements/ip/indexes/ensure` |
| `GET /api/ip/geo/:ip` | `GET /api/enhancements/ip/geo/:ip` |
| `POST /api/ip/geo/batch` | `POST /api/enhancements/ip/geo/batch` |

### 5.9 日志分析

| 旧接口 | 新接口 |
|---|---|
| `GET /api/analytics/state` | `GET /api/enhancements/analytics/state` |
| `POST /api/analytics/process` | `POST /api/enhancements/analytics/process` |
| `POST /api/analytics/batch-process` | `POST /api/enhancements/analytics/batch-process` |
| `POST /api/analytics/batch` | `POST /api/enhancements/analytics/batch-process`，兼容别名可保留 |
| `GET /api/analytics/ranking/requests` | `GET /api/enhancements/analytics/ranking/requests` |
| `GET /api/analytics/ranking/quota` | `GET /api/enhancements/analytics/ranking/quota` |
| `GET /api/analytics/users/requests` | `GET /api/enhancements/analytics/ranking/requests`，兼容别名可保留 |
| `GET /api/analytics/users/quota` | `GET /api/enhancements/analytics/ranking/quota`，兼容别名可保留 |
| `GET /api/analytics/models` | `GET /api/enhancements/analytics/models` |
| `GET /api/analytics/summary` | `GET /api/enhancements/analytics/summary` |
| `POST /api/analytics/reset` | `POST /api/enhancements/analytics/reset` |
| `GET /api/analytics/sync-status` | `GET /api/enhancements/analytics/sync-status` |
| `POST /api/analytics/check-consistency` | `POST /api/enhancements/analytics/check-consistency` |

### 5.10 模型状态

| 旧接口 | 新接口 |
|---|---|
| `GET /api/model-status/time-windows` | `GET /api/enhancements/model-status/time-windows` |
| `GET /api/model-status/models` | `GET /api/enhancements/model-status/models` |
| `GET /api/model-status/status/:model_name` | `GET /api/enhancements/model-status/status/:model_name` |
| `POST /api/model-status/status/multiple` | `POST /api/enhancements/model-status/status/multiple` |
| `POST /api/model-status/status/batch` | `POST /api/enhancements/model-status/status/batch` |
| `GET /api/model-status/status/all` | `GET /api/enhancements/model-status/status/all` |
| `GET /api/model-status/selected` | `GET /api/enhancements/model-status/selected` |
| `PUT /api/model-status/selected` | `PUT /api/enhancements/model-status/selected` |
| `GET /api/model-status/config/selected` | `GET /api/enhancements/model-status/config/selected` |
| `POST /api/model-status/config/selected` | `POST /api/enhancements/model-status/config/selected` |
| `GET /api/model-status/config/time-window` | `GET /api/enhancements/model-status/config/time-window` |
| `PUT /api/model-status/config/time-window` | `PUT /api/enhancements/model-status/config/time-window` |
| `GET /api/model-status/config/theme` | `GET /api/enhancements/model-status/config/theme` |
| `PUT /api/model-status/config/theme` | `PUT /api/enhancements/model-status/config/theme` |
| `GET /api/model-status/config/refresh-interval` | `GET /api/enhancements/model-status/config/refresh-interval` |
| `PUT /api/model-status/config/refresh-interval` | `PUT /api/enhancements/model-status/config/refresh-interval` |
| `GET /api/model-status/config/sort-mode` | `GET /api/enhancements/model-status/config/sort-mode` |
| `PUT /api/model-status/config/sort-mode` | `PUT /api/enhancements/model-status/config/sort-mode` |
| `PUT /api/model-status/config/custom-order` | `PUT /api/enhancements/model-status/config/custom-order` |
| `GET /api/model-status/config/groups` | `GET /api/enhancements/model-status/config/groups` |
| `PUT /api/model-status/config/groups` | `PUT /api/enhancements/model-status/config/groups` |
| `GET /api/model-status/config/site-title` | `GET /api/enhancements/model-status/config/site-title` |
| `PUT /api/model-status/config/site-title` | `PUT /api/enhancements/model-status/config/site-title` |
| `GET /api/model-status/token-groups` | `GET /api/enhancements/model-status/token-groups` |

公开嵌入页接口：

| 旧公开接口 | 新公开接口 |
|---|---|
| `/api/model-status/embed/*` | `/api/enhancements/model-status/embed/*` |

公开接口仅允许：

- `GET /time-windows`
- `GET /models`
- `GET /status/:model_name`
- `POST /status/multiple`
- `POST /status/batch`
- `GET /status/all`
- `GET /config`
- `GET /config/selected`
- `GET /token-groups`

### 5.11 自动分组

| 旧接口 | 新接口 |
|---|---|
| `GET /api/auto-group/config` | `GET /api/enhancements/auto-group/config` |
| `POST /api/auto-group/config` | `POST /api/enhancements/auto-group/config` |
| `GET /api/auto-group/stats` | `GET /api/enhancements/auto-group/stats` |
| `GET /api/auto-group/groups` | `GET /api/enhancements/auto-group/groups` |
| `GET /api/auto-group/preview` | `GET /api/enhancements/auto-group/preview` |
| `GET /api/auto-group/users` | `GET /api/enhancements/auto-group/users` |
| `POST /api/auto-group/scan` | `POST /api/enhancements/auto-group/scan` |
| `POST /api/auto-group/batch-move` | `POST /api/enhancements/auto-group/batch-move` |
| `GET /api/auto-group/logs` | `GET /api/enhancements/auto-group/logs` |
| `POST /api/auto-group/revert` | `POST /api/enhancements/auto-group/revert` |

### 5.12 AI 封禁

| 旧接口 | 新接口 |
|---|---|
| `GET /api/ai-ban/config` | `GET /api/enhancements/ai-ban/config` |
| `POST /api/ai-ban/config` | `POST /api/enhancements/ai-ban/config` |
| `POST /api/ai-ban/reset-api-health` | `POST /api/enhancements/ai-ban/reset-api-health` |
| `GET /api/ai-ban/audit-logs` | `GET /api/enhancements/ai-ban/audit-logs` |
| `DELETE /api/ai-ban/audit-logs` | `DELETE /api/enhancements/ai-ban/audit-logs` |
| `GET /api/ai-ban/groups` | `GET /api/enhancements/ai-ban/groups` |
| `GET /api/ai-ban/available-groups` | `GET /api/enhancements/ai-ban/groups`，兼容别名可保留 |
| `GET /api/ai-ban/models` | `GET /api/enhancements/ai-ban/models` |
| `GET /api/ai-ban/available-models-for-exclude` | `GET /api/enhancements/ai-ban/models`，兼容别名可保留 |
| `GET /api/ai-ban/suspicious` | `GET /api/enhancements/ai-ban/suspicious` |
| `GET /api/ai-ban/suspicious-users` | `GET /api/enhancements/ai-ban/suspicious`，兼容别名可保留 |
| `POST /api/ai-ban/assess` | `POST /api/enhancements/ai-ban/assess` |
| `POST /api/ai-ban/scan` | `POST /api/enhancements/ai-ban/scan` |
| `POST /api/ai-ban/test-connection` | `POST /api/enhancements/ai-ban/test-connection` |
| `GET /api/ai-ban/whitelist` | `GET /api/enhancements/ai-ban/whitelist` |
| `POST /api/ai-ban/whitelist/add` | `POST /api/enhancements/ai-ban/whitelist/add` |
| `POST /api/ai-ban/whitelist/remove` | `POST /api/enhancements/ai-ban/whitelist/remove` |
| `GET /api/ai-ban/whitelist/search` | `GET /api/enhancements/ai-ban/whitelist/search` |
| `POST /api/ai-ban/models` | `POST /api/enhancements/ai-ban/fetch-models` |
| `POST /api/ai-ban/fetch-models` | `POST /api/enhancements/ai-ban/fetch-models` |
| `POST /api/ai-ban/test-model` | `POST /api/enhancements/ai-ban/test-model` |

### 5.13 系统工具与 LinuxDO 查询

| 旧接口 | 新接口 |
|---|---|
| `GET /api/storage/config` | 不迁移为通用 KV；改为增强功能专用配置 API |
| `GET /api/storage/cache/info` | `GET /api/enhancements/system/cache/info` |
| `GET /api/storage/cache/stats` | `GET /api/enhancements/system/cache/stats` |
| `POST /api/storage/cache/cleanup` | `POST /api/enhancements/system/cache/cleanup` |
| `DELETE /api/storage/cache` | `DELETE /api/enhancements/system/cache` |
| `GET /api/storage/info` | `GET /api/enhancements/system/storage/info` |
| `GET /api/system/scale` | `GET /api/enhancements/system/scale` |
| `POST /api/system/scale/refresh` | `POST /api/enhancements/system/scale/refresh` |
| `GET /api/system/warmup-status` | `GET /api/enhancements/system/warmup-status` |
| `GET /api/system/indexes` | `GET /api/enhancements/system/indexes` |
| `POST /api/system/indexes/ensure` | `POST /api/enhancements/system/indexes/ensure` |
| `GET /api/linuxdo/lookup/:linux_do_id` | `GET /api/enhancements/linuxdo/lookup/:id` |

## 6. 数据与表映射

迁移时优先使用 NewAPI 现有 GORM 模型和连接：

- 主库：`model.DB`
- 日志库：`model.LOG_DB`
- 成功响应：`common.ApiSuccess(c, data)`
- 错误响应：`common.ApiError(c, err)`
- Redis：`common.Redis`，只做缓存或短期队列。

涉及表：

| 表 | 用途 |
|---|---|
| `users` | 用户增强、风控、自动分组、AI 封禁、邀请关系、LinuxDO ID |
| `tokens` | 令牌审计、禁用令牌、多 IP 令牌、模型状态 token group |
| `logs` | 用量趋势、模型统计、IP 分析、风险画像、日志分析 |
| `quota_data` | 用量汇总，若为空则回退 `logs` 聚合 |
| `redemptions` | 兑换码生成、统计、批量删除 |
| `top_ups` | 充值审计 |
| `channels` | 渠道状态、模型能力、系统概览 |
| `abilities` | 可用模型、模型分组、模型状态 |
| `checkins` | 用户活跃度辅助指标，可选 |

NewAPI 现有模型中已经能覆盖多数表：

- `model.User`
- `model.Token`
- `model.Log`
- `model.Redemption`
- `model.QuotaData`
- `model.Channel`

如果个别字段未在 GORM struct 中声明，优先补充模型字段；不建议为已有表新增重复模型。复杂统计可以使用 `Table(...).Select(...).Group(...)` 或 raw SQL，但要保证 MySQL/PostgreSQL 兼容。

## 7. 配置迁移设计

不要继续把长期配置只存在 Redis。建议新增：

```text
new-api/setting/enhancement_setting.go
```

并注册到 `setting/config.GlobalConfig`。

建议配置结构：

```go
type EnhancementSetting struct {
    ModelStatus EnhancementModelStatusSetting `json:"model_status"`
    AutoGroup   EnhancementAutoGroupSetting   `json:"auto_group"`
    AIBan       EnhancementAIBanSetting       `json:"ai_ban"`
    Audit       EnhancementAuditSetting       `json:"audit"`
    GeoIP       EnhancementGeoIPSetting       `json:"geo_ip"`
    Tasks       EnhancementTaskSetting        `json:"tasks"`
}
```

建议配置字段：

模型状态：

- `enabled`
- `site_title`
- `selected_models`
- `time_window_minutes`
- `refresh_interval_seconds`
- `theme`
- `sort_mode`
- `custom_order`
- `custom_groups`
- `public_embed_enabled`

自动分组：

- `enabled`
- `dry_run`
- `mode`
- `target_group`
- `source_rules`
- `scan_interval_minutes`
- `auto_scan_enabled`
- `whitelist_user_ids`
- `last_scan_time`

AI 封禁：

- `enabled`
- `dry_run`
- `base_url`
- `api_key`
- `model`
- `scan_interval_minutes`
- `custom_prompt`
- `whitelist_ips`
- `blacklist_ips`
- `whitelist_user_ids`
- `excluded_models`
- `excluded_groups`
- `last_scan_time`

审计：

- `audit_log_retention_days`
- `auto_group_log_retention_days`
- `ai_ban_log_retention_days`

GeoIP：

- `enabled`
- `provider`
- `database_path`
- `cache_ttl_hours`
- `proxy`

后台任务：

- `ip_recording_enforce_enabled`
- `model_status_warmup_enabled`
- `scheduler_enabled`

API Key 安全规则：

- `GET /api/enhancements/ai-ban/config` 只能返回：
  - `has_api_key`
  - `masked_api_key`
  - 非敏感配置字段
- `POST /api/enhancements/ai-ban/config` 中 `api_key` 为空字符串或缺省时表示“不修改旧 Key”。
- 保存新 Key 时只写入服务端配置，不回显明文。
- 只有 Root 能查看或保存敏感配置；普通 Admin 最多看到脱敏状态。

外部请求安全规则：

- `ai_ban.base_url`、`GeoIP.provider`、`GeoIP.proxy`、`LinuxDO.proxy` 等会影响出站请求的配置只能由 Root 修改。
- 所有外部 URL 只允许 `https://`，如确需 `http://` 必须有显式配置开关并默认关闭。
- 出站请求必须设置连接超时、总超时、最大响应体大小和最大重定向次数。
- 禁止访问内网、回环、链路本地、metadata 地址和 Unix socket，例如 `127.0.0.0/8`、`10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16`、`169.254.0.0/16`、`::1`、`fc00::/7`、`fe80::/10`。
- 如果允许跟随重定向，每一次重定向后的目标地址都要重新做内网地址校验。
- LinuxDO lookup 的 `:id` 只能接受数字 ID，不能把用户输入拼进任意 URL。
- GeoIP 的 `:ip` 和 batch IP 必须用 `net.ParseIP` 校验，只允许 IP 字面量，不接受域名，避免被当成 SSRF 跳板。

## 8. 查询与数据库兼容规范

NewAPI 支持 MySQL 和 PostgreSQL，迁移时必须避免只适配单库。

推荐做法：

- 能用 GORM builder 的聚合，优先使用 GORM。
- 必须 raw SQL 时，集中放在 `service/enhancement/query.go`。
- raw SQL 只能拼接服务端白名单中的表名、列名、排序字段和聚合粒度；任何来自 query/body 的 `sort`、`order`、`group_by`、`column`、`model`、`index` 参数都不能直接拼进 SQL。
- 不要手写 `?`、`$1` 混用；如果使用 GORM raw，优先用 `?` 参数让驱动处理。
- 时间窗口不要依赖数据库专有函数。能在 Go 中计算开始/结束时间，就在 Go 中算。
- 日期聚合确实需要按日/小时 group 时，为 MySQL/PostgreSQL 分别生成表达式：
  - MySQL：`DATE_FORMAT(created_at, '%Y-%m-%d')`
  - PostgreSQL：`TO_CHAR(to_timestamp(created_at), 'YYYY-MM-DD')`，具体取决于 `created_at` 是 Unix 秒还是 timestamp。
- NewAPI `logs.created_at` 在现有模型中是 `int64`，通常按 Unix 秒处理。
- 所有查询必须处理空表返回：返回空数组或 0，不应返回 500。
- `quota_data` 不存在或为空时，回退 `logs` 聚合。
- `logs.ip` 为空时，IP 分析应过滤空值，并在统计中给出 `missing_ip_count`。
- 用户软删除要按 NewAPI 现有 `DeletedAt` 语义处理；用户增强页默认不展示软删除用户，除非筛选参数指定。
- 任何批量操作都必须排除 root 用户。
- 所有列表接口必须有服务端分页和最大 `limit`，不能只依赖前端分页；建议默认 20 或 50，最大 200。
- 所有时间范围查询必须有最大跨度；普通管理员建议最大 90 天，公开 embed 建议最大 24 小时。
- 所有写接口必须使用显式 request struct 绑定，不接受任意 `map[string]any` 后整体保存，避免 mass assignment。

## 9. 索引迁移建议

new_api_tools 的大表聚合主要压在 `logs` 上。迁移后在系统工具中提供“索引状态”和“创建推荐索引”。

推荐索引：

```text
logs(created_at, type, user_id)
logs(type, created_at, token_id)
logs(type, created_at, model_name)
logs(user_id, type, created_at)
logs(created_at, ip, token_id)
tokens(user_id, deleted_at)
users(deleted_at, status)
```

如果实现完整索引管理，可兼容 new_api_tools 中更细的索引集：

```text
idx_logs_created_type_user      logs(created_at, type, user_id)
idx_logs_type_created_user      logs(type, created_at, user_id)
idx_logs_type_created_token     logs(type, created_at, token_id)
idx_logs_type_created_model     logs(type, created_at, model_name)
idx_logs_user_type_created      logs(user_id, type, created_at)
idx_logs_user_created_ip        logs(user_id, created_at, ip)
idx_logs_created_token_ip       logs(created_at, token_id, ip)
idx_logs_created_ip_token       logs(created_at, ip, token_id)
idx_users_deleted_status        users(deleted_at, status)
idx_tokens_user_deleted         tokens(user_id, deleted_at)
idx_users_group                 users(group)
```

实现要求：

- PostgreSQL 可用 `CREATE INDEX IF NOT EXISTS`。
- MySQL 需要先查 `information_schema.statistics`，不存在再 `CREATE INDEX`。
- 创建索引建议只允许 Root 执行。
- 提供 dry-run 或“查看将创建哪些索引”接口。
- 对大表创建索引要提示可能耗时，最好按单个索引逐个执行。
- 创建索引接口不能接受任意 SQL、任意表名或任意索引名，只能从服务端内置推荐索引白名单中选择。
- 索引状态接口不能暴露数据库连接串、库名之外的敏感连接信息或错误栈。

## 10. 后台任务设计

new_api_tools 当前有以下后台能力：

- IP 记录强制检查。
- 自动分组扫描。
- AI 封禁扫描。
- 模型状态缓存预热。
- 日志分析批处理或同步。

迁移后要求：

- 只在主节点运行，使用 NewAPI 已有 `common.IsMasterNode` 或等价判断。
- 每个任务都有配置开关。
- 每个任务都有间隔配置。
- 每个任务支持 dry-run。
- 每次真实修改必须写管理日志。
- 任务启动后不能阻塞主进程。
- 任务 panic 必须 recover 并记录错误。
- Redis 不可用时，任务不能直接崩溃；缓存退化为无缓存。

建议在 `main.go` 中新增统一调度入口：

```go
if common.IsMasterNode {
    service.StartEnhancementSchedulers(ctx)
}
```

调度器内部按配置启动：

- `StartIPRecordingEnforcer`
- `StartAutoGroupScanner`
- `StartAIBanScanner`
- `StartModelStatusWarmup`
- `StartAuditLogRetentionCleaner`

真实封禁、解封、禁用 token、移动用户分组、批量删除用户必须记录管理日志。

## 11. 各模块迁移细节

### 11.1 增强仪表盘

来源：

- `new_api_tools/backend/internal/handler/dashboard.go`
- `new_api_tools/backend/internal/service/dashboard_service.go`
- `new_api_tools/frontend/src/components/Dashboard.tsx`

目标：

- 展示系统总览、用量趋势、Top 用户、Top 模型、渠道状态、IP 分布。
- 图表用 VChart。
- 统计口径优先复用 NewAPI 已有日志和 quota data。

关键实现：

- `overview`：
  - 用户总数、活跃用户数、禁用用户数。
  - token 数、启用 token 数。
  - 今日请求数、今日消耗额度。
  - 渠道数、启用渠道数。
- `usage`：
  - 支持 `start_time`、`end_time`、`group_by`。
  - 先查 `quota_data`，为空则聚合 `logs`。
- `models`：
  - 按 `logs.model_name` 聚合请求数、quota、token 数。
- `top-users`：
  - 按 user_id 聚合请求数、quota，批量查用户信息补齐用户名。
- `ip-distribution`：
  - 聚合非空 IP，可结合 GeoIP 缓存。

注意：

- 不要在 handler 中写复杂 SQL。
- 大范围查询必须限制默认窗口，例如默认 7 天，最大 90 天。
- 要提供分页或 limit。

### 11.2 充值审计

来源：

- `handler/top_up.go`
- `service/top_up_service.go`
- `frontend/components/TopUps.tsx`

目标：

- 在 NewAPI 已有充值记录基础上增加审计统计。
- 支持时间范围、支付方式、状态、用户搜索。

关键实现：

- 列表查询 `top_ups`。
- 统计总金额、成功金额、失败/待处理数量、支付方式分布。
- 单条详情显示用户、金额、支付方式、状态、创建时间。

注意：

- 不重复 NewAPI 基础充值管理 UI。
- 如果 NewAPI 的 top up 状态枚举与工具项目不同，以 NewAPI 为准，并在 service 中做展示层映射。

### 11.3 兑换码增强

来源：

- `handler/redemption.go`
- `service/redemption_service.go`
- `frontend/components/Generator.tsx`
- `frontend/components/Redemptions.tsx`
- `frontend/components/History.tsx`

目标：

- 批量生成兑换码。
- 按状态、面额、创建时间统计。
- 批量删除未使用或过期兑换码。
- 生成结果临时展示，允许复制/导出，但不新增后端历史表。

关键实现：

- 使用 NewAPI `model.Redemption`。
- 生成 key 时复用 NewAPI 现有兑换码规则，避免格式不一致。
- 批量生成要限制数量上限，例如默认最大 1000。
- 每次生成、删除写管理日志。
- 批量生成建议支持 `request_id` 或幂等键，避免前端重试造成重复生成大量兑换码。
- 删除接口必须按服务端重新查询到的状态判断能否删除，不能相信前端传来的状态字段。

注意：

- `History.tsx` 旧逻辑只是浏览器本地历史。迁移时不建表。
- 删除已兑换兑换码要二次确认，默认只允许删除未兑换、过期或指定筛选结果。

### 11.4 用户增强

来源：

- `handler/user_management.go`
- `service/user_management_service.go`
- `frontend/components/UserManagement.tsx`

目标：

- 活跃用户、僵尸用户、封禁用户分析。
- 批量删除长期未活跃用户。
- 用户封禁/解封。
- 用户邀请关系查看。
- 禁用某用户 token。

关键实现：

- 活跃度从 `users.last_login_at`、`logs`、`tokens`、`request_count` 综合计算。
- 僵尸用户规则要参数化，例如：
  - 注册超过 N 天。
  - 最近 N 天无请求。
  - quota 未使用或请求数为 0。
- 批量删除必须排除：
  - root 用户
  - 当前登录管理员
  - admin/root 角色，除非 Root 明确勾选高级选项
- 封禁/解封更新 NewAPI 用户状态字段，以 NewAPI 现有状态枚举为准。
- 普通 Admin 不能封禁、删除、降级或移动另一个 Admin/Root；需要 RootAuth。
- 批量删除必须服务端重新计算候选用户，不能直接执行前端提交的一串 user_id 而不校验筛选条件。

注意：

- 所有修改必须写管理日志。
- 批量操作先提供 preview，再执行。
- 默认软删除，不建议物理删除用户。

### 11.5 令牌审计

来源：

- `handler/token.go`
- `service/token_service.go`
- `frontend/components/Tokens.tsx`

目标：

- 查看 token 分组、状态、使用情况。
- 统计 token 数、启用/禁用数量、按用户/分组聚合。
- 结合日志识别高风险 token。

关键实现：

- `tokens` 列表不要返回 token 明文。
- 如果需要展示 key，只能复用 NewAPI 现有安全接口和权限策略。
- 统计使用 `logs.token_id` 聚合请求数、quota、IP 数、模型数。

注意：

- 禁用 token 是危险操作，需二次确认并写日志。
- 多 IP token 不一定代表恶意，只作为风险信号。

### 11.6 风控中心

来源：

- `handler/risk_monitoring.go`
- `service/risk_monitoring_service.go`
- `frontend/components/RealtimeRanking.tsx`

目标：

- 实时排行。
- 用户风险画像。
- token 轮换异常。
- 关联账号。
- 同 IP 注册。
- 封禁记录。

关键指标：

- 请求数突增。
- quota 消耗突增。
- IP 数异常。
- token 数异常。
- 多账号共享 IP。
- 同一邀请链上的异常账号。
- 短时间大量失败/error 日志。

实现建议：

- 风险分数在 service 层计算，不写入 DB，除非后续需要历史记录。
- 返回风险因素列表，而不是只返回一个分数。
- 用户画像接口应把敏感字段脱敏。

### 11.7 IP 分析

来源：

- `handler/ip_monitoring.go`
- `service/ip_monitoring_service.go`
- `frontend/components/IPAnalysis.tsx`

目标：

- IP 统计。
- 共享 IP 用户。
- 多 IP token。
- 多 IP 用户。
- 用户 IP 历史。
- GeoIP 查询与地图。
- IP 记录强制开启。

关键实现：

- 从 `logs.ip` 聚合。
- 空 IP 单独计数。
- GeoIP 结果要缓存，缓存 key 可为 `enhancements:geoip:<ip>`。
- 地图资源可选择迁移：
  - `new_api_tools/frontend/public/world.json`
  - `new_api_tools/frontend/public/china.json`
- 如果暂时不迁移地图，前端先提供表格和国家/地区分布图，后续再补地图。

注意：

- `enable-all-recording` 改动用户配置，建议 Root 或至少强二次确认。
- IP 属于敏感数据，公开 embed 接口不得返回。
- IP 查询参数必须校验为合法 IPv4/IPv6 字面量；不要接受域名并在服务端解析。
- GeoIP 外部 provider 只能从服务端白名单选择，不能由请求参数传入任意 URL。
- 对共享 IP、多 IP 用户、多 IP token 接口设置默认时间窗口和最小阈值，避免一次请求枚举全站用户关系。

### 11.8 日志分析

来源：

- `handler/log_analytics.go`
- `service/log_analytics_service.go`
- `frontend/components/Analytics.tsx`

目标：

- 日志聚合状态。
- 用户请求排行。
- 用户额度排行。
- 模型统计。
- 汇总指标。
- 一致性检查。

实现建议：

- 第一阶段可以全部实时聚合，不建立新表。
- 如果性能不足，再引入 NewAPI 内部汇总表或复用 `quota_data`。
- `process`、`batch-process` 可以改为触发缓存预热或汇总刷新，不要原样照搬独立服务状态机。

注意：

- 大表查询必须默认时间窗口。
- 排行接口必须 limit。
- 一致性检查只返回摘要和建议，不自动修复数据，除非 Root 显式执行。

### 11.9 模型状态

来源：

- `handler/model_status.go`
- `service/model_status_service.go`
- `frontend/components/ModelStatusMonitor.tsx`
- `frontend/components/ModelStatusEmbed.tsx`

目标：

- 可选模型状态监控。
- 支持时间窗口。
- 支持 token group。
- 支持公开嵌入状态页。
- 支持站点标题、展示模型、排序、自定义分组、刷新间隔配置。

状态计算建议：

- 从 `logs` 中按 `model_name` 统计：
  - 请求数
  - 成功数
  - 错误数
  - 错误率
  - 平均耗时
  - p95 耗时，如实现成本高可第二阶段补
  - 最近错误时间
- 如果 NewAPI 有更可靠的渠道测试或模型可用性数据，优先融合。

公开 embed 返回字段限制：

- 站点标题。
- 选中模型。
- 刷新间隔。
- 模型状态摘要。
- 时间窗口。
- token group 名称。

不得返回：

- API Key。
- 渠道 Key。
- 内部配置原文。
- 管理员信息。
- 用户 IP。
- 详细日志 content。
- 未选中公开展示的模型。
- 渠道 ID、渠道名称、token ID、用户 ID 或任何可反推出内部路由策略的字段。

### 11.10 自动分组

来源：

- `handler/auto_group.go`
- `service/auto_group_service.go`
- `frontend/components/AutoGroup.tsx`

目标：

- 根据规则识别待移动用户。
- 支持 preview。
- 支持批量移动。
- 支持 revert。
- 支持扫描日志。

规则建议：

- 用户注册时间。
- 最近活跃时间。
- 已用额度。
- 请求数。
- 当前分组。
- 用户状态。
- 白名单用户。

实现要求：

- 默认 disabled。
- 默认 dry-run。
- `scan` 只生成候选结果，真实移动由 `batch-move` 执行。
- 移动前后都记录用户原分组、新分组、操作者、原因。
- Root 用户永远不移动。
- 管理员用户默认不移动，除非 Root 明确配置。

日志存储：

- 优先使用 NewAPI 管理日志；如果现有管理日志无法承载结构化字段，可新增 `enhancement_audit_logs`。
- 若新增表，需提供迁移与清理策略。
- 如果不新增表，可以把结构化 JSON 写入现有管理日志 content，但查询会弱一些。

### 11.11 AI 封禁

来源：

- `handler/ai_auto_ban.go`
- `service/ai_auto_ban_service.go`

目标：

- 按风险规则筛选可疑用户。
- 调用管理员配置的 AI 模型做判定。
- 支持白名单。
- 支持手动评估。
- 支持定时扫描。
- 支持审计日志。

默认配置：

- `enabled=false`
- `dry_run=true`
- `scan_interval_minutes` 使用保守值，例如 60。
- 未配置 `api_key` 或 `model` 时不允许真实封禁。

AI 调用安全要求：

- 请求模型时只发送必要字段，不发送完整敏感日志。
- 日志 content 要截断并脱敏。
- API Key 不回显。
- 失败要记录 `api_health`，但不要导致后台任务崩溃。
- AI 返回结果只能作为风险建议，不得直接信任模型输出中的用户 ID、SQL、URL 或操作指令；真实操作目标必须来自服务端筛选出的候选用户。
- AI prompt 中必须明确要求只返回结构化 JSON；解析失败时按“不执行”处理。
- AI 请求要设置超时、并发上限和每日/每轮最大评估用户数，避免异常配置导致费用或请求量失控。

真实封禁前必须满足：

- AI 封禁功能 enabled。
- dry_run=false。
- API Key 和 model 有效。
- 用户不在白名单。
- 用户不是 root。
- 用户不是当前操作者。
- 风险证据达到阈值。

审计日志至少记录：

- user_id
- username
- risk_score
- ai_decision
- evidence_summary
- dry_run
- action
- operator 或 scheduler
- created_at

### 11.12 系统工具

来源：

- `handler/storage.go`
- `handler/system.go`

目标：

- 缓存状态。
- 清理增强功能缓存。
- 系统规模统计。
- 索引状态。
- 创建推荐索引。
- 预热状态。

迁移策略：

- 不迁移旧的通用 `/storage/config` KV 管理。
- 系统工具只管理增强功能自己的缓存、索引和状态。
- 清全站缓存属于危险操作，Root 才能执行。

### 11.13 LinuxDO ID 查询

来源：

- `handler/linuxdo_lookup.go`
- `service/linuxdo_lookup_service.go`

目标：

- 通过 LinuxDO ID 查询用户名或绑定信息。
- NewAPI 已有 LinuxDO OAuth 绑定能力，但不是这个查询功能。

迁移建议：

- 新接口：`GET /api/enhancements/linuxdo/lookup/:id`
- 结果缓存 24 小时。
- 支持代理配置时放入增强配置。
- 不强依赖 `tls-client`，第一版可用标准 `net/http`；如果目标站点必须特殊 TLS，再评估是否引入依赖。
- `:id` 必须限制为数字并设置长度上限；不要允许路径、URL、查询串片段或用户名直接进入请求 URL。
- lookup 结果只返回必要字段，例如 `id`、`username`、`cached`，不要把上游 SVG/HTML 原文透传给前端。

## 12. 前端总体设计

新增功能模块：

```text
new-api/web/default/src/features/enhancements/
  api.ts
  index.tsx
  section-registry.tsx
  types.ts
  components/
    EnhancementDashboard.tsx
    TopUpAudit.tsx
    RedemptionEnhancement.tsx
    UserEnhancement.tsx
    TokenAudit.tsx
    RiskCenter.tsx
    IPAnalysis.tsx
    LogAnalytics.tsx
    ModelStatus.tsx
    AutoGroup.tsx
    AIBan.tsx
    SystemTools.tsx
```

新增路由：

```text
new-api/web/default/src/routes/_authenticated/enhancements/index.tsx
new-api/web/default/src/routes/_authenticated/enhancements/$section.tsx
```

路由行为：

- `/enhancements` 重定向到 `/enhancements/dashboard`。
- `$section` 只允许合法 section。
- `auth.user.role < ROLE.ADMIN` 重定向 `/403`。
- 未登录继续由 `_authenticated` 父路由处理。

二级 Tab 建议：

```ts
export const ENHANCEMENT_SECTIONS = [
  { id: 'dashboard', label: '增强仪表盘' },
  { id: 'top-ups', label: '充值审计' },
  { id: 'redemptions', label: '兑换码增强' },
  { id: 'users', label: '用户增强' },
  { id: 'tokens', label: '令牌审计' },
  { id: 'risk', label: '风控中心' },
  { id: 'ip', label: 'IP 分析' },
  { id: 'analytics', label: '日志分析' },
  { id: 'model-status', label: '模型状态' },
  { id: 'auto-group', label: '自动分组' },
  { id: 'ai-ban', label: 'AI 封禁' },
  { id: 'system', label: '系统工具' },
]
```

UI 规范：

- 使用 `SectionPageLayout`、`Tabs`、`DataTable`、`Dialog`、`ConfirmDialog`、`DatePicker`、`StatusBadge` 等现有组件。
- 请求使用 `api` axios 实例。
- 数据请求使用 React Query。
- 图表使用 `@visactor/react-vchart` / `@visactor/vchart`。
- 图标使用 lucide，例如侧边栏 `Sparkles` 或 `ShieldCheck`。
- 不引入 ECharts。
- 不搬运 new_api_tools 的全局布局、登录页、AuthContext。
- 不做营销落地页；进入即是管理工具。
- 移动端需要保证 Tab 可横向滚动或折叠菜单。
- 不使用 `dangerouslySetInnerHTML` 渲染日志、用户名、模型名、站点标题、自定义分组名、AI 返回内容或外部查询结果；如确实要展示富文本，必须先做白名单净化。
- 对 public embed 页面设置明确的 loading、error 和空状态，不把后端错误栈、SQL 错误、上游 AI 错误原文展示给访客。

## 13. 前端侧边栏与权限配置

### 13.1 侧边栏入口

修改：

```text
new-api/web/default/src/hooks/use-sidebar-data.ts
```

在 lucide import 中加入：

```ts
Sparkles
```

在 Admin group 中加入：

```ts
{
  title: t('Enhancements'),
  url: '/enhancements/dashboard',
  activeUrls: ['/enhancements'],
  icon: Sparkles,
}
```

建议放在 `System Settings` 前，或放在 `Redemption Codes` 后。该入口是管理员功能，不应出现在普通用户分组。

### 13.2 前端模块开关

修改：

```text
new-api/web/default/src/hooks/use-sidebar-config.ts
```

默认配置新增：

```ts
admin: {
  enabled: true,
  channel: true,
  models: true,
  redemption: true,
  user: true,
  setting: true,
  subscription: true,
  enhancements: true,
}
```

URL 映射新增：

```ts
'/enhancements': { section: 'admin', module: 'enhancements' },
'/enhancements/dashboard': { section: 'admin', module: 'enhancements' },
```

如果 `activeUrls` 使用 `/enhancements`，映射 `/enhancements` 即可覆盖多数情况；为了稳妥，可把所有二级路径都映射到 `enhancements`。

### 13.3 后端默认侧边栏配置

修改：

```text
new-api/controller/user.go
```

在 `generateDefaultSidebarConfig` 或等价默认侧边栏配置中，为 admin/root 增加：

```json
"enhancements": true
```

这样系统升级后默认可见。

### 13.4 系统设置里的模块开关

修改：

```text
new-api/web/default/src/features/system-settings/maintenance/config.ts
new-api/web/default/src/features/system-settings/maintenance/sidebar-modules-section.tsx
```

新增 admin 模块 key：

```ts
enhancements: true
```

新增显示元信息：

```ts
enhancements: {
  label: t('Enhancements'),
  description: t('Enhanced analytics, auditing, risk controls, and system tools'),
}
```

### 13.5 i18n

至少更新：

```text
new-api/web/default/src/i18n/locales/en.json
new-api/web/default/src/i18n/locales/zh.json
```

新增：

```json
{
  "Enhancements": "Enhancements"
}
```

中文：

```json
{
  "Enhancements": "增强功能"
}
```

如果项目要求所有语言文件 key 一致，则同步更新全部 locale，其他语言可先使用英文 fallback。

## 14. 前端组件迁移建议

不要直接复制 old component。建议按以下方式重写：

| new_api_tools 组件 | NewAPI 新组件 |
|---|---|
| `Dashboard.tsx` | `features/enhancements/components/EnhancementDashboard.tsx` |
| `TopUps.tsx` | `TopUpAudit.tsx` |
| `Generator.tsx` | `RedemptionEnhancement.tsx` 中的生成面板 |
| `Redemptions.tsx` | `RedemptionEnhancement.tsx` 中的列表/统计面板 |
| `History.tsx` | 生成结果临时面板，不建路由 |
| `UserManagement.tsx` | `UserEnhancement.tsx` |
| `Tokens.tsx` | `TokenAudit.tsx` |
| `RealtimeRanking.tsx` | `RiskCenter.tsx` |
| `IPAnalysis.tsx` | `IPAnalysis.tsx` |
| `Analytics.tsx` | `LogAnalytics.tsx` |
| `ModelStatusMonitor.tsx` | `ModelStatus.tsx` |
| `ModelStatusEmbed.tsx` | 可新增公开展示组件或复用普通组件的 readonly 模式 |
| `AutoGroup.tsx` | `AutoGroup.tsx` |

前端 API 文件示例：

```ts
import { api } from '@/lib/api'

const base = '/api/enhancements'

export const enhancementsApi = {
  dashboardOverview: () => api.get(`${base}/dashboard/overview`),
  topUpStatistics: (params?: unknown) =>
    api.get(`${base}/top-ups/statistics`, { params }),
  redemptionGenerate: (payload: unknown) =>
    api.post(`${base}/redemptions/generate`, payload),
}
```

注意：NewAPI 的 axios 封装可能已经统一 unwrap business response，编写前先确认 `api.ts` 当前响应约定。不要在每个组件里重复创建 fetch wrapper。

## 15. 安全与权限要求

核心原则：

- 前端路由守卫、侧边栏开关和确认弹窗只属于体验层，不能作为安全边界。
- 每个后端 handler 或 service 都必须重新校验当前登录用户角色、目标对象、批量数量、dry-run 状态和操作权限。
- 所有写操作必须是非 GET 方法，并复用 NewAPI 现有认证、中间件、限流和安全校验；不要为增强功能单独放宽 CORS。
- 所有危险操作必须在服务端设置上限，即使前端隐藏按钮也不能依赖隐藏按钮防护。

默认 AdminAuth，且只读：

- 仪表盘查看。
- 充值审计查看。
- 兑换码统计和列表。
- 用户增强查看。
- token 审计查看。
- 风控查看。
- IP 分析查看。
- 日志分析查看。
- 模型状态查看。
- 自动分组 preview。
- AI 可疑用户列表查看。
- 系统规模、索引状态、缓存状态查看。

AdminAuth 可执行，但必须有服务端保护、二次确认和审计日志：

- 生成兑换码，必须限制数量、额度、过期时间和名称长度。
- 删除兑换码，默认只允许删除未使用或过期兑换码；删除已兑换兑换码建议 RootAuth。
- 封禁/解封普通用户，必须排除 root、当前操作者和不允许被当前角色管理的管理员。
- 禁用 token，不能返回 token 明文。
- 手动刷新只读聚合缓存。

建议 RootAuth，除非另有明确产品配置开关：

- 保存或修改 AI API Key、AI base URL、代理、GeoIP 外部 provider。
- AI 手动评估、AI scan、AI 真实封禁。若允许 Admin 使用，必须有单独开关，且只发送脱敏后的聚合证据。
- 自动分组批量移动和 revert。若允许 Admin 使用，必须先生成 preview，并在执行时带上服务端签发的 preview id 或条件快照。
- 强制开启所有用户 IP 记录。
- 批量删除用户。
- 物理清理软删除用户。
- 清理全站或跨模块缓存。
- 创建索引。
- 关闭或修改公开 embed 配置。
- 清理 AI/自动分组审计日志。更推荐只允许按保留策略自动清理，并为清理动作本身写审计日志。
- `analytics/reset` 如果会删除汇总或状态数据，必须 RootAuth；如果只是清当前管理员的筛选状态或增强功能缓存，可以 AdminAuth。

必须保护：

- root 用户不能被封禁、删除、移动分组、禁用关键权限或被 AI 自动处理。
- 当前登录管理员不能批量删除自己，不能通过自动分组降低自己的权限或分组。
- API Key 不回显明文。
- token key 不回显明文。
- public embed 不返回 IP、用户、日志详情、渠道信息、token group 内部规则和敏感配置。
- 用户邮箱、OAuth ID、LinuxDO ID、IP、日志 content 等敏感字段默认脱敏或不返回；只有明确管理场景才返回。
- 任何“批量执行”接口都要有最大条数限制、幂等策略和失败明细，不能因为一条失败导致部分状态不可追踪。

## 16. 操作审计

以下操作必须记录管理日志：

- 兑换码批量生成。
- 兑换码批量删除。
- 用户封禁/解封。
- 用户批量删除。
- token 禁用。
- 强制开启 IP 记录。
- 创建索引。
- 清缓存。
- 自动分组扫描与批量移动。
- 自动分组 revert。
- AI 封禁扫描。
- AI 真实封禁。
- AI 配置变更，日志中不得包含明文 Key。

日志内容建议结构化：

```json
{
  "module": "enhancements.ai_ban",
  "action": "ban_user",
  "target_user_id": 123,
  "dry_run": false,
  "reason": "AI risk decision",
  "operator_id": 1
}
```

如果写入 NewAPI 现有管理日志，只把 JSON 放在 content/other 字段中即可。

## 17. 分阶段实施路线

### Phase 0：预检

- 确认 NewAPI 当前分支干净或记录已有改动。
- 跑一次后端和前端基础测试，记录 baseline。
- 确认 `logs.created_at` 实际存储单位。
- 确认 `top_ups`、`redemptions`、`tokens` 字段与 new_api_tools 假设差异。

### Phase 1：骨架

- 新增 `/api/enhancements/*` 路由组。
- 新增公开 embed 路由。
- 新增 `setting/enhancement_setting.go`。
- 新增前端 `/enhancements/dashboard` 路由。
- 侧边栏出现 `增强功能`。
- 二级 Tab 可跳转但可先显示空状态。

验收：

- 管理员可访问 `/enhancements/dashboard`。
- 普通用户访问跳转 `/403`。
- 未登录用户访问受认证保护。
- public embed 接口未登录可访问。

### Phase 2：只读能力

先迁移无副作用接口：

- dashboard
- top-ups
- redemptions statistics/list
- users stats/list/banned
- tokens list/statistics
- risk leaderboards/user analysis
- ip stats/shared/multi
- analytics ranking/summary/models
- model-status read/status
- system scale/index status
- LinuxDO lookup

验收：

- 每个 Tab 有真实数据或空状态。
- 空表不报错。
- 大表查询有默认时间窗口和 limit。

### Phase 3：写操作与批处理

迁移：

- 兑换码批量生成/删除。
- 用户封禁/解封。
- token 禁用。
- 自动分组 preview/batch move/revert。
- IP 记录强制开启。
- 缓存清理。
- 索引创建。

验收：

- 每个危险操作有确认弹窗。
- 操作后写管理日志。
- root 用户受保护。
- 错误提示可读。

### Phase 4：后台任务

迁移：

- IP 记录检查。
- 自动分组定时扫描。
- AI 封禁定时扫描。
- 模型状态预热。
- 审计日志保留清理。

验收：

- 只在主节点运行。
- 开关关闭时不运行。
- dry-run 不修改数据。
- 任务异常不影响主进程。

### Phase 5：性能和体验

- 对 logs 聚合接口做 explain。
- 添加推荐索引。
- 优化 React Query 缓存与 loading 状态。
- 移动端检查。
- public embed 页面做只读轻量展示。

## 18. 测试计划

后端测试：

```bash
go test ./...
```

重点单元测试：

- MySQL/PostgreSQL 聚合 SQL 生成。
- 空表返回。
- 无 Redis 环境。
- 无 `quota_data` 回退。
- 日志无 IP。
- 软删除用户过滤。
- root 用户保护。
- 权限不足。
- AI Key 脱敏。
- public embed 字段白名单。
- public embed 关闭时不可访问。
- public embed 超过模型数量、body 大小、时间窗口时被拒绝。
- 普通 Admin 不能执行 Root-only 操作。
- 普通 Admin 不能操作 Admin/Root 目标用户。
- SSRF 防护：AI base URL、GeoIP provider、LinuxDO proxy 指向内网/metadata 地址时被拒绝。
- SQL 注入防护：非法 sort/group_by/index 参数不会进入 raw SQL。

建议表驱动测试覆盖：

- dashboard overview
- usage trends
- top users
- model statistics
- redemption statistics
- top-up statistics
- token statistics
- IP shared users
- risk score
- auto group candidate selection
- AI ban decision safety gate

前端测试：

```bash
cd new-api/web/default
npm run typecheck
npm run lint
npm run build
```

前端验收：

- `/enhancements` 自动跳转 `/enhancements/dashboard`。
- `/enhancements/:section` 非法 section 跳转 dashboard 或 404。
- 管理员侧边栏显示 `增强功能`。
- 侧边栏模块开关关闭后入口隐藏。
- 二级 Tab 路由同步。
- 表格分页、筛选、空状态正常。
- 危险操作有确认弹窗。
- 手机宽度下不重叠、不溢出。

集成测试：

- 用一组测试数据验证 new_api_tools 原有接口能力在新路径下都有等价功能。
- public model status embed 未登录可访问。
- public embed 不泄露敏感字段。
- AI 自动封禁默认不会真实封禁。
- 自动分组 dry-run 不修改用户分组。

性能测试：

- 对 `logs` 大表聚合接口做 explain。
- 检查是否命中推荐索引。
- 默认时间窗口查询响应可接受。
- 大范围查询需要分页、limit 或后台汇总。

## 19. 交付标准

后端：

- 所有目标接口存在。
- 返回统一 NewAPI 响应格式。
- 使用 NewAPI 原生认证。
- 不再依赖 new_api_tools 独立配置和独立 DB 初始化。
- 配置持久化到 NewAPI options/config。
- Redis 不可用时功能降级但不崩溃。
- 危险操作有审计日志。

前端：

- 侧边栏新增 `增强功能`。
- 二级 Tab 覆盖全部模块。
- 使用 NewAPI 默认组件体系。
- 图表使用 VChart。
- 不引入 ECharts。
- 不引入独立 AuthContext。
- 不保留独立登录页。
- 不保留旧 Vite 应用结构。

安全：

- Admin/Root 权限边界明确。
- API Key 和 token key 不明文回显。
- public embed 无敏感字段。
- root 用户保护。

文档：

- README 或管理员说明中注明增强功能入口、配置、默认关闭项和危险操作说明。

## 20. 常见风险与处理

风险：`model.LOG_DB` 和 `model.DB` 可能分库，无法直接 join。

处理：日志聚合先从 `LOG_DB` 查出 user_id/token_id，再用 `DB` 批量查用户/token 信息合并。

风险：`logs` 表很大，实时聚合慢。

处理：默认时间窗口、limit、推荐索引、缓存；后续再加汇总表。

风险：旧工具 Redis 配置丢失。

处理：长期配置迁到 `setting/config.GlobalConfig`；Redis 只做缓存。

风险：AI 自动封禁误伤用户。

处理：默认关闭、默认 dry-run、白名单、root 保护、人工确认、审计日志。

风险：前端直接复制旧组件导致依赖冲突。

处理：只参考交互和字段，使用 NewAPI 组件重写。

风险：公开 embed 泄露内部信息。

处理：单独 DTO，字段白名单，不复用管理员配置响应。

风险：索引创建锁表。

处理：Root 权限、逐个创建、提示耗时、可 dry-run。

## 21. 推荐给另一个对话的执行提示词

可以把下面这段交给另一个对话作为实施指令：

```text
请根据工作区根目录的 NEWAPI_TOOLS_MIGRATION_GUIDE.md，把 new_api_tools 的功能迁移到 new-api 本体。

要求：
1. 不迁移独立登录、JWT、独立 Docker、独立数据库配置和独立前端构建。
2. 后端新增 /api/enhancements/*，默认 AdminAuth，敏感操作 RootAuth；公开模型状态 embed 使用 /api/enhancements/model-status/embed/* 且无敏感字段。
3. 使用 NewAPI 的 model.DB/model.LOG_DB、common.ApiSuccess/ApiError、setting/config.GlobalConfig、common.Redis。
4. 前端在 web/default 新增管理员侧边栏入口“增强功能”，路径 /enhancements/dashboard，内部二级 Tab 覆盖文档列出的 12 个模块。
5. 前端使用 TanStack Router、React Query、shadcn/Radix、lucide、VChart，不引入 ECharts，不复制旧 Vite 应用。
6. 危险操作必须有确认弹窗、权限控制和管理日志；root 用户不可被封禁、删除或自动移动。
7. AI 自动封禁默认 disabled 且 dry-run=true，API Key 只保存不回显。
8. 公开 embed 必须默认关闭，开启后只返回 selected_models 的只读聚合状态，并强制限流、最大模型数、最大时间窗口和字段白名单。
9. 所有外部请求配置、AI base URL、代理、GeoIP provider 必须防 SSRF；所有 raw SQL 的排序、分组、索引名必须使用服务端白名单。
10. 先做 Phase 1 骨架，再做 Phase 2 只读能力，再做 Phase 3 写操作，最后做后台任务和性能优化。
11. 完成后运行 go test ./...、web/default 下 npm run typecheck、npm run lint、npm run build，并说明任何无法运行的原因。
```

## 22. 最小可执行任务拆分

如果需要把迁移拆给多个开发者或多个 agent，建议这样拆：

任务 A：后端骨架和配置

- 新增 enhancement setting。
- 注册 `/api/enhancements` 和 public embed。
- 建立统一 service/types/query helpers。
- 写权限和响应封装。

任务 B：后端只读聚合

- dashboard/top-ups/redemptions/users/tokens/risk/ip/analytics/model-status/system read endpoints。
- 空表和分库兼容。

任务 C：后端写操作

- redemptions generate/delete。
- users ban/unban/delete。
- token disable。
- auto-group move/revert。
- system cache/index actions。
- 全部写审计日志。

任务 D：AI 封禁与后台任务

- AI config/key mask。
- suspicious users。
- manual assess。
- scan dry-run。
- scheduler。
- whitelist。

任务 E：前端框架

- 路由。
- 侧边栏。
- 权限。
- Tab registry。
- API client。
- 空状态和基础布局。

任务 F：前端模块

- 12 个 Tab 的数据展示和操作表单。
- VChart 图表。
- 表格分页筛选。
- 确认弹窗。

任务 G：测试与性能

- 后端表驱动测试。
- 前端 typecheck/lint/build。
- explain 和索引。
- public embed 泄露检查。

## 23. 迁移时不要做的事

- 不要把 `new_api_tools/backend` 整个复制进 NewAPI。
- 不要继续使用 new_api_tools 的 JWT 中间件。
- 不要新增独立服务端口。
- 不要让长期配置只存在 Redis。
- 不要复制旧前端布局和登录页。
- 不要引入 ECharts。
- 不要把浏览器本地 History 做成数据库表。
- 不要让公开 embed 复用管理员配置 DTO。
- 不要让后台任务默认真实封禁用户。
- 不要对 root 用户执行封禁、删除、分组移动。
