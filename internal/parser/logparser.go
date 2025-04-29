package parser

import (
	"fmt"
	"path/filepath"
	"regexp"
	"skeleton-internship-backend/internal/model"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type LogParser interface {
	Parse(line string, filePath string) (*model.LogEntry, error)
}

type simpleLogParser struct {
	logRegex *regexp.Regexp
}

func NewSimpleLogParser() LogParser {
	// Groups: 1:Date, 2:Time, 3:Level, 4:Component, 5:Content
	regex := regexp.MustCompile(`^(\d{2}/\d{2}/\d{2})\s+(\d{2}:\d{2}:\d{2})\s+(\w+)\s+([\w\.\-]+)\s*:\s*(.*)$`)
	return &simpleLogParser{logRegex: regex}
}

func (p *simpleLogParser) Parse(line string, sourceFilePath string) (*model.LogEntry, error) {
	matches := p.logRegex.FindStringSubmatch(line)

	if len(matches) != 6 {
		log.Debug().Str("line", line).Msg("Log line did not match expected format")
		return nil, fmt.Errorf("line does not match expected format: %s", line)
	}

	dateStr := matches[1]
	timeStr := matches[2]
	level := matches[3]
	component := matches[4]
	content := strings.TrimSpace(matches[5])

	// Assuming YY/MM/DD HH:MM:SS format. Adjust the layout string if your year is YYYY.
	// Note: "06" is for year, "01" for month, "02" for day.
	const layout = "06/01/02 15:04:05" // Adjust if year is 4 digits: "2006/01/02 ..."
	timestamp, err := time.Parse(layout, dateStr+" "+timeStr)
	if err != nil {
		log.Error().Err(err).Str("datetime_string", dateStr+" "+timeStr).Msg("Failed to parse log timestamp")
		return nil, fmt.Errorf("failed to parse timestamp: %w", err)
	}

	appID := extractApplicationID(sourceFilePath)

	entry := &model.LogEntry{
		Timestamp:   timestamp.UTC(), // Store in UTC
		Level:       strings.ToUpper(level),
		Component:   component,
		Content:     content,
		Application: appID,
		SourceFile:  sourceFilePath,
		Raw:         line,
	}

	return entry, nil
}

func extractApplicationID(filePath string) string {
	// Get the directory containing the file
	dir := filepath.Dir(filePath)
	// Get the base name of the directory (e.g., application_12345_0001)
	baseDir := filepath.Base(dir)
	if strings.HasPrefix(baseDir, "application_") {
		return baseDir
	}
	// Fallback if the structure is different
	return "unknown_application"
}
