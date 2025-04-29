package parser_test

// import (
// 	"os"
// 	"path/filepath"
// 	"strings"
// 	"testing"
// 	"time"

// 	"github.com/stretchr/testify/assert"
// 	"github.com/stretchr/testify/require"

// 	"skeleton-internship-backend/internal/model"
// 	"skeleton-internship-backend/internal/parser"
// )

// func TestSimpleLogParser_Parse(t *testing.T) {
// 	logParser := parser.NewSimpleLogParser()

// 	tests := []struct {
// 		name        string
// 		logLine     string
// 		filePath    string
// 		expected    *model.LogEntry
// 		expectError bool
// 	}{
// 		{
// 			name:        "Valid Log Entry",
// 			logLine:     "22/01/24 14:30:45 INFO logger.component: This is a log message",
// 			filePath:    "/tmp/application_12345_0001/container.log",
// 			expectError: false,
// 			expected: &model.LogEntry{
// 				Timestamp:   mustParseTime(t, "22/01/24 14:30:45"),
// 				Level:       "INFO",
// 				Component:   "logger.component",
// 				Content:     "This is a log message",
// 				Application: "application_12345_0001",
// 				SourceFile:  "/tmp/application_12345_0001/container.log",
// 				Raw:         "22/01/24 14:30:45 INFO logger.component: This is a log message",
// 			},
// 		},
// 		{
// 			name:        "Valid Log Entry With Extra Spaces",
// 			logLine:     "22/01/24 14:30:45 WARN hadoop.utils:   Multiple spaces before content",
// 			filePath:    "/tmp/application_12345_0001/container.log",
// 			expectError: false,
// 			expected: &model.LogEntry{
// 				Timestamp:   mustParseTime(t, "22/01/24 14:30:45"),
// 				Level:       "WARN",
// 				Component:   "hadoop.utils",
// 				Content:     "Multiple spaces before content",
// 				Application: "application_12345_0001",
// 				SourceFile:  "/tmp/application_12345_0001/container.log",
// 				Raw:         "22/01/24 14:30:45 WARN hadoop.utils:   Multiple spaces before content",
// 			},
// 		},
// 		{
// 			name:        "Valid Log Entry With Hyphenated Component",
// 			logLine:     "22/01/24 14:30:45 ERROR app-name: Error occurred",
// 			filePath:    "/temp/application_98765_0002/container.log",
// 			expectError: false,
// 			expected: &model.LogEntry{
// 				Timestamp:   mustParseTime(t, "22/01/24 14:30:45"),
// 				Level:       "ERROR",
// 				Component:   "app-name",
// 				Content:     "Error occurred",
// 				Application: "application_98765_0002",
// 				SourceFile:  "/temp/application_98765_0002/container.log",
// 				Raw:         "22/01/24 14:30:45 ERROR app-name: Error occurred",
// 			},
// 		},
// 		{
// 			name:        "Log Entry With Unknown Application",
// 			logLine:     "22/01/24 14:30:45 DEBUG component: Debug message",
// 			filePath:    "/var/log/other/system.log",
// 			expectError: false,
// 			expected: &model.LogEntry{
// 				Timestamp:   mustParseTime(t, "22/01/24 14:30:45"),
// 				Level:       "DEBUG",
// 				Component:   "component",
// 				Content:     "Debug message",
// 				Application: "unknown_application",
// 				SourceFile:  "/var/log/other/system.log",
// 				Raw:         "22/01/24 14:30:45 DEBUG component: Debug message",
// 			},
// 		},
// 		{
// 			name:        "Invalid Format - Missing Colon",
// 			logLine:     "22/01/24 14:30:45 INFO component Debug message without colon",
// 			filePath:    "/tmp/app/log.txt",
// 			expectError: true,
// 			expected:    nil,
// 		},
// 		{
// 			name:        "Invalid Format - Incorrect Date",
// 			logLine:     "2022/01/24 14:30:45 INFO component: Wrong date format",
// 			filePath:    "/tmp/app/log.txt",
// 			expectError: true,
// 			expected:    nil,
// 		},
// 		{
// 			name:        "Invalid Format - Empty Line",
// 			logLine:     "",
// 			filePath:    "/tmp/app/log.txt",
// 			expectError: true,
// 			expected:    nil,
// 		},
// 		{
// 			name:        "Invalid Format - Partial Log",
// 			logLine:     "22/01/24 14:30:45 INFO",
// 			filePath:    "/tmp/app/log.txt",
// 			expectError: true,
// 			expected:    nil,
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			result, err := logParser.Parse(tt.logLine, tt.filePath)

