# 专题 12：JWT 鉴权、用户数据隔离和 RAG 多租户安全

> 面试高频问题："怎么保证用户不能看到别人的视频、转写、RAG 片段和 API Key？"
> AI 视频项目的数据比普通 CRUD 更敏感，因为转写文本、RAG chunk 和聊天记录都可能还原用户视频内容。

## 1. 先给总答案

推荐先这样答：

> 项目用 JWT 做登录态，注册时密码用 bcrypt 哈希，登录后 token 里带 userID。后端 Gin middleware 解析 Bearer token，把 userID 放到 context。之后任务、AI profile、chat session、RAG 索引状态都按 userID 查询或校验。尤其 RAG 检索时，Milvus filter 会带 `user_id + task_id + embedding_model`，避免召回其他用户的视频片段。API Key 不明文返回，存储时用 AES-GCM 加密。

一句话：

> 权限隔离必须在后端查询和向量检索里做，不能只靠前端隐藏按钮。

## 2. 当前认证流程

### 2.1 注册

```text
internal/service/user.go Register
```

- 检查用户名是否存在。
- 密码用 bcrypt hash。
- 创建用户。
- 生成 JWT。

### 2.2 登录

```text
internal/service/user.go Login
```

- 按 username 查用户。
- bcrypt 校验密码。
- 生成 JWT。

### 2.3 JWT 解析

```text
internal/pkg/jwt/jwt.go
internal/middleware/auth.go
```

JWT claims 包含：

- `user_id`
- `username`
- `role`
- issuer
- issued_at
- expires_at

middleware 从 `Authorization: Bearer <token>` 解析 token，把 userID 写入 gin context。

## 3. 用户隔离落在哪里

### 3.1 任务隔离

MediaService 每次操作 task 时都会校验：

```text
task.UserID == currentUserID
```

典型接口：

- 获取任务详情。
- 删除任务。
- 请求转写。
- 请求分析。
- 创建聊天会话。

### 3.2 AI profile 隔离

`user_ai_profiles` 按 userID 查询：

- list 只看当前用户。
- update/delete 要带 userID 和 profile ID。
- consumer 处理任务时按 `task.UserID` 找默认 profile。

这能避免用户 A 使用用户 B 的 API Key。

### 3.3 Chat 隔离

chat session 和 chat message 都带 userID。

`FindSessionForUser` 会按 userID + sessionID 查，避免用户通过 sessionID 猜测访问别人的问答记录。

### 3.4 RAG 检索隔离

这是最重要的点。

Milvus 检索 filter：

```text
user_id == ? and task_id == ? and embedding_model == ?
```

为什么三者都要带：

- `user_id`：用户隔离。
- `task_id`：单视频问答范围。
- `embedding_model`：保证查询向量和索引向量来自同一模型空间。

推荐回答：

> RAG chunk 本质上是用户视频转写文本，如果检索时不带 user_id，就可能把别人的视频片段召回到当前回答里，这是严重的数据串读。

### 3.5 MinIO 对象访问

MinIO bucket 默认私有。

用户下载音频或视频时，不直接公开对象路径，而是先通过后端 task 权限校验，再生成短期预签名 URL。

当前代码中 `DownloadAudio` 会先走 `GetTaskDetail` 校验 userID，再生成 URL。

## 4. 密码和 API Key 为什么处理方式不同

### 4.1 密码用 bcrypt

密码只需要校验，不需要还原。

所以应该用不可逆慢哈希：

```text
bcrypt.GenerateFromPassword
bcrypt.CompareHashAndPassword
```

### 4.2 API Key 用 AES-GCM

API Key 调用第三方模型时必须还原明文，所以不能用 bcrypt。

当前做法：

- `security.api_key_secret` 作为加密密钥来源。
- AES-GCM 加密。
- nonce 随机生成。
- 数据库存 ciphertext。
- 响应只返回 mask 后的 key。

推荐回答：

