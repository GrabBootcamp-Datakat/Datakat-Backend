package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"skeleton-internship-backend/config"
	"skeleton-internship-backend/internal/model"

	"github.com/rs/zerolog/log"
	"github.com/segmentio/kafka-go"
	"go.uber.org/fx"
)

type LogProducer interface {
	Produce(ctx context.Context, logs []model.LogEntry) error
	Close() error
}

type kafkaLogProducer struct {
	writer *kafka.Writer
	topic  string
}

func NewKafkaLogProducer(lc fx.Lifecycle, cfg *config.Config) (LogProducer, error) {
	if len(cfg.Kafka.Brokers) == 0 || cfg.Kafka.LogTopic == "" {
		log.Error().Msg("Kafka brokers or log topic is not configured.")
		return nil, errors.New("kafka configuration missing")
	}
	writer := kafka.NewWriter(kafka.WriterConfig{

		Brokers:      cfg.Kafka.Brokers,
		Topic:        cfg.Kafka.LogTopic,
		Balancer:     &kafka.LeastBytes{},
		BatchSize:    cfg.LogProcessor.BatchSize,
		BatchTimeout: cfg.LogProcessor.MaxBatchWait,
		Async:        true,
	})
	p := &kafkaLogProducer{
		writer: writer,
		topic:  cfg.Kafka.LogTopic,
	}
	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			log.Info().Msg("Closing Kafka producer")
			return p.Close()
		},
	})
	log.Info().Strs("brokers", cfg.Kafka.Brokers).Str("topic", cfg.Kafka.LogTopic).Msg("Kafka producer initialized")
	return p, nil
}

func (p *kafkaLogProducer) Produce(ctx context.Context, logs []model.LogEntry) error {
	if len(logs) == 0 {
		return nil
	}
	messages := make([]kafka.Message, len(logs))

	for i, LogEntry := range logs {
		key := []byte(LogEntry.Application)
		value, err := json.Marshal(LogEntry)

		if err != nil {
			log.Error().Err(err).Interface("log", logs).Msg("Failed to marshal log entry for Kafka")
			continue
		}
		messages[i] = kafka.Message{
			Key:   key,
			Value: value,
		}
	}
	if len(messages) == 0 {
		log.Warn().Msg("No valid messages to produce.")
		return nil
	}

	err := p.writer.WriteMessages(ctx, messages...)
	if err != nil {
		log.Error().Err(err).Int("message_count", len(messages)).Msg("Failed to write messages to Kafka")
		return err
	}

	log.Debug().Int("message_count", len(messages)).Str("topic", p.topic).Msg("Successfully produced messages to Kafka")

	return nil
}

func (p *kafkaLogProducer) Close() error {
	return p.writer.Close()
}
