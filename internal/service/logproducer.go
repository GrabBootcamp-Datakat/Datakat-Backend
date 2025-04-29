package service

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"skeleton-internship-backend/config"
	"skeleton-internship-backend/internal/filestate"
	"skeleton-internship-backend/internal/kafka"
	"skeleton-internship-backend/internal/model"
	"skeleton-internship-backend/internal/parser"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

type LogProducerService interface {
	ProcessLogs(ctx context.Context) error
}

type logProducerService struct {
	parser      parser.LogParser
	producer    kafka.LogProducer
	kafkaCfg    *config.KafkaConfig
	cfg         *config.LogProcessorConfig
	stateMgr    filestate.Manager
	processLock sync.Mutex
}

func NewLogProducerService(
	cfg *config.Config,
	stateMgr filestate.Manager,
	parser parser.LogParser,
	producer kafka.LogProducer,
) LogProducerService {
	return &logProducerService{
		cfg:      &cfg.LogProcessor,
		kafkaCfg: &cfg.Kafka,
		stateMgr: stateMgr,
		parser:   parser,
		producer: producer,
	}
}
func (s *logProducerService) ProcessLogs(ctx context.Context) error {
	if !s.processLock.TryLock() {
		log.Warn().Msg("Log processing already in progress, skipping run.")
		return nil
	}
	defer s.processLock.Unlock()

	log.Info().Msg("Starting log processing cycle...")
	startTime := time.Now()

	currentState, err := s.stateMgr.LoadState()
	if err != nil {
		if !os.IsNotExist(err) {
			log.Error().Err(err).Msg("Failed to load initial file state")
			return fmt.Errorf("failed to load file state: %w", err)
		}
		log.Warn().Str("file", s.stateMgr.GetStateFilePath()).Msg("State file not found, starting fresh.")
		currentState = make(filestate.FileProcessState)
	}

	newState := make(filestate.FileProcessState)
	for k, v := range currentState {
		newState[k] = v
	}
	logFiles, err := s.findLogFiles()
	if err != nil {
		log.Error().Err(err).Msg("Failed to find log files")
		return fmt.Errorf("failed to find log files: %w", err)
	}
	log.Debug().Int("file_count", len(logFiles)).Msg("Found log files to process")
	var totalLinesRead int64
	var totalEntriesSent int64
	var allLogs []model.LogEntry

	for _, filePath := range logFiles {
		processedCount, newOffset, fileLogs, err := s.processSingleFileMultiline(ctx, filePath, newState)
		if err != nil {
			log.Error().Err(err).Str("file", filePath).Msg("Failed to process file")
			newState[filePath] = currentState[filePath]
			continue
		}

		newState[filePath] = newOffset
		if processedCount > 0 {
			log.Debug().Str("file", filePath).Int64("lines_read", processedCount).Int("entries_found", len(fileLogs)).Msg("Processed file")
			totalLinesRead += processedCount
			allLogs = append(allLogs, fileLogs...)

			if len(allLogs) >= s.cfg.BatchSize {
				batchToSend := make([]model.LogEntry, len(allLogs))
				copy(batchToSend, allLogs)
				allLogs = []model.LogEntry{}

				err := s.sendBatch(ctx, batchToSend) // Gửi batch
				if err != nil {
					log.Error().Err(err).Msg("Failed to send intermediate batch to Kafka")
					// Tạm thời log lỗi và bỏ qua batch này (có thể mất log)
				} else {
					totalEntriesSent += int64(len(batchToSend))
				}
			}
		}
	}

	if len(allLogs) > 0 {
		err := s.sendBatch(ctx, allLogs)
		if err != nil {
			log.Error().Err(err).Msg("Failed to send final batch to Kafka")
		} else {
			totalEntriesSent += int64(len(allLogs))
		}
	}

	if err := s.stateMgr.SaveState(newState); err != nil {
		log.Error().Err(err).Msg("Failed to save final file state")
		return fmt.Errorf("failed to save final file state: %w", err)
	}

	duration := time.Since(startTime)
	log.Info().
		Int64("lines_read", totalLinesRead).
		Int64("entries_sent", totalEntriesSent).
		Int("files_processed", len(logFiles)).
		Dur("duration", duration).
		Msg("Finished log processing cycle.")

	return nil
}

func (s *logProducerService) findLogFiles() ([]string, error) {
	var logFiles []string
	appDirs, err := os.ReadDir(s.cfg.LogDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to load log directory: %w", err)
	}
	for _, appDir := range appDirs {
		if appDir.IsDir() && strings.HasPrefix(appDir.Name(), "application") {
			appDirPath := filepath.Join(s.cfg.LogDirectory, appDir.Name())
			containerFiles, err := os.ReadDir(appDirPath)
			if err != nil {
				log.Warn().Err(err).Str("dir", appDirPath).Msg("Failed to read application directory")
				continue
			}
			for _, containerFile := range containerFiles {
				if !containerFile.IsDir() && strings.HasSuffix(containerFile.Name(), ".log") {
					logFiles = append(logFiles, filepath.Join(appDirPath, containerFile.Name()))
				}
			}
		}
	}
	return logFiles, nil
}

