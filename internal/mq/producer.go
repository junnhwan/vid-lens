package mq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// AnalyzePayload 任务消息载荷
type AnalyzePayload struct {
	TaskID     int64  `json:"task_id"`
	MD5        string `json:"md5"`
	TraceID    string `json:"trace_id"`
	ClaimToken string `json:"claim_token,omitempty"`
	BudgetID   string `json:"budget_id,omitempty"`
}

type DownloadPayload struct {
	TaskID     int64  `json:"task_id"`
	Key        string `json:"key"`
	TraceID    string `json:"trace_id"`
	ClaimToken string `json:"claim_token,omitempty"`
	BudgetID   string `json:"budget_id,omitempty"`
}

type RAGIndexPayload struct {
	TaskID     int64  `json:"task_id"`
	TraceID    string `json:"trace_id"`
	ClaimToken string `json:"claim_token,omitempty"`
	BudgetID   string `json:"budget_id,omitempty"`
}

type retryBudgetContextKey struct{}

// ContextWithRetryBudgetID carries the durable retry-cycle identity into a
// message payload. It is exported so request services and internal handoffs use
// exactly the same context contract as the RetryScheduler.
func ContextWithRetryBudgetID(ctx context.Context, budgetID string) context.Context {
	if budgetID == "" {
		return ctx
	}
	return context.WithValue(ctx, retryBudgetContextKey{}, budgetID)
}

func RetryBudgetIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(retryBudgetContextKey{}).(string)
	return value
}

func contextWithRetryBudgetID(ctx context.Context, budgetID string) context.Context {
	return ContextWithRetryBudgetID(ctx, budgetID)
}

func retryBudgetIDFromContext(ctx context.Context) string {
	return RetryBudgetIDFromContext(ctx)
}

type claimTokenContextKey struct{}

// ContextWithClaimToken carries the database-owned dispatch lease into the
// message payload. HTTP initial dispatches and RetryScheduler redispatches must
// use the same contract so consumers can atomically hand it off to a processing
// lease.
func ContextWithClaimToken(ctx context.Context, token string) context.Context {
	if token == "" {
		return ctx
	}
	return context.WithValue(ctx, claimTokenContextKey{}, token)
}

func ClaimTokenFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	token, _ := ctx.Value(claimTokenContextKey{}).(string)
	return token
}

func contextWithClaimToken(ctx context.Context, token string) context.Context {
	return ContextWithClaimToken(ctx, token)
}

func claimTokenFromContext(ctx context.Context) string {
	return ClaimTokenFromContext(ctx)
}

// Producer RabbitMQ 生产者
//
// 选型理由（痛点驱动，非名气驱动）：
//
// vid-lens 用 MQ 的真实痛点是 ① 耗时任务出 HTTP 请求、② 失败可恢复
// （AI 服务挂任务不丢）、③ 削峰（ASR 配额有限需排队）。吞吐量级是用户
// 级并发（个位到几十 QPS），不是日志管道（万级 TPS）——Kafka 的
// partition 并行、ISR 副本、高吞吐日志聚合在用户级并发下都用不上，
// 所以选 RabbitMQ 而非 Kafka（决策与故障矩阵见 docs/decisions/02-dispatch-consistency.md）。
//
// RabbitMQ 的 ack 重投 / 死信队列 / 优先级 / 路由天然咬合"任务可靠投递
// + 失败可恢复 + ASR 排队"痛点。配置：classic persistent queue + publisher
// confirm + manual ack + delivery_mode=2 持久化。不上 quorum queue（基于
// Raft-backed quorum queues are intentionally outside the current deployment scope.
//
// Boundary with the dispatch lease:
// RabbitMQ publisher confirm 保证消息从 publisher 到 queue 的投递（异步
// 回调确认），但 confirm 回调是异步的——进程崩在 confirm 回来前消息
// 就丢了。dispatch lease（投递一致性 lease，transactional outbox 等价）
// 填这个窗口：业务事务内同表写 lease，提交后 publish，进程在 commit 与
// publisher confirm 回调之间崩溃由 RetryScheduler 发现过期 lease 补投。
// 这层分工是必须写清的，否则 publisher confirm 看起来已足够、dispatch
// lease 像多余——实则补的是 confirm 的盲区。
type Producer struct {
	conn         *amqp.Connection
	ch           *amqp.Channel
	queues       map[string]string // jobType -> queue name
	confirmCtx   context.Context
	confirmStop  context.CancelFunc
	returns      chan amqp.Return
	confirmsDone chan error
}

