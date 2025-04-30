package metrics

import (
	"regexp"
	"skeleton-internship-backend/internal/model"
	"sync"

	"github.com/rs/zerolog/log"
)

type Extractor interface {
	ExtractMetricEvents(logEntry *model.LogEntry) []model.MetricEvent
}

type sparkLogExtractor struct {
	mu             sync.RWMutex
	exceptionRegex *regexp.Regexp
}

func NewSparkLogExtractor() Extractor {
	return &sparkLogExtractor{
		exceptionRegex: regexp.MustCompile(`(?i)(exception|error|fail|caused by)`),
	}
}

func (e *sparkLogExtractor) ExtractMetricEvents(logEntry *model.LogEntry) []model.MetricEvent {
	if logEntry == nil {
		return nil
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	events := make([]model.MetricEvent, 0, 2)

	// "log_event" cho mọi log để đếm theo Level và Component
	app := logEntry.Application
	ts := logEntry.Timestamp

	logEventTags := map[string]string{
		"level":     logEntry.Level,
		"component": logEntry.Component,
	}
	if logEntry.Level == "UNKNOWN" || logEntry.Component == "UNKNOWN" || logEntry.Component == "ORPHAN" {
		logEventTags["parse_status"] = "failed_or_orphan"
	}

	events = append(events, model.MetricEvent{
		Time:        ts,
		MetricName:  "log_event",
		Application: app,
		Tags:        logEventTags,
	})

	isError := logEntry.Level == "ERROR"

	if !isError && e.exceptionRegex.MatchString(logEntry.Content) {
		isError = true
	}

	if isError {
		events = append(events, model.MetricEvent{
			Time:        ts,
			MetricName:  "error_event",
			Application: app,
			Tags: map[string]string{
				"component": logEntry.Component,
				"level":     logEntry.Level,
				"error_key": logEntry.Content,
			},
		})
	}
	if len(events) > 0 {
		log.Trace().Str("application", app).Str("log_timestamp", ts.String()).Int("event_count", len(events)).Msg("Extracted metric events")
	}
	return events
}
