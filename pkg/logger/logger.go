package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	// DefaultMaxBuffer is the maximum number of recent log lines retained in memory.
	DefaultMaxBuffer = 1000
	// DefaultBufferTTL is the maximum age of log lines kept in memory buffer.
	DefaultBufferTTL = 24 * time.Hour
)

type bufferEntry struct {
	timestamp time.Time
	line      string
}

type Logger struct {
	*log.Logger
	fileLogger *lumberjack.Logger
	mu         sync.Mutex
	entries    []bufferEntry
	maxBuffer  int
	bufferTTL  time.Duration
}

type Config struct {
	Enabled    bool
	FilePath   string
	MaxSize    int
	MaxBackups int
	MaxAge     int
	Compress   bool
	MaxBuffer  int
	BufferTTL  time.Duration
}

func New() *Logger {
	return &Logger{
		Logger:    log.New(os.Stdout, "", 0),
		entries:   make([]bufferEntry, 0, DefaultMaxBuffer),
		maxBuffer: DefaultMaxBuffer,
		bufferTTL: DefaultBufferTTL,
	}
}

func NewWithConfig(cfg *Config) *Logger {
	writers := []io.Writer{os.Stdout}

	var fileLogger *lumberjack.Logger
	maxBuffer := DefaultMaxBuffer
	bufferTTL := DefaultBufferTTL

	if cfg != nil {
		if cfg.Enabled && cfg.FilePath != "" {
			fileLogger = &lumberjack.Logger{
				Filename:   cfg.FilePath,
				MaxSize:    cfg.MaxSize,
				MaxBackups: cfg.MaxBackups,
				MaxAge:     cfg.MaxAge,
				Compress:   cfg.Compress,
			}
			writers = append(writers, fileLogger)
		}
		if cfg.MaxBuffer > 0 {
			maxBuffer = cfg.MaxBuffer
		}
		if cfg.BufferTTL > 0 {
			bufferTTL = cfg.BufferTTL
		}
	}

	multiWriter := io.MultiWriter(writers...)

	return &Logger{
		Logger:     log.New(multiWriter, "", 0),
		fileLogger: fileLogger,
		entries:    make([]bufferEntry, 0, maxBuffer),
		maxBuffer:  maxBuffer,
		bufferTTL:  bufferTTL,
	}
}

func (l *Logger) pruneLocked(now time.Time) {
	if l.bufferTTL <= 0 || len(l.entries) == 0 {
		return
	}
	cutoff := now.Add(-l.bufferTTL)
	idx := 0
	for idx < len(l.entries) && l.entries[idx].timestamp.Before(cutoff) {
		idx++
	}
	if idx > 0 {
		l.entries = l.entries[idx:]
	}
}

func (l *Logger) log(level, format string, args ...any) {
	now := time.Now()
	timestamp := now.Format("2006-01-02 15:04:05")
	message := fmt.Sprintf(format, args...)
	logLine := fmt.Sprintf("[%s] %s: %s", timestamp, level, message)

	// Store in buffer for support bundle generation with TTL & count bounds
	l.mu.Lock()
	l.pruneLocked(now)
	l.entries = append(l.entries, bufferEntry{
		timestamp: now,
		line:      logLine,
	})
	if len(l.entries) > l.maxBuffer {
		// Keep only the last maxBuffer entries
		l.entries = l.entries[len(l.entries)-l.maxBuffer:]
	}
	l.mu.Unlock()

	l.Printf("%s", logLine)
}

func (l *Logger) Info(format string, args ...any) {
	l.log("INFO", format, args...)
}

func (l *Logger) Error(format string, args ...any) {
	l.log("ERROR", format, args...)
}

func (l *Logger) Warn(format string, args ...any) {
	l.log("WARN", format, args...)
}

func (l *Logger) Debug(format string, args ...any) {
	l.log("DEBUG", format, args...)
}

func (l *Logger) Fatal(format string, args ...any) {
	l.log("FATAL", format, args...)
	os.Exit(1)
}

// GetRecentLogs returns all currently unexpired log lines from memory buffer.
func (l *Logger) GetRecentLogs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.pruneLocked(time.Now())
	logs := make([]string, len(l.entries))
	for i, entry := range l.entries {
		logs[i] = entry.line
	}
	return logs
}

// GetRecentLogsSince returns unexpired buffered logs newer than the specified relative duration.
func (l *Logger) GetRecentLogsSince(since time.Duration) []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	l.pruneLocked(now)
	cutoff := now.Add(-since)
	var logs []string
	for _, entry := range l.entries {
		if !entry.timestamp.Before(cutoff) {
			logs = append(logs, entry.line)
		}
	}
	return logs
}

// SetBufferTTL configures the retention TTL for buffered in-memory logs.
func (l *Logger) SetBufferTTL(ttl time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.bufferTTL = ttl
	l.pruneLocked(time.Now())
}

// Close file logger
func (l *Logger) Close() error {
	if l.fileLogger != nil {
		return l.fileLogger.Close()
	}
	return nil
}

// GetLogFilePath returns current log file path
func (l *Logger) GetLogFilePath() string {
	if l.fileLogger != nil {
		return l.fileLogger.Filename
	}
	return ""
}
