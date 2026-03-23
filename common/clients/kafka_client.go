package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/IBM/sarama"
	"go.uber.org/zap"
)

const (
	TopicLocationEvents      = "lmf.location.events"
	TopicSubscriptions       = "lmf.subscriptions"
	TopicPositioningRequests = "lmf.positioning.requests"
)

// KafkaProducer wraps a sarama sync producer
type KafkaProducer struct {
	producer sarama.SyncProducer
	logger   *zap.Logger
}

// NewKafkaProducer creates a new Kafka sync producer
func NewKafkaProducer(brokers []string, logger *zap.Logger) (*KafkaProducer, error) {
	cfg := sarama.NewConfig()
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Producer.Retry.Max = 5
	cfg.Producer.Return.Successes = true
	cfg.Version = sarama.V3_0_0_0
	cfg.Net.DialTimeout = 10 * time.Second
	cfg.Net.WriteTimeout = 10 * time.Second

	producer, err := sarama.NewSyncProducer(brokers, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating kafka producer: %w", err)
	}

	return &KafkaProducer{producer: producer, logger: logger}, nil
}

// Publish sends a message to a Kafka topic
func (p *KafkaProducer) Publish(ctx context.Context, topic, key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshalling message: %w", err)
	}

	msg := &sarama.ProducerMessage{
		Topic: topic,
		Key:   sarama.StringEncoder(key),
		Value: sarama.ByteEncoder(data),
	}

	partition, offset, err := p.producer.SendMessage(msg)
	if err != nil {
		return fmt.Errorf("sending message to %s: %w", topic, err)
	}

	p.logger.Debug("published kafka message",
		zap.String("topic", topic),
		zap.String("key", key),
		zap.Int32("partition", partition),
		zap.Int64("offset", offset),
	)

	return nil
}

// Close closes the Kafka producer
func (p *KafkaProducer) Close() error {
	return p.producer.Close()
}

// KafkaConsumer wraps a sarama consumer group
type KafkaConsumer struct {
	group   sarama.ConsumerGroup
	topic   string
	logger  *zap.Logger
}

// NewKafkaConsumer creates a new Kafka consumer group
func NewKafkaConsumer(brokers []string, group, topic string, logger *zap.Logger) (*KafkaConsumer, error) {
	cfg := sarama.NewConfig()
	cfg.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRoundRobin()}
	cfg.Consumer.Offsets.Initial = sarama.OffsetNewest
	cfg.Version = sarama.V3_0_0_0

	consumerGroup, err := sarama.NewConsumerGroup(brokers, group, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating consumer group: %w", err)
	}

	return &KafkaConsumer{
		group:  consumerGroup,
		topic:  topic,
		logger: logger,
	}, nil
}

// Consume starts consuming messages, calling handler for each one
func (c *KafkaConsumer) Consume(ctx context.Context, handler func(key, value []byte) error) error {
	h := &consumerGroupHandler{handler: handler, logger: c.logger}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := c.group.Consume(ctx, []string{c.topic}, h); err != nil {
				c.logger.Error("consumer group error", zap.Error(err))
				return err
			}
		}
	}
}

// Close closes the Kafka consumer
func (c *KafkaConsumer) Close() error {
	return c.group.Close()
}

// consumerGroupHandler implements sarama.ConsumerGroupHandler
type consumerGroupHandler struct {
	handler func(key, value []byte) error
	logger  *zap.Logger
}

func (h *consumerGroupHandler) Setup(_ sarama.ConsumerGroupSession) error   { return nil }
func (h *consumerGroupHandler) Cleanup(_ sarama.ConsumerGroupSession) error { return nil }

func (h *consumerGroupHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		if err := h.handler(msg.Key, msg.Value); err != nil {
			h.logger.Error("message handler error",
				zap.String("topic", msg.Topic),
				zap.Int32("partition", msg.Partition),
				zap.Int64("offset", msg.Offset),
				zap.Error(err),
			)
		}
		session.MarkMessage(msg, "")
	}
	return nil
}
