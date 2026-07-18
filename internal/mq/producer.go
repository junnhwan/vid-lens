package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
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
// Kafka payload. It is exported so request services and internal handoffs use
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
// Kafka payload. HTTP initial dispatches and RetryScheduler redispatches must
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

// Producer Kafka 生产者
// 面试亮点（选型理由）：
//
//	为什么选 Kafka 而不是 RocketMQ / RabbitMQ？
//	1. Kafka 是 Go 后端生态中最主流的 MQ，社区活跃，Go 客户端成熟
//	2. 天然支持消息持久化（磁盘落盘），不怕宕机丢消息
//	3. 基于拉取模式消费，消费者按自己的节奏处理，天然削峰
//	4. 分区机制支持水平扩展，未来增加消费者实例就能提升吞吐
//	不选 RocketMQ 的理由：Go 客户端不够成熟，更偏 Java 生态
//	不选 RabbitMQ 的理由：海量消息堆积能力不如 Kafka，Erlang 底层不好排查问题
type Producer struct {
	analyzeWriter    *kafka.Writer
	transcribeWriter *kafka.Writer
	downloadWriter   *kafka.Writer
	ragIndexWriter   *kafka.Writer
}

// NewProducer 创建 Kafka 生产者
func NewProducer(brokers []string, analyzeTopic, transcribeTopic, downloadTopic string, ragIndexTopic ...string) *Producer {
	newWriter := func(topic string) *kafka.Writer {
		if topic == "" {
			return nil
		}
		return &kafka.Writer{
			Addr:         kafka.TCP(brokers...),
			Topic:        topic,
			Balancer:     &kafka.LeastBytes{}, // 按负载均衡选择分区
			RequiredAcks: kafka.RequireAll,    // 等所有 ISR 副本确认（消息不丢失）
			MaxAttempts:  3,                   // 发送失败最多重试 3 次
			Async:        false,               // 同步发送，确保消息投递成功
		}
	}

	var ragTopic string
	if len(ragIndexTopic) > 0 {
		ragTopic = ragIndexTopic[0]
	}
	return &Producer{
		analyzeWriter:    newWriter(analyzeTopic),
		transcribeWriter: newWriter(transcribeTopic),
		downloadWriter:   newWriter(downloadTopic),
		ragIndexWriter:   newWriter(ragTopic),
	}
}

// EnqueueAnalyze 投递视频分析任务
// 面试亮点：投递即返回，接口 RT 压缩到 50ms 以内
// 使用 MD5 作为消息 Key → 同一视频的任务会被路由到同一分区，保证消费顺序
func (p *Producer) EnqueueAnalyze(ctx context.Context, taskID int64, md5 string) error {
	payload, _ := json.Marshal(AnalyzePayload{
		TaskID:     taskID,
		MD5:        md5,
		TraceID:    TraceIDFromContext(ctx),
		ClaimToken: claimTokenFromContext(ctx),
		BudgetID:   retryBudgetIDFromContext(ctx),
	})

	return p.analyzeWriter.WriteMessages(ctx, kafka.Message{
		Key:   []byte(md5), // Key = MD5，保证同视频进入同一分区
		Value: payload,
	})
}

// EnqueueTranscribe 投递文字提取任务
func (p *Producer) EnqueueTranscribe(ctx context.Context, taskID int64, md5 string) error {
	payload, _ := json.Marshal(AnalyzePayload{
		TaskID:     taskID,
		MD5:        md5,
		TraceID:    TraceIDFromContext(ctx),
		ClaimToken: claimTokenFromContext(ctx),
		BudgetID:   retryBudgetIDFromContext(ctx),
	})

	return p.transcribeWriter.WriteMessages(ctx, kafka.Message{
		Key:   []byte(md5),
		Value: payload,
	})
}

func (p *Producer) EnqueueDownload(ctx context.Context, taskID int64, key string) error {
	payload, _ := json.Marshal(DownloadPayload{
		TaskID:     taskID,
		Key:        key,
		TraceID:    TraceIDFromContext(ctx),
		ClaimToken: claimTokenFromContext(ctx),
		BudgetID:   retryBudgetIDFromContext(ctx),
	})

	return p.downloadWriter.WriteMessages(ctx, kafka.Message{
		Key:   []byte(key),
		Value: payload,
	})
}