// NewProducer 创建 RabbitMQ 生产者。连接与 channel 建立失败即返回错误，
// 不留半初始化状态。Publisher confirm 模式：PublishWithDeferredConfirm
// 异步返回 confirm 句柄，不阻塞等回调——正是为了保留"confirm 回调前进程
// 崩"窗口由 dispatch lease 兜底的语义（同步 WaitForConfirms 会消掉这个窗口）。
//
// 但 confirm 不是 fire-and-forget：一个后台 goroutine 消费 NotifyConfirm 的
// ack/nack 流，nack（broker 拒绝）被记日志。这是分工叙事里 RabbitMQ 原生
// 兜底的那一半——dispatch lease 补的是 confirm 回调前的窗口，confirm 本身
// 仍要被观测，否则 broker 侧等于 no-op。同时 mandatory=true + Return 监听
// 捕获路由不到队列的 publish，避免消息静默丢失。
func NewProducer(brokers []string, analyzeQueue, transcribeQueue, downloadQueue string, ragIndexQueue ...string) (*Producer, error) {
	if len(brokers) == 0 {
		return nil, fmt.Errorf("mq brokers must not be empty")
	}
	addr := strings.TrimSpace(brokers[0])
	if addr == "" {
		return nil, fmt.Errorf("mq broker must not be empty")
	}
	conn, err := amqp.Dial("amqp://" + addr)
	if err != nil {
		return nil, fmt.Errorf("dial rabbitmq %s: %w", addr, err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("open rabbitmq channel: %w", err)
	}
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("enable publisher confirm: %w", err)
	}
	// Drain publisher confirms: ack = delivered, nack = broker rejected.
	ackChan, nackChan := ch.NotifyConfirm(make(chan uint64, 64), make(chan uint64, 64))
	// Capture unroutable publishes (mandatory=true routes these to Return).
	returns := ch.NotifyReturn(make(chan amqp.Return, 64))
	ctx, stop := context.WithCancel(context.Background())
	p := &Producer{
		conn: conn, ch: ch,
		queues: map[string]string{
			TaskJobAnalyze:    analyzeQueue,
			TaskJobTranscribe: transcribeQueue,
			TaskJobDownload:   downloadQueue,
		},
		confirmCtx: ctx, confirmStop: stop,
		returns: returns,
	}
	if len(ragIndexQueue) > 0 && ragIndexQueue[0] != "" {
		p.queues[TaskJobRAGIndex] = ragIndexQueue[0]
	}
	go p.drainConfirms(ackChan, nackChan)
	return p, nil
}

// drainConfirms observes publisher confirms so the broker-side half of the
// 投递一致性分工 is real, not a no-op. Acks are a no-op (the message reached
// the queue); nacks mean the broker rejected the message and are logged —
// recovery is the dispatch lease / RetryScheduler's job, not the publisher's.
func (p *Producer) drainConfirms(ack, nack chan uint64) {
	for {
		select {
		case <-p.confirmCtx.Done():
			return
		case tag, ok := <-ack:
			if !ok {
				return
			}
			_ = tag
		case tag, ok := <-nack:
			if !ok {
				return
			}
			slog.Default().Warn("rabbitmq publisher confirm nack",
				slog.Uint64("delivery_tag", tag))
		case ret, ok := <-p.returns:
			if !ok {
				return
			}
			slog.Default().Warn("rabbitmq publish unroutable",
				slog.String("exchange", ret.Exchange), slog.String("routing_key", ret.RoutingKey),
				slog.Int("reply_code", int(ret.ReplyCode)), slog.String("reply_text", ret.ReplyText))
		}
	}
}

// EnqueueAnalyze 投递视频分析任务。MessageId = "<jobType>:<taskID>" 作为
// 消费侧幂等键：同一消息重复投递被 Redis SETNX 挡住，retry 产生新 taskID
// 时键不同故放行。
func (p *Producer) EnqueueAnalyze(ctx context.Context, taskID int64, md5 string) error {
	payload, _ := json.Marshal(AnalyzePayload{
		TaskID:     taskID,
		MD5:        md5,
		TraceID:    TraceIDFromContext(ctx),
		ClaimToken: claimTokenFromContext(ctx),
		BudgetID:   retryBudgetIDFromContext(ctx),
	})
	return p.publish(TaskJobAnalyze, taskID, payload)
}

// EnqueueTranscribe 投递文字提取任务。
func (p *Producer) EnqueueTranscribe(ctx context.Context, taskID int64, md5 string) error {
	payload, _ := json.Marshal(AnalyzePayload{
		TaskID:     taskID,
		MD5:        md5,
		TraceID:    TraceIDFromContext(ctx),
		ClaimToken: claimTokenFromContext(ctx),
		BudgetID:   retryBudgetIDFromContext(ctx),
	})
	return p.publish(TaskJobTranscribe, taskID, payload)
}

func (p *Producer) EnqueueDownload(ctx context.Context, taskID int64, key string) error {
	payload, _ := json.Marshal(DownloadPayload{
		TaskID:     taskID,
		Key:        key,
		TraceID:    TraceIDFromContext(ctx),
		ClaimToken: claimTokenFromContext(ctx),
		BudgetID:   retryBudgetIDFromContext(ctx),
	})
	return p.publish(TaskJobDownload, taskID, payload)
}

func (p *Producer) EnqueueRAGIndex(ctx context.Context, taskID int64) error {
	queue := p.queues[TaskJobRAGIndex]
	if queue == "" {
		return fmt.Errorf("RAG 索引 RabbitMQ 队列未配置")
	}
	payload, _ := json.Marshal(RAGIndexPayload{
		TaskID:     taskID,
		TraceID:    TraceIDFromContext(ctx),
		ClaimToken: claimTokenFromContext(ctx),
		BudgetID:   retryBudgetIDFromContext(ctx),
	})
	return p.publish(TaskJobRAGIndex, taskID, payload)
}

