package mq

import (
	"context"
	"testing"
)

func TestDeclareQueuesRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		brokers []string
		specs   []QueueSpec
	}{
		{name: "missing brokers", specs: []QueueSpec{{Name: "video-analyze"}}},
		{name: "missing specs", brokers: []string{"127.0.0.1:5672"}},
		{name: "empty broker", brokers: []string{"  "}, specs: []QueueSpec{{Name: "video-analyze"}}},
		{name: "empty queue name", brokers: []string{"127.0.0.1:5672"}, specs: []QueueSpec{{Name: "  "}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := DeclareQueues(tt.brokers, tt.specs); err == nil {
				t.Fatal("DeclareQueues should reject invalid configuration")
			}
		})
	}
}

func TestPingBrokerRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		brokers []string
	}{
		{name: "missing brokers"},
		{name: "empty broker", brokers: []string{"  "}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := PingBroker(context.Background(), tt.brokers); err == nil {
				t.Fatal("PingBroker should reject invalid configuration")
			}
		})
	}
}

func TestNewProducerRejectsInvalidBrokers(t *testing.T) {
	if _, err := NewProducer(nil, "a", "b", "c", "d"); err == nil {
		t.Fatal("NewProducer should reject empty brokers")
	}
	if _, err := NewProducer([]string{"  "}, "a", "b", "c", "d"); err == nil {
		t.Fatal("NewProducer should reject empty broker")
	}
}