func (p *Producer) EnqueueRAGIndex(ctx context.Context, taskID int64) error {
	if p.ragIndexWriter == nil {
		return fmt.Errorf("RAG 索引 Kafka topic 未配置")
	}
	return p.ragIndexWriter.WriteMessages(ctx, newRAGIndexMessage(ctx, taskID))
}

func newRAGIndexMessage(ctx context.Context, taskID int64) kafka.Message {
	payload, _ := json.Marshal(RAGIndexPayload{
		TaskID:     taskID,
		TraceID:    TraceIDFromContext(ctx),
		ClaimToken: claimTokenFromContext(ctx),
		BudgetID:   retryBudgetIDFromContext(ctx),
	})
	key := fmt.Sprint(taskID)
	return kafka.Message{
		Key:   []byte(key),
		Value: payload,
	}
}

// Close 关闭生产者
func (p *Producer) Close() error {
	err1 := closeWriter(p.analyzeWriter)
	err2 := closeWriter(p.transcribeWriter)
	err3 := closeWriter(p.downloadWriter)
	err4 := closeWriter(p.ragIndexWriter)
	if err1 != nil {
		return err1
	}
	if err2 != nil {
		return err2
	}
	if err3 != nil {
		return err3
	}
	return err4
}

func closeWriter(w *kafka.Writer) error {
	if w == nil {
		return nil
	}
	return w.Close()
}

// CreateTopics ensures the configured topics exist. kafka-go treats an existing
// topic as an idempotent success; connectivity, authorization, and invalid
// configuration errors are returned to the caller instead of being hidden.
func CreateTopics(brokers []string, topics []string) error {
	if len(brokers) == 0 {
		return fmt.Errorf("kafka brokers must not be empty")
	}
	broker := strings.TrimSpace(brokers[0])
	if broker == "" {
		return fmt.Errorf("kafka broker must not be empty")
	}
	if len(topics) == 0 {
		return fmt.Errorf("kafka topics must not be empty")
	}

	seen := make(map[string]struct{}, len(topics))
	configs := make([]kafka.TopicConfig, 0, len(topics))
	for _, rawTopic := range topics {
		topic := strings.TrimSpace(rawTopic)
		if topic == "" {
			return fmt.Errorf("kafka topic must not be empty")
		}
		if _, exists := seen[topic]; exists {
			continue
		}
		seen[topic] = struct{}{}
		configs = append(configs, kafka.TopicConfig{
			Topic:             topic,
			NumPartitions:     4,
			ReplicationFactor: 1,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	bootstrap, err := kafka.DialContext(ctx, "tcp", broker)
	if err != nil {
		return fmt.Errorf("connect kafka broker %s: %w", broker, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = bootstrap.SetDeadline(deadline)
	}
	controller, err := bootstrap.Controller()
	closeErr := bootstrap.Close()
	if err != nil {
		return fmt.Errorf("discover kafka controller: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close kafka bootstrap connection: %w", closeErr)
	}

	controllerAddress := net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port))
	conn, err := kafka.DialContext(ctx, "tcp", controllerAddress)
	if err != nil {
		return fmt.Errorf("connect kafka controller %s: %w", controllerAddress, err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if err := conn.CreateTopics(configs...); err != nil {
		return fmt.Errorf("create kafka topics: %w", err)
	}
	return nil
}

// PingBroker checks that at least one configured Kafka broker accepts a TCP
// connection. It deliberately does not create topics or perform any writes so
// it is safe to use from a readiness probe.
func PingBroker(ctx context.Context, brokers []string) error {
	if len(brokers) == 0 {
		return fmt.Errorf("kafka brokers must not be empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var lastErr error
	for _, rawBroker := range brokers {
		broker := strings.TrimSpace(rawBroker)
		if broker == "" {
			lastErr = fmt.Errorf("kafka broker must not be empty")
			continue
		}

		conn, err := kafka.DialContext(ctx, "tcp", broker)
		if err != nil {
			lastErr = fmt.Errorf("connect kafka broker %s: %w", broker, err)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		if err := conn.Close(); err != nil {
			lastErr = fmt.Errorf("close kafka broker connection: %w", err)
			continue
		}
		return nil
	}
	if lastErr == nil {
		return fmt.Errorf("kafka brokers must not be empty")
	}
	return lastErr
}