// publish 用 PublishWithDeferredConfirm 异步投递。返回 nil 仅表示消息已
// 写入 socket buffer，publisher confirm 回调前进程崩的消息由 dispatch lease
// 兜底（见 Producer 选型注释的分工边界）。delivery_mode=2 持久化消息，
// MessageId 作为消费侧幂等键。
func (p *Producer) publish(jobType string, taskID int64, body []byte) error {
	queue := p.queues[jobType]
	if queue == "" {
		return fmt.Errorf("mq queue for job type %q not configured", jobType)
	}
	messageID := fmt.Sprintf("%s:%d", jobType, taskID)
	_, err := p.ch.PublishWithDeferredConfirm(
		"",    // default exchange
		queue, // routing key = queue name
		true,  // mandatory: unroutable publishes come back via Return (drainConfirms logs them)
		false, // immediate
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent, // = 2, 持久化消息
			MessageId:    messageID,
			Timestamp:    time.Now(),
			Body:         body,
		},
	)
	if err != nil {
		return fmt.Errorf("publish to queue %s: %w", queue, err)
	}
	return nil
}

// Close 关闭生产者，停止 confirm/return 监听 goroutine 并释放连接。
func (p *Producer) Close() error {
	var errs []error
	if p.confirmStop != nil {
		p.confirmStop()
	}
	if p.ch != nil {
		if err := p.ch.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close rabbitmq channel: %w", err))
		}
	}
	if p.conn != nil {
		if err := p.conn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close rabbitmq connection: %w", err))
		}
	}
	return errors.Join(errs...)
}

// QueueSpec describes a job queue to be declared. Only Name is required; the DLQ
// and DLX binding are derived from it.
type QueueSpec struct {
	Name string
}

// DeclareQueues declares each job queue as a classic persistent queue plus its
// dead-letter queue. Messages whose consumer retries exhaust MaxRetries are
// routed to video-<queue>-dlq via the x-dead-letter-exchange binding. Idempotent:
// re-declaring a queue with identical parameters is a no-op.
func DeclareQueues(brokers []string, specs []QueueSpec) error {
	if len(brokers) == 0 {
		return fmt.Errorf("mq brokers must not be empty")
	}
	addr := strings.TrimSpace(brokers[0])
	if addr == "" {
		return fmt.Errorf("mq broker must not be empty")
	}
	if len(specs) == 0 {
		return fmt.Errorf("mq queue specs must not be empty")
	}
	conn, err := amqp.Dial("amqp://" + addr)
	if err != nil {
		return fmt.Errorf("dial rabbitmq %s: %w", addr, err)
	}
	defer func() { _ = conn.Close() }()
	ch, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("open rabbitmq channel: %w", err)
	}
	defer func() { _ = ch.Close() }()
	for _, spec := range specs {
		name := strings.TrimSpace(spec.Name)
		if name == "" {
			return fmt.Errorf("mq queue name must not be empty")
		}
		dlq := name + "-dlq"
		dlx := name + "-dlx"
		// Dead-letter exchange: a direct exchange that routes to the DLQ.
		if err := ch.ExchangeDeclare(dlx, "direct", true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare dlx %s: %w", dlx, err)
		}
		if _, err := ch.QueueDeclare(dlq, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare dlq %s: %w", dlq, err)
		}
		if err := ch.QueueBind(dlq, name, dlx, false, nil); err != nil {
			return fmt.Errorf("bind dlq %s to dlx %s: %w", dlq, dlx, err)
		}
		// Job queue: durable, dead-letters to the per-queue DLX.
		args := amqp.Table{"x-dead-letter-exchange": dlx}
		if _, err := ch.QueueDeclare(name, true, false, false, false, args); err != nil {
			return fmt.Errorf("declare queue %s: %w", name, err)
		}
	}
	return nil
}

// PingBroker checks that the RabbitMQ broker accepts a connection. It does not
// declare queues or perform any writes, so it is safe to use from a readiness
// probe.
func PingBroker(ctx context.Context, brokers []string) error {
	if len(brokers) == 0 {
		return fmt.Errorf("mq brokers must not be empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var lastErr error
	for _, rawBroker := range brokers {
		broker := strings.TrimSpace(rawBroker)
		if broker == "" {
			lastErr = fmt.Errorf("mq broker must not be empty")
			continue
		}
		conn, err := amqp.Dial("amqp://" + broker)
		if err != nil {
			lastErr = fmt.Errorf("dial rabbitmq broker %s: %w", broker, err)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		if err := conn.Close(); err != nil {
			lastErr = fmt.Errorf("close rabbitmq broker connection: %w", err)
			continue
		}
		return nil
	}
	if lastErr == nil {
		return fmt.Errorf("mq brokers must not be empty")
	}
	return lastErr
}
