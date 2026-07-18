# 专题 12：JWT 鉴权、BYOK 与用户数据隔离

## 1. 面试口语答案

> VidLens 的权限控制不靠前端隐藏按钮。登录成功后签发 JWT，Gin middleware 校验 Bearer token，把 `userID`、username 和 role 放入请求上下文。后续 task、upload session、AI profile、chat session 和 RAG 查询都必须以当前 userID 查询或再次校验 owner。
>
> RAG 隔离尤其重要：pgvector Search 的 SQL 条件固定包含 `user_id + task_id + embedding_model`，关系 chunk 查询也使用同一 scope。这样即使用户猜到别人的 task ID，也不能只凭 taskID 召回内容。上传 session 同样绑定 userID，读取、上传分片和 complete 都带 owner 条件。
>
> 密码和 BYOK API Key 的处理不同。密码只需要验证，所以使用 bcrypt 单向哈希；API Key 运行时必须还原后调用用户 provider，因此用 AES-GCM 加密保存，接口只返回脱敏值。代码和日志不应输出明文 key。

## 2. 隔离边界

| 资源 | 当前约束 |
|---|---|
| Task | service/repository 读取后校验 `task.UserID` |
| Upload session | repository 条件包含 `session_id + user_id` |
| AI profile | CRUD 条件包含 `user_id`，默认 profile 也按用户选择 |
| Chat | session/message 查询包含 `user_id` |
| RAG relation | chunk/index 按 `user_id + task_id + model` |
| pgvector | Search/Delete/manifest 按相同 scope |
| MinIO object | 不把 object name 当权限；通过已鉴权 task/session 业务路径访问 |

## 3. 高频追问

### JWT 能主动失效吗？

> 当前主要依赖签名和过期时间，没有完整 token blacklist、refresh token rotation 或设备会话管理，因此不能声称强制单点登出已完成。

### 用户能否通过 ID 枚举数据？

> ID 不是权限。Handler 从 JWT 获取 userID，service/repository 查询必须带 userID 或校验归属；找不到和无权访问应避免泄漏过多资源存在信息。

### pgvector 与关系库同库是否自动安全？

> 不会。安全来自每条检索 SQL 的 scope 和上游 task owner 校验，同库只减少部署复杂度。

### API Key 会不会出现在响应或日志？

> profile 响应只返回 masked 字段，调用前才解密。异常包装和日志需要使用安全错误处理，不能记录请求 header、完整 provider body 或明文配置。

## 4. 代码证据

- `internal/middleware/auth.go`：JWT middleware。
- `internal/pkg/jwt/`、`internal/pkg/secret/`：token 与 AES-GCM。
- `internal/service/ai_profile.go`、`internal/repository/ai_profile.go`：BYOK owner 与脱敏响应。
- `internal/repository/upload_session.go`、`internal/repository/chat.go`：用户条件。
- `internal/vector/pgvector.go`：检索和删除 scope。
- `internal/handler/*_test.go`、`internal/service/upload_session_test.go`：越权测试。

## 5. 当前限制

- 没有 refresh token、token blacklist、细粒度 RBAC 或审计后台。
- URL 下载入口仅作自用辅助，不能把 allowlist/DNS 校验说成完整的多租户下载沙箱。
- 全局资产按 MD5 可跨用户复用对象，这是存储复用设计；用户 task、结果和问答仍必须隔离。
