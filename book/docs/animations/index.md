# 动画实验

> 这里放的是帮助面试前快速回忆链路的可视化页面。它们是自包含 HTML 文件，不依赖外部服务；打开后可用键盘推进步骤。

## 可视化入口

<div class="animation-grid">
  <a class="animation-card" href="./task-flow.html" target="_blank">
    <span class="animation-kicker">Async Pipeline</span>
    <strong>视频任务异步状态机</strong>
    <small>URL 上传、Kafka 投递、下载、转写、摘要、RAG 建索引的端到端链路。</small>
  </a>
  <a class="animation-card" href="./rag-fusion.html" target="_blank">
    <span class="animation-kicker">RAG Retrieval</span>
    <strong>向量 + BM25 + RRF 融合</strong>
    <small>演示 VidLens 为什么不只做纯向量检索，以及 RRF 如何按排名融合。</small>
  </a>
</div>

## 快捷键

| 键位 | 动作 |
|------|------|
| → / Space | 下一步 |
| ← | 上一步 |
| A | 自动播放 |
| R | 重置 |

## 对应源码

- 异步任务投递与消费：`internal/service/media.go:150`, `internal/mq/producer.go:77`, `internal/mq/consumer.go:194`, `internal/mq/retry.go:193`
- RAG 检索融合：`internal/service/chat.go:160`, `internal/service/chat.go:258`, `internal/repository/video_chunk.go:47`, `internal/service/retrieval_fusion.go:14`
