package dto

import "time"

type MetricSummaryRequest struct {
	StartTime    time.Time
	EndTime      time.Time
	Applications []string
}

type MetricTimeseriesRequest struct {
	StartTime    time.Time
	EndTime      time.Time
	Applications []string
	MetricName   string // Ví dụ: "log_event", "error_event"
	Interval     string // Ví dụ: "5 minute", "1 hour"
	GroupBy      string // Ví dụ: "level", "component", "error_key", "application"
}

type ApplicationListRequest struct {
	StartTime time.Time
	EndTime   time.Time
}
