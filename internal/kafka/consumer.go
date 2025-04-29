package kafka

import (
	"context"
	"encoding/json"
	"skeleton-internship-backend/config"
	"skeleton-internship-backend/internal/model"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/segmentio/kafka-go"
	"go.uber.org/fx"
)

type LogConsumer interface {
	FetchMessage(ctx context.Context) (*model.LogEntry, kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

type kafkaLogConsumer struct {
	reader *kafka.Reader
}

func NewKafkaLogConsumer(lc fx.Lifecycle, cfg *config.Config) (LogConsumer, error) {

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        cfg.Kafka.Brokers,
		GroupID:        cfg.Kafka.ConsumerGroup,
		Topic:          cfg.Kafka.LogTopic,
		MinBytes:       10e3,             // 10KB
		MaxBytes:       10e6,             // 10MB
		MaxWait:        10 * time.Second, // Wait up to 10 second for data
		CommitInterval: 0,
		StartOffset:    kafka.FirstOffset,
	})
	c := &kafkaLogConsumer{
		reader: reader,
	}
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			log.Info().Str("group", cfg.Kafka.ConsumerGroup).Msg("Closing Kafka consumer")
			return c.Close()
		},
	})
	log.Info().
		Strs("brokers", cfg.Kafka.Brokers).
		Str("topic", cfg.Kafka.LogTopic).
		Str("group", cfg.Kafka.ConsumerGroup).
		Msg("Kafka consumer initialized")
	return c, nil
}

func (c *kafkaLogConsumer) FetchMessage(ctx context.Context) (*model.LogEntry, kafka.Message, error) {
	msg, err := c.reader.FetchMessage(ctx)
	if err != nil {
		log.Debug().Msg("Fail when fetching Kafka message.")
		return nil, kafka.Message{}, err
	}
	log.Debug().
		Str("topic", msg.Topic).
		Int("partition", msg.Partition).
		Int64("offset", msg.Offset).
		Msg("Fetched message from Kafka")
	var logEntry model.LogEntry
	if err := json.Unmarshal(msg.Value, &logEntry); err != nil {
		log.Error().Err(err).Int64("offset", msg.Offset).Msg("Failed to unmarshal Kafka message value")
		return nil, msg, err
	}
	return &logEntry, msg, nil
}

func (c *kafkaLogConsumer) CommitMessages(ctx context.Context, msgs ...kafka.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	err := c.reader.CommitMessages(ctx, msgs...)
	if err != nil {
		log.Error().Err(err).Int("count", len(msgs)).Msg("Failed to commit Kafka messages")
		return err
	}
	log.Debug().Int("count", len(msgs)).Int64("last_offset", msgs[len(msgs)-1].Offset).Msg("Committed Kafka messages")
	return nil
}
func (c *kafkaLogConsumer) Close() error {
	return c.reader.Close()
}
