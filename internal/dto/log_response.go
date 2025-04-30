package dto

import (
	"skeleton-internship-backend/internal/model"
	"time"
)

type LogSearchRequest struct {
	StartTime    time.Time
	EndTime      time.Time
	Query        string
	Levels       []string
	Applications []string
	SortBy       string
	SortOrder    string
	Page         int
	Size         int
}

type LogSearchResponse struct {
	Logs       []model.LogEntry `json:"logs"`
	TotalCount int64            `json:"totalCount"`
	Page       int              `json:"page"`
	Size       int              `json:"size"`
}
