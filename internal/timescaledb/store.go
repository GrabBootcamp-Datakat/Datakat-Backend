package timescaledb

import (
	"context"
	"encoding/json"
	"fmt"
	"skeleton-internship-backend/config"
	"skeleton-internship-backend/internal/model"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
	"go.uber.org/fx"
)

type MetricStore interface {
	StoreMetricEvents(ctx context.Context, events []model.MetricEvent) error
	Close()
}
type timescaleMetricStore struct {
	pool      *pgxpool.Pool
	tableName string
}

const (
	metricEventsTableName = "log_metric_events"
	colTime               = "time"
	colMetricName         = "metric_name"
	colApplication        = "application"
	colTags               = "tags" // Kiểu JSONB
)

func ProvideTimescaleDBPool(lc fx.Lifecycle, cfg *config.Config) (MetricStore, *pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.TimescaleDB.DSN)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse TimescaleDB DSN")
		return nil, nil, fmt.Errorf("invalid TimescaleDB DSN: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		log.Error().Err(err).Msg("Unable to create connection pool to TimescaleDB")
		return nil, nil, fmt.Errorf("failed to connect to TimescaleDB: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = pool.Ping(pingCtx)
	if err != nil {
		pool.Close()
		log.Error().Err(err).Msg("Failed to ping TimescaleDB")
		return nil, nil, fmt.Errorf("failed to ping TimescaleDB: %w", err)
	}
	log.Info().Msg("TimescaleDB connection pool created and verified.")

	store := &timescaleMetricStore{
		pool:      pool,
		tableName: metricEventsTableName,
	}

	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelSetup()
	err = store.ensureHypertable(setupCtx)
	if err != nil {
		pool.Close()
		log.Error().Err(err).Msg("Failed to ensure TimescaleDB hypertable exists")
		return nil, nil, fmt.Errorf("failed ensuring hypertable: %w", err)
	}

	lc.Append(fx.Hook{
		OnStop: func(ctx context.Context) error {
			log.Info().Msg("Closing TimescaleDB connection pool...")
			store.Close()
			return nil
		},
	})

	return store, pool, nil
}

func (s *timescaleMetricStore) ensureHypertable(ctx context.Context) error {
	// Tạo bảng nếu chưa tồn tại
	createTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			%s TIMESTAMPTZ NOT NULL,
			%s TEXT NOT NULL,
			%s TEXT NOT NULL,
			%s JSONB
		);`,
		s.tableName, colTime, colMetricName, colApplication, colTags)

	if _, err := s.pool.Exec(ctx, createTableSQL); err != nil {
		return fmt.Errorf("failed to create base table %s: %w", s.tableName, err)
	}
	log.Info().Str("table", s.tableName).Msg("Ensured base table exists.")

	checkHyperSQL := `SELECT EXISTS (
        SELECT 1 FROM timescaledb_information.hypertables WHERE hypertable_name = $1
    );`
	var isHypertable bool
	_ = s.pool.QueryRow(ctx, checkHyperSQL, s.tableName).Scan(&isHypertable)

	if !isHypertable {
		log.Info().Str("table", s.tableName).Msg("Table is not a hypertable, attempting to create...")
		_, err := s.pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS timescaledb;")
		if err != nil {
			log.Warn().Err(err).Msg("Failed to ensure timescaledb extension exists (permission issue?). Trying to proceed...")
		}

		createHyperSQL := fmt.Sprintf(
			"SELECT create_hypertable('%s', '%s', if_not_exists => TRUE, chunk_time_interval => INTERVAL '1 day');",
			s.tableName,
			colTime,
		)
		_, err = s.pool.Exec(ctx, createHyperSQL)
		if err != nil && !strings.Contains(err.Error(), "already a hypertable") {
			return fmt.Errorf("failed to create hypertable %s: %w", s.tableName, err)
		}
		log.Info().Str("table", s.tableName).Msg("Successfully ensured hypertable.")
	} else {
		log.Info().Str("table", s.tableName).Msg("Table is already a hypertable.")
	}

	// Tạo index
	indexSQL := fmt.Sprintf(`
        CREATE INDEX IF NOT EXISTS idx_%s_name_app_time ON %s (metric_name, application, time DESC);
        CREATE INDEX IF NOT EXISTS idx_%s_tags ON %s USING GIN (tags);
    `, s.tableName, s.tableName, s.tableName, s.tableName)
	_, err := s.pool.Exec(ctx, indexSQL)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to create indexes on metrics table (continuing)")
	} else {
		log.Info().Str("table", s.tableName).Msg("Ensured indexes exist on metrics table.")
	}

	return nil
}

// StoreMetricEvents lưu các sự kiện metric
func (s *timescaleMetricStore) StoreMetricEvents(ctx context.Context, events []model.MetricEvent) error {
	if len(events) == 0 {
		return nil
	}

	columns := []string{colTime, colMetricName, colApplication, colTags}

	source := pgx.CopyFromSlice(len(events), func(i int) ([]interface{}, error) {
		e := events[i]
		tagsJSON, err := json.Marshal(e.Tags)
		if err != nil {
			// Log lỗi nhưng vẫn cố gắng insert với tags null để không mất dữ liệu chính
			log.Error().Err(err).Interface("tags", e.Tags).Msg("Failed to marshal metric tags to JSON, inserting null")
			tagsJSON = nil // Sử dụng giá trị null của SQL
		}
		return []interface{}{e.Time, e.MetricName, e.Application, tagsJSON}, nil
	})

	copyCount, err := s.pool.CopyFrom(ctx, pgx.Identifier{s.tableName}, columns, source)
	if err != nil {
		log.Error().Err(err).Msg("Failed to bulk insert metric events into TimescaleDB")
		return fmt.Errorf("timescaledb copyfrom failed: %w", err)
	}

	if int(copyCount) != len(events) {
		log.Warn().Int64("inserted", copyCount).Int("expected", len(events)).Msg("TimescaleDB CopyFrom event count mismatch")
	} else {
		log.Debug().Int64("count", copyCount).Msg("Successfully inserted metric events into TimescaleDB")
	}
	return nil
}

func (s *timescaleMetricStore) Close() {
	s.pool.Close()
}
