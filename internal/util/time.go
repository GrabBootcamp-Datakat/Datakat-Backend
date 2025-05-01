package util

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/araddon/dateparse"
)

func ParseTimeFlexible(timeStr string) (time.Time, error) {
	// Try parsing as RFC3339 (ISO 8601)
	t, err := time.Parse(time.RFC3339Nano, timeStr)
	if err == nil {
		return t.UTC(), nil // Convert to UTC
	}
	t, err = time.Parse(time.RFC3339, timeStr) // Try without nano
	if err == nil {
		return t.UTC(), nil
	}

	// Try parsing as epoch milliseconds
	ms, err := strconv.ParseInt(timeStr, 10, 64)
	if err == nil {
		return time.UnixMilli(ms).UTC(), nil // Convert to UTC
	}

	return time.Time{}, fmt.Errorf("invalid time format: %s", timeStr)
}

// ParseTimeInput parses flexible time inputs (ISO, epoch ms, relative like "now-1h")
func ParseTimeInput(timeStr string) (time.Time, error) {
	if strings.HasPrefix(timeStr, "now") {
		if timeStr == "now" {
			return time.Now().UTC(), nil
		}
		parts := strings.Split(timeStr, "-")
		if len(parts) == 2 && parts[0] == "now" {
			durationStr := parts[1]
			duration, err := time.ParseDuration(durationStr)
			if err == nil {
				return time.Now().UTC().Add(-duration), nil
			}
		}
	}

	t, err := dateparse.ParseStrict(timeStr)
	if err == nil {
		return t.UTC(), nil
	}

	ms, errMs := strconv.ParseInt(timeStr, 10, 64)
	if errMs == nil {
		return time.UnixMilli(ms).UTC(), nil
	}

	return time.Time{}, fmt.Errorf("invalid time format: %s (%w)", timeStr, err)
}
