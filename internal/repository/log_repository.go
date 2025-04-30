package repository

import (
	"context"
	"skeleton-internship-backend/internal/dto"
)

type LogRepository interface {
	Search(ctx context.Context, req dto.LogSearchRequest) (*dto.LogSearchResponse, error)
}
