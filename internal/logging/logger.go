package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Level represents log severity
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// String returns the string representation of a log level
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "unknown"
	}
}

// ParseLevel parses a log level string
func ParseLevel(s string) Level {
	switch s {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// Entry represents a log entry
type Entry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

// Logger handles structured logging
type Logger struct {
	output io.Writer
	level  Level
	mu     sync.Mutex
}

// Config configures the logger
type Config struct {
	Level  string
	Format string // json or text
	Output string // stdout, stderr, or file path
}

// New creates a new logger
func New(cfg Config) (*Logger, error) {
	var output io.Writer

	switch cfg.Output {
	case "", "stdout":
		output = os.Stdout
	case "stderr":
		output = os.Stderr
	default:
		f, err := os.OpenFile(cfg.Output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to open log file: %w", err)
		}
		output = f
	}

	return &Logger{
		output: output,
		level:  ParseLevel(cfg.Level),
	}, nil
}

// Log logs a message at the specified level
func (l *Logger) Log(level Level, msg string, fields map[string]interface{}) {
	if level < l.level {
		return
	}

	entry := Entry{
		Timestamp: time.Now().UTC(),
		Level:     level.String(),
		Message:   msg,
		Fields:    fields,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.output.Write(data)
	l.output.Write([]byte("\n"))
}

// Debug logs a debug message
func (l *Logger) Debug(msg string, fields map[string]interface{}) {
	l.Log(LevelDebug, msg, fields)
}

// Info logs an info message
func (l *Logger) Info(msg string, fields map[string]interface{}) {
	l.Log(LevelInfo, msg, fields)
}

// Warn logs a warning message
func (l *Logger) Warn(msg string, fields map[string]interface{}) {
	l.Log(LevelWarn, msg, fields)
}

// Error logs an error message
func (l *Logger) Error(msg string, fields map[string]interface{}) {
	l.Log(LevelError, msg, fields)
}

// RequestLog represents a request log entry
type RequestLog struct {
	Timestamp  time.Time `json:"timestamp"`
	RequestID  string    `json:"request_id"`
	ProfileID  string    `json:"profile_id"`
	ClientIP   string    `json:"client_ip"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	UserAgent  string    `json:"user_agent"`
	Action     string    `json:"action"`
	Reason     string    `json:"reason"`
	Labels     []string  `json:"labels,omitempty"`
	StatusCode int       `json:"status_code"`
	Duration   float64   `json:"duration_ms"`
	TLSVersion string    `json:"tls_version,omitempty"`
	SNI        string    `json:"sni,omitempty"`
}

// LogRequest logs a request with metadata
func (l *Logger) LogRequest(req RequestLog) {
	if LevelInfo < l.level {
		return
	}

	data, err := json.Marshal(req)
	if err != nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.output.Write(data)
	l.output.Write([]byte("\n"))
}

// Close closes the logger output if it's a file
func (l *Logger) Close() error {
	if closer, ok := l.output.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
