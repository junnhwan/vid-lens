# 专题 13：测试策略与外部依赖隔离

## 1. 面试口语答案

> 这个项目不追求用大量脆弱 UI 测试制造覆盖率数字，而是优先测试状态机和失败窗口。Service/consumer 测试用 fake storage、recording producer、fake AI client 验证 owner、幂等、enqueue 失败、lease 接管和重试；Redis Lua 用 miniredis；部分 repository 测试用 SQLite 做快速反馈；pgvector SQL 用 sqlmock，并提供显式环境变量开启的真实 PostgreSQL/pgvector 集成测试。
>
> 迁移后我额外验证了 PostgreSQL AutoMigrate、`FOR UPDATE` 竞争、repository upsert、retry/usage ledger 和 pgvector extension/schema/upsert/search/delete。普通 `go test ./...` 不应强制每个开发者启动全部中间件；真实集成测试通过 opt-in 环境变量运行，并在迁移 smoke 中实际执行。
>
> 前端是验证面，Node 测试覆盖 API contract、分片 session、轮询和 SSE/citations 等策略，再跑构建检查。当前仍没有覆盖全部真实 Kafka/MinIO/provider 的自动化端到端流水线，所以不能把单元测试通过说成完整生产验收。

## 2. 当前测试层次

| 层次 | 主要手段 | 重点 |
|---|---|---|
| 纯逻辑 | table-driven Go tests | 错误分类、chunk、RRF、配置校验 |
| Service | fake repo/storage/AI/producer | owner、幂等、失败补偿、调用编排 |
| Repository | SQLite 快测 + PostgreSQL opt-in | SQL 条件、唯一约束、行锁、事务 |
| Redis | miniredis | Lua 桶、lock owner、TTL/错误策略 |
| Vector | sqlmock + pgvector opt-in | schema、维度、scope、replace/search/delete |
| MQ | recording writer/producer + consumer handler tests | payload、lease、commit/失败移交 |
| HTTP | Gin recorder | 鉴权、状态码、响应 envelope |
| Frontend | Node tests + production build | API 策略和构建兼容 |

## 3. 关键质量门禁

后端常规门禁：

```powershell
go test -count=1 ./...
go vet ./...
```

阶段收口还应运行：

```powershell
staticcheck ./...
deadcode -test ./...
go build ./cmd/server ./cmd/rag-eval ./cmd/rag-reindex ./cmd/rag-audit ./cmd/mysql-to-postgres
```

前端：

```powershell
cd web
npm test
npm run build
```

真实集成测试必须按测试文件声明的环境变量显式开启，不能把默认 skip 写成“已经验证”。

## 4. 高频追问

### 为什么不让单元测试连接真实 AI？

> 真实 provider 有费用、限流、网络抖动和非确定输出。单元测试验证请求构造、错误分类和业务状态；少量真实 provider smoke 应单独运行、明确凭证和成本边界。

### SQLite repository 测试能证明 PostgreSQL 行为吗？

> 不能。它适合快速验证普通查询和 service 流程，但 `FOR UPDATE`、PostgreSQL 类型、sequence 和 pgvector 必须由真实 PostgreSQL 集成测试证明。

### Kafka 怎么测？

> producer 配置和 payload 可直接测试；consumer handler 用 recording/failing producer 与 repository 验证状态移交。broker、consumer group rebalance 和真实 offset 行为仍需要独立集成/E2E 测试。

### 当前最大测试缺口是什么？

> 真实 Kafka + MinIO + provider 的自动化全链路、远端部署 smoke，以及首次 DB 写入—Kafka enqueue 一致性失败场景仍需继续加强。

## 5. 代码证据

- `internal/model/postgres_integration_test.go`
- `internal/repository/postgres_integration_test.go`
- `internal/vector/pgvector_integration_test.go`
- `internal/service/media_test.go`
- `cmd/server/router_test.go`
- `internal/service/task_cleanup_test.go`
- `internal/mq/reliability_review_test.go`
- `internal/middleware/ratelimit_test.go`
- `cmd/server/*_test.go`
- `web/test/`

## 6. 当前限制

- 尚无完整 CI 交付证据和自动化跨组件 E2E。
- Milvus adapter 的存在只用于回滚，不应把历史 Milvus fake 测试写成当前 pgvector 验证。
- 每次声称“测试通过”都必须附本次实际命令和结果，不能沿用旧文档结论。
