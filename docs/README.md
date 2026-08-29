# VidLens 工程文档

本目录只保留当前项目的公开工程资料，不放简历、面试准备、个人信息、临时交接文档或本地工作记录。

## 文档导航

- [架构总览](architecture/overview.md)
- [数据模型与存储边界](architecture/data-model.md)
- [检索与回答链路](architecture/retrieval.md)
- [可靠性与幂等](architecture/reliability.md)
- [架构图](images/vidlens-architecture.svg)
- [数据库关系图](architecture/database-schema.svg)
- [评测资料](eval/README.md)
- [压测与故障演练](operations/stress-testing.md)

## 目录约定

- `architecture/`：当前系统结构、模块边界、数据流和可靠性约束
- `eval/`：可复现的评测规范、配置和示例数据
- `operations/`：部署、压测和故障处理资料
- `images/`：README 和架构文档使用的图片资源

个人材料、未定稿内容和当前本地评测输入放在仓库根目录的 `docs-private/`，该目录不会提交到 Git。
