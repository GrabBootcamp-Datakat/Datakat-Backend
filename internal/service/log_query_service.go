package service

import (
	"context"
	"errors"
	"skeleton-internship-backend/internal/dto"
	"skeleton-internship-backend/internal/repository"
	"strings"

	"github.com/rs/zerolog/log"
)

type LogQueryService interface {
	SearchLogs(ctx context.Context, req dto.LogSearchRequest) (*dto.LogSearchResponse, error)
}

type logQueryService struct {
	logRepo repository.LogRepository
}

func NewLogQueryService(logRepo repository.LogRepository) LogQueryService {
	return &logQueryService{
		logRepo: logRepo,
	}
}
func (s *logQueryService) SearchLogs(ctx context.Context, req dto.LogSearchRequest) (*dto.LogSearchResponse, error) {
	if req.StartTime.IsZero() || req.EndTime.IsZero() {
		return nil, errors.New("startTime and endTime are required")
	}
	if req.EndTime.Before(req.StartTime) {
		return nil, errors.New("endTime cannot be before startTime")
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Size <= 0 || req.Size > 1000 {
		req.Size = 500
	}
	if req.SortBy == "" {
		req.SortBy = "@timestamp"
	}
	req.SortOrder = strings.ToLower(req.SortOrder)
	if req.SortOrder != "asc" && req.SortOrder != "desc" {
		req.SortOrder = "desc"
	}

	for i, level := range req.Levels {
		req.Levels[i] = strings.ToUpper(level)
	}

	log.Info().
		Time("start_time", req.StartTime).
		Time("end_time", req.EndTime).
		Str("query", req.Query).
		Strs("levels", req.Levels).
		Strs("applications", req.Applications).
		Int("page", req.Page).
		Int("size", req.Size).
		Msg("Searching logs")

	return s.logRepo.Search(ctx, req)
}
