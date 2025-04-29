package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"skeleton-internship-backend/config"
	"skeleton-internship-backend/internal/model"
	"sync/atomic"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/elastic/go-elasticsearch"
	"github.com/elastic/go-elasticsearch/v8/esutil"
	"github.com/rs/zerolog/log"
	"go.uber.org/fx"
)

type LogStore interface {
	StoreLogs(ctx context.Context, logs []model.LogEntry) error
	Close(ctx context.Context) error
}
type elasticLogStore struct {
	client          *elasticsearch.Client
	bulkIndexer     esutil.BulkIndexer
	indexPrefix     string
	countSuccessful uint64
	countFailed     uint64
}

func NewElasticLogStore(lc fx.Lifecycle, cfg *config.Config) (LogStore, *elasticsearch.Client, error) {
	if len(cfg.Elasticsearch.Addresses) == 0 {
		log.Error().Msg("Elasticsearch addresses are not configured.")
		return nil, nil, errors.New("elasticsearch configuration missing")
	}
	transport := &http.Transport{
		MaxIdleConnsPerHost:   10,                                                  // Giữ tối đa 10 idle connections cho mỗi host
		ResponseHeaderTimeout: time.Second * 10,                                    // Timeout đọc header response
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext, // Timeout kết nối TCP
		TLSHandshakeTimeout:   5 * time.Second,                                     // Timeout TLS handshake
	}
	esCfg := elasticsearch.Config{
		Addresses: cfg.Elasticsearch.Addresses,
		Transport: transport,
	}

	var esClient *elasticsearch.Client
	var err error
	operation := func() error {
		esClient, err = elasticsearch.NewClient(esCfg)
		if err != nil {
			log.Warn().Err(err).Msg("Attempt failed: Error creating the Elasticsearch client")
			return err
		}

		// Verify connection (ping)
		res, errPing := esClient.Info(
			esClient.Info.WithContext(context.Background()),
		)
		if errPing != nil {
			log.Warn().Err(errPing).Msg("Attempt failed: Error during Elasticsearch Info() call (transport level)")
			return errPing
		}
		defer res.Body.Close()
		if res.IsError() {
			errMsg := fmt.Errorf("elasticsearch Info() returned error status: %s", res.Status())
			log.Warn().Err(errMsg).Msg("Attempt failed: Elasticsearch ping returned error status")
			return errMsg
		}
		log.Info().Str("server_info", res.String()).Msg("Elasticsearch client initialized and connection verified!")
		return nil
	}

	connectBackoff := backoff.NewExponentialBackOff()
	connectBackoff.InitialInterval = 2 * time.Second
	connectBackoff.MaxInterval = 15 * time.Second
	connectBackoff.MaxElapsedTime = 90 * time.Second

	log.Info().Msg("Attempting to connect to Elasticsearch with retries...")
	err = backoff.Retry(operation, connectBackoff)

	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to Elasticsearch after multiple retries")
		return nil, nil, err
	}
	// Create BulkIndexer
	store := &elasticLogStore{
		client:      esClient,
		indexPrefix: cfg.Elasticsearch.LogIndex,
	}

	bi, err := esutil.NewBulkIndexer(esutil.BulkIndexerConfig{
		Client:        esClient,
		Index:         store.getIndexName(),            // Default index, can be overridden per item
		NumWorkers:    cfg.Elasticsearch.BulkWorkers,   // Number of workers
		FlushBytes:    cfg.Elasticsearch.FlushBytes,    // Flush threshold
		FlushInterval: cfg.Elasticsearch.FlushInterval, // Flush interval
		OnError: func(ctx context.Context, err error) {
			log.Error().Err(err).Msg("BulkIndexer error")
			// TODO: error handling/retry logic here
		},
		OnFlushStart: func(ctx context.Context) context.Context {
			log.Debug().Msg("BulkIndexer flush starting")
			return ctx
		},
		OnFlushEnd: func(ctx context.Context) {
			log.Debug().Msg("BulkIndexer flush ended")
		},
	})
	if err != nil {
		log.Fatal().Err(err).Msg("Error creating the BulkIndexer")
		return nil, nil, err
	}
	store.bulkIndexer = bi
	log.Info().Msg("Elasticsearch BulkIndexer initialized")

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			log.Info().Msg("Closing Elasticsearch BulkIndexer...")
			return store.Close(ctx)
		},
	})

	return store, esClient, nil
}

// StoreLogs adds multiple log entries to the bulk indexer.
func (s *elasticLogStore) StoreLogs(ctx context.Context, logs []model.LogEntry) error {
	if len(logs) == 0 {
		return nil
	}

	currentFailed := atomic.LoadUint64(&s.countFailed)

	for _, logEntry := range logs {
		data, err := json.Marshal(logEntry)
		if err != nil {
			log.Error().Err(err).Msg("Failed to marshal log entry for Elasticsearch")
			atomic.AddUint64(&s.countFailed, 1)
			continue
		}

		err = s.bulkIndexer.Add(
			ctx,
			esutil.BulkIndexerItem{
				Action: "index",
				Index:  s.getIndexName(),
				Body:   bytes.NewReader(data),
			},
		)
		if err != nil {
			log.Error().Err(err).Msg("Failed to add item to BulkIndexer")
			atomic.AddUint64(&s.countFailed, 1)
		}
	}
	log.Debug().Int("count", len(logs)).Msg("Added log entries to Elasticsearch BulkIndexer queue")

	if atomic.LoadUint64(&s.countFailed) > currentFailed {
		return errors.New("one or more logs failed during bulk indexing attempt")
	}

	return nil
}
func (s *elasticLogStore) Close(ctx context.Context) error {
	log.Info().Msg("Attempting to close BulkIndexer...")
	err := s.bulkIndexer.Close(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Error closing BulkIndexer")
	} else {
		log.Info().Msg("BulkIndexer closed.")
	}

	stats := s.bulkIndexer.Stats()
	log.Info().
		Uint64("indexed", stats.NumIndexed).
		Uint64("added", stats.NumAdded).
		Uint64("flushed", stats.NumFlushed).
		Uint64("failed", stats.NumFailed).
		Uint64("requests", stats.NumRequests).
		Msg("Elasticsearch BulkIndexer final stats")

	log.Info().
		Uint64("callback_successful", atomic.LoadUint64(&s.countSuccessful)).
		Uint64("callback_failed", atomic.LoadUint64(&s.countFailed)).
		Msg("Elasticsearch BulkIndexer final callback stats")

	return err
}

// getIndexName generates the index name, e.g., "applogs-YYYY-MM-DD"
func (s *elasticLogStore) getIndexName() string {
	return fmt.Sprintf("%s-%s", s.indexPrefix, time.Now().UTC().Format("2006-01-02"))
}
