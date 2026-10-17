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

## 运行环境

请从仓库 README、config.example.yaml 和 config-examples 中的示例开始设置运行环境。运行配置、凭据、Cookie、数据库和媒体目录应留在 Git 外。

浏览器页面与 API 需要使用同一来源，代理应保留 Cookie、CSRF 和 SSE 头，并关闭 SSE 缓冲。模型服务需要支持图片输入与流式工具调用；ASR 使用独立的同步音频转写接口。实际可用性取决于所接服务与平台访问条件。

## 更多实现说明

完整源码、部署说明和后端设计在 [GitHub 项目](https://github.com/junnhwan/vid-lens)。阅读指南介绍使用方式，仓库文档补充请求协议、证据校验与执行恢复设计。
