package repository

import (
	"context"
	"skeleton-internship-backend/internal/dto"
)

type MetricRepository interface {
	GetSummaryMetrics(ctx context.Context, req dto.MetricSummaryRequest) (*dto.MetricSummaryResponse, error)
	GetTimeseriesMetrics(ctx context.Context, req dto.MetricTimeseriesRequest) (*dto.MetricTimeseriesResponse, error)
	GetDistinctApplications(ctx context.Context, req dto.ApplicationListRequest) (*dto.ApplicationListResponse, error)
}
