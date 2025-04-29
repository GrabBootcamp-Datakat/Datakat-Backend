package service

import (
	"context"
	"errors"
	"fmt"
	"skeleton-internship-backend/config"
	"skeleton-internship-backend/internal/elasticsearch"
	"skeleton-internship-backend/internal/model"
	"sync"
	"time"

	"skeleton-internship-backend/internal/kafka"

	"github.com/rs/zerolog/log"
	kafkaGo "github.com/segmentio/kafka-go"
)

type LogConsumerService interface {
	Run(ctx context.Context, wg *sync.WaitGroup)
}

type logConsumerService struct {
	consumer    kafka.LogConsumer
	logStore    elasticsearch.LogStore
	batchSize   int           // How many Kafka messages to process at once
	maxWaitTime time.Duration // Max time to wait for batchSize messages
}

func NewLogConsumerService(
	consumer kafka.LogConsumer,
	logStore elasticsearch.LogStore,
	cfg *config.Config,
) LogConsumerService {
	batchSize := cfg.LogProcessor.BatchSize
	maxWaitTime := time.Duration(cfg.LogProcessor.MaxBatchWait) * time.Second

	return &logConsumerService{
		consumer:    consumer,
		logStore:    logStore,
		batchSize:   batchSize,
		maxWaitTime: maxWaitTime,
	}
}

func (s *logConsumerService) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	log.Info().Msg("Starting Log Consumer Service loop...")

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Log Consumer Service loop stopping due to context cancellation.")
			return
		default:
		}

		// Process one batch of messages
		err := s.processBatch(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				log.Info().Msg("Context cancelled during batch processing.")
				return
			}
			log.Error().Err(err).Msg("Error processing consumer batch")
			time.Sleep(1 * time.Second)
		}
	}
}

func (s *logConsumerService) processBatch(ctx context.Context) error {
	logEntries := make([]*model.LogEntry, 0, s.batchSize)
	originalMessages := make([]kafkaGo.Message, 0, s.batchSize)
	batchStartTime := time.Now()
	commitNeeded := false

	for len(logEntries) < s.batchSize {
		select {
		case <-ctx.Done():
			log.Info().Msg("Context cancelled while building consumer batch.")
			return ctx.Err()
		default:
		}

		fetchCtx, cancel := context.WithTimeout(ctx, s.maxWaitTime-time.Since(batchStartTime))

		logEntry, originalMsg, err := s.consumer.FetchMessage(fetchCtx)
		cancel() // Cancel the fetch context

		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				// Waited long enough, process whatever we have collected
				log.Debug().Int("batch_size", len(logEntries)).Msg("Max wait time reached for batch, processing partial batch.")
				break
			}
			// If FetchMessage returns the original message even on unmarshal error, we still need to process it (to commit)
			if originalMsg.Topic != "" { // Check if we got a message back despite error
				originalMessages = append(originalMessages, originalMsg)
				logWarn := log.Warn().Int64("offset", originalMsg.Offset)
				if logEntry == nil {
					logWarn.Msg("Adding message with unmarshal error to batch for commit tracking.")
				}
				// Add nil entry if parsing failed, but track the message
				logEntries = append(logEntries, logEntry) // logEntry might be nil here
				continue                                  // Continue fetching next message
			}

			log.Error().Err(err).Msg("Failed to fetch message, stopping batch accumulation for now.")
			// Decide: return error immediately? Or try processing what we have?
			// Returning error to retry fetch on next cycle.
			return fmt.Errorf("failed to fetch kafka message: %w", err)
		}

		// Successfully fetched and parsed
		logEntries = append(logEntries, logEntry)
		originalMessages = append(originalMessages, originalMsg)
		commitNeeded = true // We got at least one valid message

		// If we hit batch size limit exactly, break loop
		if len(logEntries) >= s.batchSize {
			break
		}
	}

	// --- Process the collected batch ---
	if len(logEntries) == 0 {
		log.Debug().Msg("No messages in batch to process.")
		return nil // Nothing to do
	}

	log.Debug().Int("batch_size", len(logEntries)).Msg("Processing collected batch...")

	// 1. Extract Metrics

	// 2. Store Logs (Elasticsearch) - Filter out nil entries before sending
	validLogEntries := make([]model.LogEntry, 0, len(logEntries))
	for _, entry := range logEntries {
		if entry != nil {
			validLogEntries = append(validLogEntries, *entry)
		}
	}
	errLogStore := s.logStore.StoreLogs(ctx, validLogEntries)
	if errLogStore != nil {
		log.Error().Err(errLogStore).Msg("Failed to store logs to Elasticsearch")
		// Decide strategy: Don't commit? Retry? Dead-letter queue?
		// For now: Log error, DO NOT commit, leads to reprocessing.
		return fmt.Errorf("failed storing logs: %w", errLogStore)
	}

	// 3. Store Metrics (TimescaleDB)

	// 4. Commit to Kafka (only if both stores succeeded)
	if commitNeeded && errLogStore == nil {
		log.Debug().Int("message_count", len(originalMessages)).Msg("Attempting to commit Kafka messages...")
		errCommit := s.consumer.CommitMessages(ctx, originalMessages...)
		if errCommit != nil {
			log.Error().Err(errCommit).Msg("Failed to commit Kafka messages after successful storage")
			// This is tricky. Data is stored, but offset isn't committed. Will reprocess on restart.
			// Potentially implement retry logic for commits.
			return fmt.Errorf("failed committing kafka messages: %w", errCommit)
		}
		log.Info().Int("batch_size", len(logEntries)).Msg("Successfully processed and committed batch.")
	} else if !commitNeeded {
		log.Debug().Msg("No valid messages processed, skipping commit.")
	} else {
		log.Warn().Msg("Skipping Kafka commit due to storage errors.")
		// Return the first error encountered
		if errLogStore != nil {
			return fmt.Errorf("failed storing logs: %w", errLogStore)
		}
	}

	return nil // Batch processed successfully
}
