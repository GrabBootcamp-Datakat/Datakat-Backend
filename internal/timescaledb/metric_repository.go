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
	groupByTag := req.GroupBy
	groupBySQL, ok := allowedGroupBy[req.GroupBy]
	isGroupByTotal := false
	if !ok {
		groupBySQL = "'total'"
		isGroupByTotal = true
	}

	validIntervals := map[string]bool{"1 minute": true, "5 minute": true, "10 minute": true, "30 minute": true, "1 hour": true, "1 day": true}
	if !validIntervals[req.Interval] {
		return nil, fmt.Errorf("invalid interval: %s", req.Interval)
	}

	var queryBuilder strings.Builder
	args := []interface{}{}
	argCounter := 1

	queryBuilder.WriteString(fmt.Sprintf("SELECT time_bucket($%d::interval, time) AS bucket, ", argCounter))
	args = append(args, req.Interval)
	argCounter++

	if isGroupByTotal {
		queryBuilder.WriteString("'total' AS group_key, ")
	} else {
		queryBuilder.WriteString(fmt.Sprintf("%s AS group_key, ", groupBySQL))
	}
	queryBuilder.WriteString(fmt.Sprintf("COUNT(*) AS value FROM %s WHERE metric_name = $%d AND time >= $%d AND time < $%d ", r.eventTable, argCounter, argCounter+1, argCounter+2))
	args = append(args, req.MetricName, req.StartTime, req.EndTime)
	argCounter += 3

	if len(req.Applications) > 0 {
		appPlaceholders := make([]string, len(req.Applications))
		for i, app := range req.Applications {
			appPlaceholders[i] = fmt.Sprintf("$%d", argCounter)
			args = append(args, app)
			argCounter++
		}
		queryBuilder.WriteString(fmt.Sprintf("AND application IN (%s) ", strings.Join(appPlaceholders, ",")))
	}

	if groupByTag == "component" {
		queryBuilder.WriteString("AND tags->>'component' NOT IN ('UNKNOWN', 'ORPHAN') ")
	}

	queryBuilder.WriteString("GROUP BY bucket")
	if !isGroupByTotal {
		queryBuilder.WriteString(", group_key ")
	}

	orderByClause := "ORDER BY bucket ASC"
	var limitClause string

	if req.Sort != nil {
		sortField := req.Sort.Field
		if sortField == "value" {
			sortField = "value"
		} else if sortField == "time" || sortField == "@timestamp" {
			sortField = "bucket"
		} else if _, isTag := allowedGroupBy[sortField]; isTag {
			sortField = "group_key"
		} else {
			log.Warn().Str("sort_field", req.Sort.Field).Msg("Unsupported sort field requested, defaulting to time bucket.")
			sortField = "bucket"
		}

		sortOrder := "ASC"
		if strings.ToLower(req.Sort.Order) == "desc" {
			sortOrder = "DESC"
		}
		orderByClause = fmt.Sprintf("ORDER BY %s %s, bucket ASC", sortField, sortOrder)
	}

	if req.Limit != nil && *req.Limit > 0 {
		limitClause = fmt.Sprintf(" LIMIT $%d", argCounter)
		args = append(args, *req.Limit)
		argCounter++
	}

	queryBuilder.WriteString(" ")
	queryBuilder.WriteString(orderByClause)
	if limitClause != "" {
		queryBuilder.WriteString(" ")
		queryBuilder.WriteString(limitClause)
	}

	querySQL := queryBuilder.String()

	log.Debug().Str("query", querySQL).Interface("args", args).Msg("Executing TimescaleDB timeseries query with sort/limit")

	rows, err := r.pool.Query(ctx, querySQL, args...)
	if err != nil {
		log.Error().Err(err).Str("query", querySQL).Interface("args", args).Msg("Failed to execute timeseries query with sort/limit")
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
			continue
		}

		var key string
		if isGroupByTotal {
			key = "total"
			if groupKey == nil || (groupKey != nil && *groupKey != "total") {
				log.Warn().Str("groupByTag", groupByTag).Bool("isGroupByTotal", isGroupByTotal).
					Interface("scanned_groupKey", groupKey).Msg("Unexpected groupKey value when isGroupByTotal is true; using 'total'")
			}
		} else {
			if groupKey != nil {
				key = *groupKey
			} else {
				key = fmt.Sprintf("%s_NULL", groupByTag)
			}
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

func (r *timescaleMetricRepository) GetDistributionMetrics(ctx context.Context, req dto.MetricDistributionRequest) (*dto.MetricDistributionResponse, error) {
	tagColumnSQL, ok := map[string]string{
		"level":       "tags->>'level'",
		"component":   "tags->>'component'",
		"error_key":   "tags->>'error_key'",
		"application": "application",
	}[req.Dimension]

	if !ok {
		return nil, fmt.Errorf("unsupported dimension for distribution: %s", req.Dimension)
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

	if req.Dimension == "component" {
		whereClauses = append(whereClauses, "tags->>'component' NOT IN ('UNKNOWN', 'ORPHAN')")
	}
	whereClauses = append(whereClauses, fmt.Sprintf("%s IS NOT NULL", tagColumnSQL))

	whereSQL := strings.Join(whereClauses, " AND ")

	querySQL := fmt.Sprintf(`
        SELECT
            %s AS dimension_key,
            COUNT(*) AS value
        FROM %s
        WHERE %s
        GROUP BY dimension_key
        ORDER BY value DESC
    `, tagColumnSQL, r.eventTable, whereSQL)

	log.Debug().Str("query", querySQL).Interface("args", args).Msg("Executing TimescaleDB distribution query")

	rows, err := r.pool.Query(ctx, querySQL, args...)
	if err != nil {
		log.Error().Err(err).Msg("Failed to execute distribution query")
		return nil, fmt.Errorf("distribution query failed: %w", err)
	}
	defer rows.Close()

	distribution := make([]dto.DistributionDataPoint, 0)
	for rows.Next() {
		var key *string
		var value int64
		if err := rows.Scan(&key, &value); err != nil {
			log.Error().Err(err).Msg("Failed to scan distribution row")
			continue
		}
		if key != nil {
			distribution = append(distribution, dto.DistributionDataPoint{
				Name:  *key,
				Value: value,
			})
		}
	}

	if err := rows.Err(); err != nil {
		log.Error().Err(err).Msg("Error iterating distribution rows")
		return nil, fmt.Errorf("failed iterating distribution results: %w", err)
	}

	return &dto.MetricDistributionResponse{
		MetricName:   req.MetricName,
		Dimension:    req.Dimension,
		Distribution: distribution,
	}, nil
}
