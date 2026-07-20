# 归档文档（默认不要读）

本目录存放**面试材料、历史规划、简历专题**。它们不是当前产品实现的事实源。

## 给后续 AI 的规则

- **默认不要读取** `docs/archive/**`、`docs/grill/**`、`book/**`。
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

归档不等于作废：面试时仍可用，但**不能覆盖代码与维护地图**。
