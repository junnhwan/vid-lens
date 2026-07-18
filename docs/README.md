# VidLens 文档入口与事实源

> 目标：让开发者和后续 AI 先找到当前事实，再阅读历史材料。代码与最新验证结果始终高于文档；文档互相冲突时，按本页层级处理。

## 1. 权威层级

### L0：运行时事实

- `cmd/server/`：服务启动、依赖连接、组装和路由。
- `internal/model/`、`internal/repository/`：PostgreSQL schema、状态机和事务边界。
- `internal/service/`、`internal/mq/`：业务流程、Kafka 消费和失败恢复。
- `internal/vector/factory.go`：正式向量 backend 的唯一选择入口。
- `config.yaml`、`docker-compose.yml`：当前本地正式配置与可选回滚 profile。

任何简历、book 或历史说明与这些文件冲突时，以代码和测试为准。

### L1：项目级维护事实

1. `README.md`：项目入口、运行方式和对外能力边界。
2. `MEMORY.md`：长期事实、面试口径和明确禁止夸大的能力。
3. `docs/backend-maintenance-map.md`：模块 owner、调用链、不变量和修改入口。
4. `docs/backend-optimization-roadmap.md`：当前阶段、已完成项和后续顺序。
5. `docs/troubleshooting-and-interview-notes.md`：真实故障、根因和修复证据。

做架构建议或跨模块改动前，至少阅读以上文件。

### L2：专项权威文档

- PostgreSQL 单库：`docs/postgresql-single-database-migration.md`
- pgvector：`docs/pgvector-migration.md`
- 分片上传：`docs/resume-topics/03-chunk-upload-resume.md`
- RAG：`docs/resume-topics/05-rag-hybrid-retrieval.md`
- 简历当前版本：`docs/resume-final-draft.md`
- 面试总入口：`docs/resume-interview-defense-index.md`

专项实现变化时，先更新对应 L2 文档，再让其他入口只保留摘要和链接，避免复制十份实现说明。

### L3：派生材料与历史快照

`book/docs/`、`docs/grill/`、早期 QA script 和设计草稿可能包含历史实现。它们适合复习或追溯，不应单独作为当前架构证据。若未明确写“当前事实”，必须回到 L0-L2 核对。

## 2. 当前数据库与上传术语

统一使用以下表述：

- PostgreSQL 是唯一正式关系数据库；pgvector 是同一 PostgreSQL 的扩展。
- `video_chunks` 是 RAG 文本事实源，pgvector 表是可重建向量投影。
- MySQL 与 Milvus 只在迁移观察期作为离线回滚资产，不参与默认 server 运行，也没有双写。
- 分片上传由 PostgreSQL 保存 session、manifest、chunk ledger 和 completion lease；MinIO 保存字节；Redis 不参与上传正确性。
- 旧 `/upload-chunk`、`/check-upload`、`/merge-chunks`、Redis Set 进度和 ComposeObject 协议均已退役。

## 3. 架构变化时的最小同步清单

1. 修改代码与测试，先证明状态 owner、事务边界和失败语义。
2. 更新 `docs/backend-maintenance-map.md` 中的 owner 与调用链。
3. 更新唯一对应的专项文档，不在多个大文档复制实现细节。
4. 若影响项目定位或面试口径，再更新 `README.md`、`MEMORY.md`、`docs/resume-final-draft.md`。
5. 搜索旧术语，历史命中必须明确标记“旧协议”“已退役”或“回滚资产”。
6. 后端变更至少运行 `go test -count=1 ./...`；阶段完成时再运行路线图规定的完整质量门禁。

## 4. 当前不能声称

- 远端部署已经完成 PostgreSQL/pgvector 迁移；
- task/job 入库与首次 Kafka enqueue 已通过 outbox 实现原子提交；
- Kafka exactly-once 或全链路强一致；
- MySQL/Milvus 回滚资产已经安全删除；
- URL 下载已经达到生产级 SSRF 防护；
- 模型 rerank/Cross-Encoder 已进入正式在线问答。
