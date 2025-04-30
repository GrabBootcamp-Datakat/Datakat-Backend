package util

import (
	"fmt"
	"strconv"
	"time"
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