> 密码和 API Key 是两个场景。密码不需要还原，所以 bcrypt；API Key 需要调用 provider 时还原，所以用可逆加密，并且只在服务端短暂解密使用。

## 5. 高频追问

### Q1：JWT 安全吗？

答：

> JWT 只是认证机制的一部分。它适合前后端分离和无状态 API。安全还要依赖 HTTPS、合理过期时间、secret 管理、后端权限校验和敏感数据脱敏。当前项目实现了基础 JWT 鉴权和用户维度隔离，生产上可以加 refresh token、黑名单和强制登出。

### Q2：前端隐藏按钮能不能算权限控制？

答：

> 不能。前端隐藏只是体验层，真正权限必须在后端校验。项目里 task、AI profile、chat session、RAG 检索都会按 userID 做后端校验。

### Q3：用户能不能通过 taskID 猜别人的任务？

答：

> 不能只靠 taskID。后端查到 task 后会校验 `task.UserID` 是否等于当前用户。如果不匹配，返回无权访问。

### Q4：RAG 为什么必须带 userID？

答：

> 因为 RAG chunk 是视频转写文本，属于用户私有内容。只按 taskID 理论上也能定位任务，但多租户系统里更稳的是把 userID 也作为 filter 条件，避免任何跨用户召回风险。

### Q5：API Key 会不会泄露到日志？

答：

> 当前 profile 响应只返回 masked key，数据库存 ciphertext。AI 调用审计也不保存 Authorization header、明文 key、完整 prompt 和完整 response，只保存 provider、model、耗时、状态和错误摘要。

### Q6：JWT 怎么主动失效？

答：

> 当前项目还没有完整 token 黑名单或 refresh token 体系，所以 token 在过期前默认有效。生产上可以用短 access token + refresh token，或者 Redis 黑名单支持主动登出和封禁。

### Q7：MinIO 对象是不是公开访问？

答：

> 不是公开桶。对象访问由后端权限控制后生成短期预签名 URL。用户不能直接靠对象名访问所有视频。

## 6. 30 秒话术

> 用户隔离是后端做的，不靠前端隐藏。登录后 JWT 里带 userID，Gin middleware 解析后放到 context。后续任务、AI profile、chat session、RAG 索引状态都按 userID 查询或校验。RAG 检索时 Milvus filter 带 `user_id + task_id + embedding_model`，避免召回其他用户的视频片段。密码用 bcrypt，API Key 因为要调用 provider，需要 AES-GCM 可逆加密。

## 7. 2 分钟话术

> 这个项目里的用户数据比较敏感，因为视频转写、RAG chunk、聊天记录和用户 API Key 都可能包含隐私。认证上我用 JWT，登录成功后 token 里带 userID，后端 middleware 校验签名、issuer 和过期时间，再把 userID 放到 gin context。
>
> 权限隔离落在后端 service 和 repository 查询里。比如查看任务、删除任务、请求转写和分析都会校验 task.UserID；AI profile 按当前用户查询和修改；chat session 也必须属于当前用户。RAG 是重点，Milvus 检索时必须带 userID、taskID 和 embeddingModel，避免跨用户召回视频片段。
>
> 密钥处理上，密码用 bcrypt，因为不需要还原；API Key 要调用三方服务，所以用 AES-GCM 加密存储，接口只返回脱敏值，AI 调用日志也不保存明文 key。

## 8. 不要这么说

- 不要说 JWT 能天然主动踢下线，当前没有黑名单体系。
- 不要说前端隐藏按钮就是权限控制。
- 不要说 RAG 只按 taskID 过滤就完全足够，最好强调 userID。
- 不要说 API Key 用 bcrypt 加密，bcrypt 是不可逆哈希。
- 不要说 MinIO 对象公开访问。

## 9. 代码证据路径

```text
internal/service/user.go
internal/pkg/jwt/jwt.go
internal/middleware/auth.go

internal/service/media.go
internal/service/ai_profile.go
internal/service/chat.go
internal/vector/milvus.go

internal/pkg/secret/crypto.go
internal/model/ai_profile.go
```

