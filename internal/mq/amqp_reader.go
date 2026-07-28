package mq

import (
	"context"
	"fmt"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
)

// amqpReader is the production messageReader over a real RabbitMQ connection.
// One connection+channel per reader; manual ack, prefetch capped by config.
type amqpReader struct {
	conn   *amqp.Connection
	ch     *amqp.Channel
	queue  string
	tag    string
	deliv  <-chan amqp.Delivery
	closeo sync.Once
	closed error
}

func newAmqpReader(amqpURL, queue, groupID string, prefetch int) *amqpReader {
	r := &amqpReader{queue: queue, tag: groupID}
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		r.closed = fmt.Errorf("dial rabbitmq %s: %w", amqpURL, err)
		return r
	}
	r.conn = conn
	ch, err := conn.Channel()
	if err != nil {
		r.closed = fmt.Errorf("open rabbitmq channel: %w", err)
		_ = conn.Close()
		return r
	}
	if prefetch <= 0 {
		prefetch = 1
	}
	if err := ch.Qos(prefetch, 0, false); err != nil {
		r.closed = fmt.Errorf("set rabbitmq prefetch: %w", err)
		_ = ch.Close()
		_ = conn.Close()
		return r
	}
	r.ch = ch
	return r
}

func (r *amqpReader) Consume(ctx context.Context) (<-chan amqp.Delivery, error) {
	if r.closed != nil {
		return nil, r.closed
	}
	if r.ch == nil {
		return nil, fmt.Errorf("rabbitmq channel not open")
	}
	deliveries, err := r.ch.Consume(r.queue, r.tag, false, false, false, false, nil)
	if err != nil {
		return nil, fmt.Errorf("consume queue %s: %w", r.queue, err)
	}
	r.deliv = deliveries
	return deliveries, nil
}

func (r *amqpReader) Close() error {
	var errs []error
	r.closeo.Do(func() {
		if r.ch != nil {
			if err := r.ch.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		if r.conn != nil {
			if err := r.conn.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	})
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("close amqp reader: %v", errs[0])
}
