package service

import (
	"context"
	"errors"
	"fmt"
	"skeleton-internship-backend/internal/dto"
	"skeleton-internship-backend/internal/repository"

	"github.com/rs/zerolog/log"
)

type MetricQueryService interface {
	GetSummary(ctx context.Context, req dto.MetricSummaryRequest) (*dto.MetricSummaryResponse, error)
	GetTimeseries(ctx context.Context, req dto.MetricTimeseriesRequest) (*dto.MetricTimeseriesResponse, error)
	GetApplications(ctx context.Context, req dto.ApplicationListRequest) (*dto.ApplicationListResponse, error)
}

type metricQueryService struct {
	metricRepo repository.MetricRepository
}

func NewMetricQueryService(metricRepo repository.MetricRepository) MetricQueryService {
	return &metricQueryService{
		metricRepo: metricRepo,
	}
}

func (s *metricQueryService) GetSummary(ctx context.Context, req dto.MetricSummaryRequest) (*dto.MetricSummaryResponse, error) {
	if req.StartTime.IsZero() || req.EndTime.IsZero() {
		return nil, errors.New("startTime and endTime are required")
	}
	if req.EndTime.Before(req.StartTime) {
		return nil, errors.New("endTime cannot be before startTime")
	}
	log.Info().Time("start", req.StartTime).Time("end", req.EndTime).Strs("apps", req.Applications).Msg("Getting summary metrics")
	return s.metricRepo.GetSummaryMetrics(ctx, req)
}

func (s *metricQueryService) GetTimeseries(ctx context.Context, req dto.MetricTimeseriesRequest) (*dto.MetricTimeseriesResponse, error) {
	// Validate time
	if req.StartTime.IsZero() || req.EndTime.IsZero() {
		return nil, errors.New("startTime and endTime are required")
	}
	if req.EndTime.Before(req.StartTime) {
		return nil, errors.New("endTime cannot be before startTime")
	}

	// Validate metric name
	allowedMetrics := map[string]bool{"log_event": true, "error_event": true}
	if !allowedMetrics[req.MetricName] {
		return nil, fmt.Errorf("invalid metricName: %s", req.MetricName)
	}

	// Validate interval
	allowedIntervals := map[string]bool{
		"1 minute": true, "5 minute": true, "10 minute": true,
		"30 minute": true, "1 hour": true, "1 day": true,
	}
	if !allowedIntervals[req.Interval] {
		return nil, fmt.Errorf("invalid interval: %s", req.Interval)
	}

	// Validate groupBy (nên có danh sách chặt chẽ hơn)
	allowedGroupBy := map[string]bool{
		"level": true, "component": true, "error_key": true, "application": true, "total": true, "": true, // Chấp nhận rỗng hoặc 'total'
	}
	if req.GroupBy == "" {
		req.GroupBy = "total" // Mặc định nếu rỗng
	}
	if !allowedGroupBy[req.GroupBy] {
		return nil, fmt.Errorf("invalid groupBy: %s", req.GroupBy)
	}

	log.Info().
		Time("start", req.StartTime).
		Time("end", req.EndTime).
		Strs("apps", req.Applications).
		Str("metric", req.MetricName).
		Str("interval", req.Interval).
		Str("group_by", req.GroupBy).
		Msg("Getting timeseries metrics")

	return s.metricRepo.GetTimeseriesMetrics(ctx, req)
}

// GetApplications validates input and calls the repository
func (s *metricQueryService) GetApplications(ctx context.Context, req dto.ApplicationListRequest) (*dto.ApplicationListResponse, error) {
	if req.StartTime.IsZero() || req.EndTime.IsZero() {
		return nil, errors.New("startTime and endTime are required")
	}
	if req.EndTime.Before(req.StartTime) {
		return nil, errors.New("endTime cannot be before startTime")
	}
	log.Info().Time("start", req.StartTime).Time("end", req.EndTime).Msg("Getting distinct applications")
	return s.metricRepo.GetDistinctApplications(ctx, req)
}
