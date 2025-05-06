package timescaledb

import (
	"context"
	"errors"
	"fmt"
	"skeleton-internship-backend/internal/dto"
	"skeleton-internship-backend/internal/repository"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

type timescaleMetricRepository struct {
	pool       *pgxpool.Pool
	eventTable string
}

func NewTimescaleMetricRepository(pool *pgxpool.Pool) (repository.MetricRepository, error) {
	if pool == nil {
		log.Warn().Msg("TimescaleDB pool is nil in NewTimescaleMetricRepository. Returning no-op repository.")
		return nil, errors.New("TimescaleDB connection pool is required for MetricRepository")
	}
	return &timescaleMetricRepository{
		pool:       pool,
		eventTable: metricEventsTableName,
	}, nil
}

func (r *timescaleMetricRepository) GetSummaryMetrics(ctx context.Context, req dto.MetricSummaryRequest) (*dto.MetricSummaryResponse, error) {
	resp := &dto.MetricSummaryResponse{}
	var err error

	whereClauses := []string{"time >= $1", "time < $2"}
	args := []interface{}{req.StartTime, req.EndTime}
	argCounter := 3

	if len(req.Applications) > 0 {
		appPlaceholders := make([]string, len(req.Applications))
		for i, app := range req.Applications {
			appPlaceholders[i] = fmt.Sprintf("$%d", argCounter)
			args = append(args, app)
			argCounter++
		}
		whereClauses = append(whereClauses, fmt.Sprintf("application IN (%s)", strings.Join(appPlaceholders, ",")))
	}
	whereSQL := strings.Join(whereClauses, " AND ")

	logCountSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE metric_name = 'log_event' AND %s", r.eventTable, whereSQL)
	err = r.pool.QueryRow(ctx, logCountSQL, args...).Scan(&resp.TotalLogEvents)
	if err != nil {
		log.Error().Err(err).Str("query", logCountSQL).Msg("Failed to count log events")
	}

	errorCountSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE metric_name = 'error_event' AND %s", r.eventTable, whereSQL)
	err = r.pool.QueryRow(ctx, errorCountSQL, args...).Scan(&resp.TotalErrorEvents)
	if err != nil {
		log.Error().Err(err).Str("query", errorCountSQL).Msg("Failed to count error events")
	}

	if resp.TotalLogEvents == 0 && resp.TotalErrorEvents == 0 && err != nil {
		return nil, fmt.Errorf("failed to get summary metrics: %w", err)
	}

	return resp, nil
}

func (r *timescaleMetricRepository) GetTimeseriesMetrics(ctx context.Context, req dto.MetricTimeseriesRequest) (*dto.MetricTimeseriesResponse, error) {
	allowedGroupBy := map[string]string{
		"level":       "tags->>'level'",
		"component":   "tags->>'component'",
		"error_key":   "tags->>'error_key'",
		"application": "application",
	}
	groupBySQL, ok := allowedGroupBy[req.GroupBy]
	if !ok {
		log.Warn().Str("group_by_requested", req.GroupBy).Msg("Invalid or no groupBy provided, aggregating total.")
		groupBySQL = "'total'"
		req.GroupBy = "total"
	}

	validIntervals := map[string]bool{
		"1 minute": true, "5 minute": true, "10 minute": true,
		"30 minute": true, "1 hour": true, "1 day": true,
	}
	if !validIntervals[req.Interval] {
		return nil, fmt.Errorf("invalid interval: %s", req.Interval)
	}

	whereClauses := []string{"metric_name = $1", "time >= $2", "time < $3"}
	args := []interface{}{req.MetricName, req.StartTime, req.EndTime}
	argCounter := 4

	if len(req.Applications) > 0 {
		appPlaceholders := make([]string, len(req.Applications))
		for i, app := range req.Applications {
			appPlaceholders[i] = fmt.Sprintf("$%d", argCounter)
			args = append(args, app)
			argCounter++
		}
		whereClauses = append(whereClauses, fmt.Sprintf("application IN (%s)", strings.Join(appPlaceholders, ",")))
	}

	if req.GroupBy == "component" {
		whereClauses = append(whereClauses, "tags->>'component' NOT IN ('UNKNOWN', 'ORPHAN')")
	}

	whereSQL := strings.Join(whereClauses, " AND ")

	querySQL := fmt.Sprintf(`
        SELECT
            time_bucket($%d::interval, time) AS bucket,
            %s AS group_key,
            COUNT(*) AS value
        FROM %s
        WHERE %s
        GROUP BY bucket, group_key
        ORDER BY bucket ASC
    `, argCounter, groupBySQL, r.eventTable, whereSQL)
	args = append(args, req.Interval)

	log.Debug().Str("query", querySQL).Interface("args", args).Msg("Executing TimescaleDB timeseries query")

	rows, err := r.pool.Query(ctx, querySQL, args...)
	if err != nil {
		log.Error().Err(err).Msg("Failed to execute timeseries query")
		return nil, fmt.Errorf("timeseries query failed: %w", err)
	}
	defer rows.Close()

	seriesMap := make(map[string][]dto.TimeseriesDataPoint)

	for rows.Next() {
		var bucket time.Time
		var groupKey *string
		var value int64

		if err := rows.Scan(&bucket, &groupKey, &value); err != nil {
			log.Error().Err(err).Msg("Failed to scan timeseries row")
			continue // Bỏ qua dòng lỗi
		}

		key := "total"
		if groupKey != nil {
			key = *groupKey
		} else if req.GroupBy != "total" {
			key = fmt.Sprintf("%s_NULL", req.GroupBy)
		}

		if _, exists := seriesMap[key]; !exists {
			seriesMap[key] = make([]dto.TimeseriesDataPoint, 0)
		}
		seriesMap[key] = append(seriesMap[key], dto.TimeseriesDataPoint{
			Timestamp: bucket.UnixMilli(),
			Value:     value,
		})
	}

	if err := rows.Err(); err != nil {
		log.Error().Err(err).Msg("Error iterating timeseries rows")
		return nil, fmt.Errorf("failed iterating query results: %w", err)
	}

	response := &dto.MetricTimeseriesResponse{
		Series: make([]dto.TimeseriesSeries, 0, len(seriesMap)),
	}
	for name, data := range seriesMap {
		response.Series = append(response.Series, dto.TimeseriesSeries{
			Name: name,
			Data: data,
		})
	}

	return response, nil
}

func (r *timescaleMetricRepository) GetDistinctApplications(ctx context.Context, req dto.ApplicationListRequest) (*dto.ApplicationListResponse, error) {
	querySQL := fmt.Sprintf("SELECT DISTINCT application FROM %s WHERE time >= $1 AND time < $2 ORDER BY application", r.eventTable)
	args := []interface{}{req.StartTime, req.EndTime}

	rows, err := r.pool.Query(ctx, querySQL, args...)
	if err != nil {
		log.Error().Err(err).Msg("Failed to query distinct applications")
		return nil, fmt.Errorf("failed getting applications: %w", err)
	}
	defer rows.Close()

	apps := make([]string, 0)
	for rows.Next() {
		var app string
		if err := rows.Scan(&app); err != nil {
			log.Error().Err(err).Msg("Failed to scan application row")
			continue
		}
		apps = append(apps, app)
	}

	if err := rows.Err(); err != nil {
		log.Error().Err(err).Msg("Error iterating application rows")
		return nil, fmt.Errorf("failed iterating application results: %w", err)
	}

	return &dto.ApplicationListResponse{Applications: apps}, nil
}
