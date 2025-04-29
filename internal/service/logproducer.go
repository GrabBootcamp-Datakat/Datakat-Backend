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
		return nil // Not an error, just skipping this scheduled run
	}
	defer s.processLock.Unlock()

	log.Info().Msg("Starting log processing cycle...")
	startTime := time.Now()

	currentState, err := s.stateMgr.LoadState()
	if err != nil {
		log.Error().Err(err).Msg("Failed to load initial file state")
		return fmt.Errorf("failed to load file state: %w", err)
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
	var totalLinesSent int64
	var allLogs []model.LogEntry

	for _, filePath := range logFiles {
		processedCount, newOffset, fileLogs, err := s.processSingleFile(ctx, filePath, newState)
		if err != nil {
			log.Error().Err(err).Str("file", filePath).Msg("Failed to process file")
			continue
		}

		if processedCount > 0 {
			log.Debug().Str("file", filePath).Int64("lines_read", processedCount).Msg("Read new lines from file")
			newState[filePath] = newOffset
			totalLinesRead += processedCount
			allLogs = append(allLogs, fileLogs...)

			if len(allLogs) >= s.cfg.BatchSize {
				err := s.sendBatch(ctx, allLogs)
				if err != nil {
					log.Error().Err(err).Msg("Failed to send intermediate batch to Kafka")
				} else {
					totalLinesSent += int64(len(allLogs))
					allLogs = []model.LogEntry{} // Reset batch
				}
			}
		}
	}

	if len(allLogs) > 0 {
		err := s.sendBatch(ctx, allLogs)
		if err != nil {
			log.Error().Err(err).Msg("Failed to send final batch to Kafka")
		} else {
			totalLinesSent += int64(len(allLogs))
		}
	}

	if err := s.stateMgr.SaveState(newState); err != nil {
		log.Error().Err(err).Msg("Failed to save final file state")
		return fmt.Errorf("failed to save final file state: %w", err)
	}

	duration := time.Since(startTime)
	log.Info().
		Int64("lines_read", totalLinesRead).
		Int64("lines_sent", totalLinesSent).
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

func (s *logProducerService) processSingleFile(ctx context.Context, filePath string, state filestate.FileProcessState) (int64, int64, []model.LogEntry, error) {

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
		lastOffset = 0
	}

	_, err = file.Seek(lastOffset, 0)
	if err != nil {
		return 0, lastOffset, nil, fmt.Errorf("failed to seek in file %s: %w", filePath, err)
	}

	scanner := bufio.NewScanner(file)
	var linesRead int64
	var currentOffset = lastOffset
	var parsedLogs []model.LogEntry

	for scanner.Scan() {
		line := scanner.Text()
		linesRead++
		currentOffset += int64(len(line) + 1)
		logEntry, err := s.parser.Parse(line, filePath)
		if err != nil {
			log.Warn().Err(err).Str("file", filePath).Str("line", line).Msg("Failed to parse log line")
		}
		if logEntry != nil {
			parsedLogs = append(parsedLogs, *logEntry)
		}
		lastOffset = currentOffset
	}
	return linesRead, lastOffset, parsedLogs, nil
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
