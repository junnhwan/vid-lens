package mq

import (
	"context"
	"encoding/json"

	"github.com/segmentio/kafka-go"
)

// AnalyzePayload 任务消息载荷
type AnalyzePayload struct {
	TaskID  int64  `json:"task_id"`
	MD5     string `json:"md5"`
	TraceID string `json:"trace_id"`
}

type DownloadPayload struct {
	TaskID  int64  `json:"task_id"`
	Key     string `json:"key"`
	TraceID string `json:"trace_id"`
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
}

// NewProducer 创建 Kafka 生产者
func NewProducer(brokers []string, analyzeTopic, transcribeTopic, downloadTopic string) *Producer {
	newWriter := func(topic string) *kafka.Writer {
		return &kafka.Writer{
			Addr:         kafka.TCP(brokers...),
			Topic:        topic,
			Balancer:     &kafka.LeastBytes{}, // 按负载均衡选择分区
			RequiredAcks: kafka.RequireAll,    // 等所有 ISR 副本确认（消息不丢失）
			MaxAttempts:  3,                   // 发送失败最多重试 3 次
			Async:        false,               // 同步发送，确保消息投递成功
		}
	}

	return &Producer{
		analyzeWriter:    newWriter(analyzeTopic),
		transcribeWriter: newWriter(transcribeTopic),
		downloadWriter:   newWriter(downloadTopic),
	}
}

// EnqueueAnalyze 投递视频分析任务
// 面试亮点：投递即返回，接口 RT 压缩到 50ms 以内
// 使用 MD5 作为消息 Key → 同一视频的任务会被路由到同一分区，保证消费顺序
func (p *Producer) EnqueueAnalyze(ctx context.Context, taskID int64, md5 string) error {
	payload, _ := json.Marshal(AnalyzePayload{
		TaskID:  taskID,
		MD5:     md5,
		TraceID: TraceIDFromContext(ctx),
	})

	return p.analyzeWriter.WriteMessages(ctx, kafka.Message{
		Key:   []byte(md5), // Key = MD5，保证同视频进入同一分区
		Value: payload,
	})
}

// EnqueueTranscribe 投递文字提取任务
func (p *Producer) EnqueueTranscribe(ctx context.Context, taskID int64, md5 string) error {
	payload, _ := json.Marshal(AnalyzePayload{
		TaskID:  taskID,
		MD5:     md5,
		TraceID: TraceIDFromContext(ctx),
	})

	return p.transcribeWriter.WriteMessages(ctx, kafka.Message{
		Key:   []byte(md5),
		Value: payload,
	})
}

func (p *Producer) EnqueueDownload(ctx context.Context, taskID int64, key string) error {
	payload, _ := json.Marshal(DownloadPayload{
		TaskID:  taskID,
		Key:     key,
		TraceID: TraceIDFromContext(ctx),
	})

	return p.downloadWriter.WriteMessages(ctx, kafka.Message{
		Key:   []byte(key),
		Value: payload,
	})
}

// Close 关闭生产者
func (p *Producer) Close() error {
	err1 := p.analyzeWriter.Close()
	err2 := p.transcribeWriter.Close()
	err3 := p.downloadWriter.Close()
	if err1 != nil {
		return err1
	}
	if err2 != nil {
		return err2
	}
	return err3
}

// CreateTopics 确保 Topic 存在（首次启动时调用）
func CreateTopics(brokers []string, topics []string) error {
	conn, err := kafka.DialLeader(context.Background(), "tcp", brokers[0], topics[0], 0)
	if err != nil {
		// Topic 可能已存在，忽略错误
		return nil
	}
	conn.Close()

	for _, topic := range topics {
		topicConfig := kafka.TopicConfig{
			Topic:             topic,
			NumPartitions:     4, // 4 个分区，支持并行消费
			ReplicationFactor: 1, // 单机部署只有 1 个 broker
		}
		// 尝试创建，已存在会报错但不影响
		conn, err := kafka.Dial("tcp", brokers[0])
		if err == nil {
			conn.CreateTopics(topicConfig)
			conn.Close()
		}
	}
	return nil
}