func (s *logProducerService) processSingleFileMultiline(ctx context.Context, filePath string, state filestate.FileProcessState) (linesRead int64, newOffset int64, entries []model.LogEntry, err error) {
	lastOffset := state[filePath]

	file, err := os.Open(filePath)
	if err != nil {
		return 0, lastOffset, nil, fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return 0, lastOffset, nil, fmt.Errorf("failed to stat file %s: %w", filePath, err)
	}
	currentSize := info.Size()

	if currentSize < lastOffset {
		log.Warn().Str("file", filePath).Int64("last_offset", lastOffset).Int64("current_size", currentSize).Msg("File truncated or rotated? Resetting offset.")
		lastOffset = 0
	}

	if _, err = file.Seek(lastOffset, 0); err != nil {
		return 0, lastOffset, nil, fmt.Errorf("failed to seek file %s to offset %d: %w", filePath, lastOffset, err)
	}

	scanner := bufio.NewScanner(file)
	currentOffset := lastOffset

	var currentEntry *model.LogEntry  // Entry đang được xây dựng
	var contentBuffer strings.Builder // Buffer cho content đa dòng
	var rawBuffer strings.Builder     // Buffer cho raw log đa dòng

	appID := parser.ExtractApplicationID(filePath)

	// Hàm nội bộ để hoàn thiện và thêm entry vào kết quả
	finalizeEntry := func() {
		if currentEntry != nil {
			currentEntry.Content = contentBuffer.String()
			currentEntry.Raw = rawBuffer.String()
			entries = append(entries, *currentEntry)
			log.Trace().Str("file", filePath).Msg("Finalized log entry")
		}
		currentEntry = nil
		contentBuffer.Reset()
		rawBuffer.Reset()
	}

	for scanner.Scan() {
		line := scanner.Text()
		linesRead++
		lineOffset := int64(len(line)) + 1

		select {
		case <-ctx.Done():
			log.Info().Str("file", filePath).Msg("Context cancelled during multiline file processing.")
			finalizeEntry()
			return linesRead, currentOffset, entries, ctx.Err()
		default:
			// Continue processing
		}

		headerInfo, isHeader := s.parser.ParseHeader(line)

		if isHeader {
			// === Là dòng Header ===
			log.Trace().Str("file", filePath).Msg("Detected header line")
			// 1. Hoàn thiện entry trước đó (nếu có)
			finalizeEntry()

			// 2. Bắt đầu entry mới
			currentEntry = &model.LogEntry{
				Timestamp:   headerInfo.Timestamp,
				Level:       headerInfo.Level,
				Component:   headerInfo.Component,
				Application: appID,
				SourceFile:  filePath,
			}
			// 3. Thêm content và raw của dòng header vào buffer
			contentBuffer.WriteString(headerInfo.InitialContent)
			rawBuffer.WriteString(line)

		} else {
			// === Là dòng Continuation ===
			log.Trace().Str("file", filePath).Msg("Detected continuation line")
			if currentEntry != nil {
				contentBuffer.WriteString("\n")
				contentBuffer.WriteString(line)
				rawBuffer.WriteString("\n")
				rawBuffer.WriteString(line)
			} else {
				log.Warn().Str("file", filePath).Str("line", line).Msg("Orphan continuation line detected")
				orphanEntry := model.LogEntry{
					Timestamp:   time.Now().UTC(),
					Level:       "UNKNOWN",
					Component:   "ORPHAN",
					Content:     line,
					Application: appID,
					SourceFile:  filePath,
					Raw:         line,
				}
				entries = append(entries, orphanEntry)
			}
		}
		currentOffset += lineOffset
	}

	if err := scanner.Err(); err != nil {
		finalizeEntry()
		return linesRead, currentOffset, entries, fmt.Errorf("error reading file %s: %w", filePath, err)
	}

	finalizeEntry()

	log.Debug().Str("file", filePath).Int64("lines_read", linesRead).Int("entries_created", len(entries)).Msg("Finished processing file")
	return linesRead, currentOffset, entries, nil
}

func (s *logProducerService) sendBatch(ctx context.Context, batch []model.LogEntry) error {
	if len(batch) == 0 {
		return nil
	}
	log.Debug().Int("batch_size", len(batch)).Msg("Sending batch to Kafka...")
	err := s.producer.Produce(ctx, batch)
	if err != nil {
		return fmt.Errorf("kafka produce error: %w", err)
	}
	log.Debug().Int("batch_size", len(batch)).Msg("Successfully sent batch to Kafka.")
	return nil
}
