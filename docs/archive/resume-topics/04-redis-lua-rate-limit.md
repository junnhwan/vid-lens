# 专题 4：Redis Lua 令牌桶限流

## 1. 推荐简历表述

使用 Redis Lua 实现按“路由 + 用户/IP”隔离的令牌桶，并支持高成本接口覆盖默认容量和补充速率；Redis 异常时选择可观测的 fail-open，避免限流组件成为业务单点。

## 2. 面试口语答案

> VidLens 的上传、分析、转写和问答成本差异很大，不能只做一个全局计数器。当前中间件根据 Gin 路由和登录用户 ID 生成桶 key；未登录场景退化到客户端 IP。每条路由可以覆盖默认 capacity 和 rate，所以高成本 AI 接口可以比普通查询更严格。
>
> 令牌数和最后更新时间保存在 Redis Hash。一次 Lua 调用里完成读取、按经过时间补充 token、判断是否消费、写回状态和设置 TTL，避免“读 token—计算—写 token”被并发请求穿透。被拒绝时返回 429 和 `Retry-After`。
>
> Redis 故障时当前策略是 fail-open，因为限流是保护层而不是数据正确性来源。这个选择会牺牲故障期间的流量保护，所以代码会记录结构化错误和 `fail_open` 指标。生产环境还应在网关/WAF 增加独立限流，而不是只依赖应用内 Redis。

## 3. 当前算法

```text
key = route + userID
   或 route + clientIP

Lua:
  HMGET tokens,last_time
  -> 根据 elapsed * rate 补 token
  -> min(capacity, tokens)
  -> tokens >= 1 时消费一个
  -> HMSET + EXPIRE 60
```

令牌桶允许容量范围内的短时突发，再按 rate 恢复；它与 Kafka 的任务缓冲不是一回事：前者在入口拒绝过量请求，后者承接已经接受的异步任务。

## 4. 高频追问

### 为什么使用 Lua？

> 因为补充、判断、扣减和写回必须作为一个原子操作执行。拆成多条 Redis 命令会让并发请求读到同一份 token。

### 为什么按路由和用户？

> 只按用户会让查询接口和昂贵 AI 接口共享同一桶；只按路由会让所有用户互相影响。组合维度能隔离用户，也能按成本配置不同上限。

### fail-open 会不会打穿服务？

> 会增加 Redis 故障期间的风险，所以它不是完整防护。当前取舍是避免 Redis 抖动让所有 API 不可用，并通过指标暴露降级；更强方案是在反向代理和基础设施层另设限流或熔断。

### 能否控制用户 AI 费用？

> 不能单独做到。请求限流控制频率，不等于 token 配额、日预算或账单。项目还有调用日志、usage ledger 和日聚合，但不能称为生产级计费系统。

### Lua 会阻塞 Redis 吗？

> Redis 执行脚本期间是原子的，所以脚本必须短。当前脚本只有固定数量的 Hash 读写和算术，没有循环或大 key 扫描。

## 5. 代码证据

- `internal/middleware/ratelimit.go`：Lua、key、路由覆盖、fail-open 和 429 响应。
- `internal/middleware/ratelimit_test.go`：桶消费、补充、路由隔离和错误策略。
- `cmd/server/wiring.go`：从配置注册 route overrides。
- `internal/observability/metrics.go`：allowed/rejected/fail_open 指标。

## 6. 当前限制

- 依赖客户端 IP 识别时，需要正确配置可信代理，否则 IP 维度可能不准确。
- TTL 固定为 60 秒，超低 rate 的长期桶需要重新评估过期策略。
- 没有网关级全局配额、跨区域一致限流或生产级计费。
