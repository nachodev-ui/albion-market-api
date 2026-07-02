package observability

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Field is an ordered key/value pair written by Logger.
type Field struct {
	Key   string
	Value any
}

func F(key string, value any) Field {
	return Field{Key: key, Value: value}
}

type Logger struct {
	writer io.Writer
	color  bool
	format string
	mu     sync.Mutex
	now    func() time.Time
}

func NewLogger(writer io.Writer, colorMode string) *Logger {
	return NewLoggerWithFormat(writer, colorMode, "text")
}

func NewLoggerWithFormat(writer io.Writer, colorMode, format string) *Logger {
	if writer == nil {
		writer = io.Discard
	}
	normalizedFormat := strings.ToLower(strings.TrimSpace(format))
	if normalizedFormat != "json" {
		normalizedFormat = "text"
	}
	return &Logger{
		writer: writer,
		color:  normalizedFormat == "text" && shouldUseColor(writer, colorMode),
		format: normalizedFormat,
		now:    time.Now,
	}
}

func (l *Logger) Info(event string, fields ...Field) {
	l.write("INFO", "\x1b[36m", event, fields...)
}

func (l *Logger) Success(event string, fields ...Field) {
	l.write("OK", "\x1b[32m", event, fields...)
}

func (l *Logger) Duplicate(event string, fields ...Field) {
	l.write("DUP", "\x1b[36m", event, fields...)
}

func (l *Logger) Warn(event string, fields ...Field) {
	l.write("WARN", "\x1b[33m", event, fields...)
}

func (l *Logger) Error(event string, fields ...Field) {
	l.write("ERROR", "\x1b[31m", event, fields...)
}

func (l *Logger) write(label, color, event string, fields ...Field) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.format == "json" {
		record := make(map[string]any, len(fields)+3)
		record["timestamp"] = l.now().UTC().Format(time.RFC3339Nano)
		record["level"] = label
		record["event"] = event
		for _, field := range fields {
			key := strings.TrimSpace(field.Key)
			if key == "" || key == "timestamp" || key == "level" || key == "event" {
				continue
			}
			if isSensitiveLogKey(key) {
				record[key] = "[REDACTED]"
				continue
			}
			record[key] = normalizeJSONLogValue(field.Value)
		}
		encoded, err := json.Marshal(record)
		if err == nil {
			_, _ = l.writer.Write(append(encoded, '\n'))
		}
		return
	}

	timestamp := l.now().UTC().Format(time.RFC3339Nano)
	visibleLabel := fmt.Sprintf("%-5s", label)
	if l.color {
		visibleLabel = color + visibleLabel + "\x1b[0m"
	}

	_, _ = fmt.Fprintf(l.writer, "%s [%s] %s", timestamp, visibleLabel, event)
	for _, field := range fields {
		if strings.TrimSpace(field.Key) == "" {
			continue
		}
		_, _ = fmt.Fprintf(l.writer, " %s=%s", field.Key, formatLogField(field.Key, field.Value))
	}
	_, _ = io.WriteString(l.writer, "\n")
}

func normalizeJSONLogValue(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case error:
		return typed.Error()
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	case time.Duration:
		return typed.String()
	default:
		return value
	}
}

func formatLogField(key string, value any) string {
	if isSensitiveLogKey(key) {
		return `"[REDACTED]"`
	}
	return formatLogValue(value)
}

func isSensitiveLogKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	switch normalized {
	case "authorization", "proxy_authorization", "database_url", "dsn", "connection_string", "password", "secret", "token":
		return true
	}
	return strings.HasSuffix(normalized, "_token") ||
		strings.HasSuffix(normalized, "_secret") ||
		strings.HasSuffix(normalized, "_password")
}

func formatLogValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case string:
		if typed == "" {
			return `""`
		}
		encoded, _ := json.Marshal(typed)
		return string(encoded)
	case error:
		encoded, _ := json.Marshal(typed.Error())
		return string(encoded)
	case time.Time:
		return typed.UTC().Format(time.RFC3339Nano)
	case time.Duration:
		return typed.String()
	default:
		return fmt.Sprint(value)
	}
}

func shouldUseColor(writer io.Writer, mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "always", "on", "true", "1":
		return true
	case "never", "off", "false", "0":
		return false
	}

	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		return false
	}
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	return prepareColorOutput(file)
}
