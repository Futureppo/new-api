# NodeLoc 内置 OAuth 登录

本功能在 new-api 中提供 NodeLoc 注册、登录、个人账号绑定和管理员解绑。协议依据：[NodeLoc OAuth 对接文档](https://docs.nodeloc.com/api-reference/introduction)。

## 创建 NodeLoc 应用

1. 使用具有黄金会员 TL2 及以上资格的 NodeLoc 账号，打开 [OAuth 应用管理](https://www.nodeloc.com/oauth-provider/applications)。这是文档对创建应用者的要求，本站不据此限制登录用户等级。
2. 创建应用，仅申请 `openid profile`。不申请 `email`；如已有应用申请了邮箱权限，请按 NodeLoc 文档移除该权限或完成其审核。
3. 在 NodeLoc 应用侧设置用户准入要求。
4. 将回调地址填写为本站保存的 **服务器地址 + `/oauth/nodeloc`**。例如服务器地址为 `https://api.example.com`，回调就是 `https://api.example.com/oauth/nodeloc`，没有 `/api` 前缀和末尾斜杠。
5. 保存 Client ID 和仅显示一次的 Client Secret。

## 配置 new-api

1. 部署包含本功能的后端与前端。启动时 GORM 会给用户表新增 `nodeloc_id` 字段和唯一索引，保留已有账号；未绑定值在数据库中为 NULL，防止并发请求产生重复绑定。
2. 在系统设置中填写并保存正式的服务器地址。生产环境使用 HTTPS；地址须为站点源地址，不含路径、查询参数、片段或用户密码。开发环境可使用 `http://localhost:3000`。
3. 在“配置 NodeLoc OAuth”中填写 Client ID、Client Secret，点击“保存 NodeLoc OAuth 设置”。保存后 Secret 不再显示；再次编辑时留空会保留原值。
4. 核对界面显示的回调 URL 与 NodeLoc 应用登记值完全一致。
5. 打开“允许通过 NodeLoc 账户登录 & 注册”，刷新登录页后使用“使用 NodeLoc 继续”。

请从服务器地址对应的站点访问。别名域名会在授权前被提示切换到正式站点，避免会话 Cookie 与 state 丢失。更改正式域名时，需要同步修改 NodeLoc 应用的回调地址，并刷新页面重新发起授权。

## 账号行为

- 首次授权按本站注册开关、邀请码和注册码规则创建账号；已有 NodeLoc 绑定可继续登录，不会重复注册。
- 以 NodeLoc 数字用户 ID 识别账号，不按用户名合并；名称冲突时使用本站现有的自动命名方式。
- 不读取或写入 NodeLoc 邮箱，不同步头像，不增加用户等级限制。
- 已有本站账号在个人设置中绑定 NodeLoc；需先完成本站现有的密码设置要求。若 NodeLoc ID 已被占用，绑定失败。
- 管理员在用户绑定管理中查看或清除 NodeLoc 绑定，权限和审计沿用现有流程。
- 禁用和已删除账号不能借此重新登录或重新注册。退出登录沿用本站会话处理。
- Access Token 仅在本次回调中用于获取用户信息，不持久化；不保存或刷新 NodeLoc Token，不依赖 OIDC 自动发现或 ID Token。

## 接口及配置

| 项目 | 值 |
|---|---|
| 提供商标识 | `nodeloc` |
| 配置项 | `nodeloc.enabled`、`nodeloc.client_id`、`nodeloc.client_secret` |
| 默认状态 | 关闭 |
| 公共状态字段 | `nodeloc_oauth`、`nodeloc_client_id`、`nodeloc_redirect_uri` |
| 用户字段 | `nodeloc_id` |
| 授权端点 | `https://www.nodeloc.com/oauth-provider/authorize` |
| Token 端点 | `https://www.nodeloc.com/oauth-provider/token` |
| 用户信息端点 | `https://www.nodeloc.com/oauth-provider/userinfo` |
| 浏览器回调 | `/oauth/nodeloc` |
| 后端回调处理 | `/api/oauth/nodeloc` |

## 上线后验收

1. 使用未绑定的 NodeLoc 测试账号注册，确认邀请码和注册码要求符合本站配置。
2. 退出后再次登录，确认进入原本站账号。
3. 使用另一已有本站账号完成绑定，验证重复绑定冲突与管理员解绑。
4. 在 NodeLoc 授权页拒绝授权，确认本站提示拒绝，并返回登录或个人设置页。
5. 验证关闭注册仍允许已绑定用户登录，禁用用户不能登录。
6. 确认个人邮箱没有因 NodeLoc 授权而改变。

本次代码测试使用模拟 OAuth 服务，不代表已完成上述真实应用验收。SQLite 覆盖了实际旧表升级；MySQL/PostgreSQL 覆盖 GORM 字段类型和 SQL 生成，仍需在部署使用的实际数据库版本上验证升级。

常见失败原因：未先保存凭据便启用、服务器地址无效、NodeLoc 应用回调不匹配、从别名域名发起授权、上游准入拒绝或应用审核未完成。修改配置后重新发起授权，避免复用过期授权码。
