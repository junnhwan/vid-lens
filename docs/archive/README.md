# 归档文档（默认不要读）

本目录存放**面试材料、历史规划、简历专题**。它们不是当前产品实现的事实源。

## 给后续 AI 的规则

- **默认不要读取** `docs/archive/**`、`docs/grill/**`，以及本机可能残留的 `book/**`（`book/` 已从 git 主路径 untrack，并 gitignore）。
- 只有用户明确要求面试/简历/复盘时，才进入本目录。
- 做功能开发、修 bug、改架构时，只看：
  1. 代码（`cmd/server`、`internal/**`）
  2. `docs/backend-maintenance-map.md`
  3. `docs/README.md`
  4. 必要时 `README.md`、`docs/pgvector-migration.md`、`docs/postgresql-single-database-migration.md`

## 目录说明

| 路径 | 内容 |
|---|---|
| `interview/` | 面试 defense、QA 脚本、简历草稿、troubleshooting 笔记 |
| `plans/` | 已完成/过期的实施规划 |
| `resume-topics/` | 简历专题拆解（历史材料） |

## 关于旧 `book/` 站点

历史上仓库内有 VitePress 面试站 `book/` 与 `deploy-book.yml`。为减少 AI 误读（部分页面仍写 MySQL/Milvus 为默认栈），已从 git 跟踪移除并关闭自动部署。

独立仓库（面试站专用，非产品真相）：

- https://github.com/junnhwan/vidlens-book-site

本机若仍有 `vid-lens/book/`，只是本地残留或嵌套 clone，**不能覆盖代码与维护地图**。建议在主仓外单独 clone 该仓，不要把面试站再塞回产品仓。

归档不等于作废：面试时仍可用，但**不能覆盖代码与维护地图**。