// 			if tt.expectError {
// 				assert.Error(t, err)
// 				assert.Nil(t, result)
// 			} else {
// 				require.NoError(t, err)
// 				require.NotNil(t, result)

// 				// Compare time separately since direct equality check may fail due to formatting
// 				assert.Equal(t, tt.expected.Timestamp.Unix(), result.Timestamp.Unix())

// 				// Check other fields
// 				assert.Equal(t, tt.expected.Level, result.Level)
// 				assert.Equal(t, tt.expected.Component, result.Component)
// 				assert.Equal(t, tt.expected.Content, result.Content)
// 				assert.Equal(t, tt.expected.Application, result.Application)
// 				assert.Equal(t, tt.expected.SourceFile, result.SourceFile)
// 				assert.Equal(t, tt.expected.Raw, result.Raw)
// 			}
// 		})
// 	}
// }

// func TestExtractApplicationID(t *testing.T) {
// 	tests := []struct {
// 		name     string
// 		filePath string
// 		expected string
// 		setup    func() (string, func())
// 	}{
// 		{
// 			name:     "Standard Application Path",
// 			filePath: "/tmp/application_12345_0001/container.log",
// 			expected: "application_12345_0001",
// 		},
// 		{
// 			name:     "Non-Standard Path",
// 			filePath: "/var/log/random/file.log",
// 			expected: "unknown_application",
// 		},
// 		{
// 			name:     "Real File Path",
// 			setup:    createTempAppDir,
// 			expected: "application_54321", // Only checking prefix
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			path := tt.filePath
// 			var cleanup func()

// 			// If we need to create a real directory structure
// 			if tt.setup != nil {
// 				path, cleanup = tt.setup()
// 				defer cleanup()
// 				require.NotEmpty(t, path)
// 			}

// 			// We can't directly test extractApplicationID as it's not exported
// 			// So we'll use the Parse method which calls it internally
// 			logParser := parser.NewSimpleLogParser()
// 			result, err := logParser.Parse("22/01/24 14:30:45 INFO test: Test message", path)

// 			require.NoError(t, err)

// 			if tt.name == "Real File Path" {
// 				// For real file path test, only check that it starts with the expected prefix
// 				// since os.MkdirTemp adds random characters
// 				assert.True(t, strings.HasPrefix(result.Application, tt.expected),
// 					"Expected application ID to start with %s, got %s", tt.expected, result.Application)
// 			} else {
// 				assert.Equal(t, tt.expected, result.Application)
// 			}
// 		})
// 	}
// }

// // Helper function to create a temporary application directory and log file
// func createTempAppDir() (string, func()) {
// 	// Create a temporary directory with application_ID format
// 	// os.MkdirTemp will append random characters to the pattern
// 	tempDir, err := os.MkdirTemp("", "application_54321")
// 	if err != nil {
// 		return "", func() {}
// 	}

// 	// Create a log file in the directory
// 	logFile := filepath.Join(tempDir, "container.log")
// 	file, err := os.Create(logFile)
// 	if err != nil {
// 		os.RemoveAll(tempDir)
// 		return "", func() {}
// 	}
// 	file.Close()

// 	return logFile, func() {
// 		os.RemoveAll(tempDir)
// 	}
// }

// // Helper function to parse time in the expected format
// func mustParseTime(t *testing.T, timeStr string) time.Time {
// 	parsed, err := time.Parse("06/01/02 15:04:05", timeStr)
// 	require.NoError(t, err, "Failed to parse test time string")
// 	return parsed.UTC()
// }
