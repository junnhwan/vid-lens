# VidLens RAG Eval Cases Example

> 示例数据只用于说明格式，不包含真实用户视频内容。

```yaml
- task_hint: "操作系统课程视频 01"
  question: "视频里怎么解释进程和线程的区别？"
  expected_chunk_keywords:
    - "进程"
    - "线程"
  expected_answer_points:
    - "资源分配"
    - "调度"

- task_hint: "后端项目复盘视频 02"
  question: "为什么分布式锁释放时要校验 owner？"
  expected_chunk_keywords:
    - "分布式锁"
    - "owner"
  expected_answer_points:
    - "避免误删别人的锁"
    - "锁续期或任务超时后可能发生 owner 变化"

- task_hint: "限流设计讲解视频 03"
  question: "令牌桶限流适合解决什么问题？"
  expected_chunk_keywords:
    - "令牌桶"
    - "限流"
  expected_answer_points:
    - "控制请求速率"
    - "允许一定突发流量"
```
