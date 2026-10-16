# 开发与部署入口

## 项目结构

后端是仓库根目录的 Go module，入口为 cmd/vidlens，业务模块位于 internal。前端使用 React、TypeScript 和 Vite，位于 frontend。PostgreSQL 保存应用状态，Redis 与 Asynq 负责工作投递和资源协调。

## 本地验证

后端资源代码包含 Unix 系统调用，Windows 使用 Linux 或 Docker 环境运行。集成测试需要隔离的 PostgreSQL、Redis 和 FFmpeg。

```sh
go test ./cmd/... ./internal/...
go vet ./cmd/... ./internal/...
go build ./cmd/vidlens
```

前端构建包含 TypeScript 检查。

```sh
cd frontend
npm ci
npm test
npm run build
```

## 配置与管理

请从仓库 README 和 config-examples 开始设置运行环境。配置、凭据、Cookie、数据库和媒体目录都应留在 Git 外。站长密码通过服务器交互式命令设置或重置，不使用默认密码。

```sh
vidlens -config /path/to/private.yaml -set-owner-password
```

模型发现仅拉取名字；启用前需要当前修订通过真实图片、工具调用和结构化提交检查。探测会产生真实模型调用，缓存字段、命中证据和账单折扣应分别核对。

## 更多实现说明

完整源码、部署说明和后端设计在 [GitHub 项目](https://github.com/junnhwan/vid-lens)。此站点只提供使用与实现文档，不发布私人交接或服务器凭据。
