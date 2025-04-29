package parser

import (
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type HeaderInfo struct {
	Timestamp      time.Time
	Level          string
	Component      string
	InitialContent string
}

type LogParser interface {
	ParseHeader(line string) (*HeaderInfo, bool)
}

type multilineCapableParser struct {
	// Groups: 1:Date, 2:Time, 3:Level, 4:Component, 5:InitialContent
	headerRegex *regexp.Regexp
}

func NewMultilineCapableParser() LogParser {
	regex := regexp.MustCompile(`^(\d{2}/\d{2}/\d{2})\s+(\d{2}:\d{2}:\d{2})\s+(\w+)\s+([\w\.\-]+)\s*:\s*(.*)$`)
	return &multilineCapableParser{headerRegex: regex}
}

func (p *multilineCapableParser) ParseHeader(line string) (*HeaderInfo, bool) {
	matches := p.headerRegex.FindStringSubmatch(line)

	if len(matches) != 6 {
		return nil, false
	}

	dateStr := matches[1]
	timeStr := matches[2]
	level := matches[3]
	component := matches[4]
	initialContent := strings.TrimSpace(matches[5])

	const layout = "06/01/02 15:04:05"
	timestamp, err := time.Parse(layout, dateStr+" "+timeStr)
	if err != nil {
		log.Warn().Err(err).Str("datetime_string", dateStr+" "+timeStr).Str("line", line).Msg("Failed to parse timestamp in header, using current time")
		timestamp = time.Now().UTC()
	}

	info := &HeaderInfo{
		Timestamp:      timestamp.UTC(),
		Level:          strings.ToUpper(level),
		Component:      component,
		InitialContent: initialContent,
	}

	return info, true
}

func ExtractApplicationID(filePath string) string {
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
