package model

import "time"

type LogEntry struct {
	Timestamp   time.Time `json:"@timestamp"`
	Level       string    `json:"level"`
	Component   string    `json:"component"`
	Content     string    `json:"content"`
	Application string    `json:"application"`
	SourceFile  string    `json:"source_file"`
	Raw         string    `json:"raw_log"`
}
