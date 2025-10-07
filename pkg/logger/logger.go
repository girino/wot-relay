package logger

import (
	"encoding/json"
	"log"
	"time"
)

// LogLevel represents the logging level
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

// Logger provides structured logging
type Logger struct {
	level LogLevel
}

var defaultLogger *Logger

// NewLogger creates a new logger instance
func NewLogger(level LogLevel) *Logger {
	return &Logger{level: level}
}

// SetDefault sets the default logger instance
func SetDefault(logger *Logger) {
	defaultLogger = logger
}

// GetDefault returns the default logger instance
func GetDefault() *Logger {
	if defaultLogger == nil {
		defaultLogger = NewLogger(INFO) // Default to INFO level
	}
	return defaultLogger
}

// Log writes a structured log entry
func (l *Logger) Log(level LogLevel, component, message string, fields ...map[string]interface{}) {
	if level < l.level {
		return
	}

	levelNames := map[LogLevel]string{
		DEBUG: "DEBUG",
		INFO:  "INFO",
		WARN:  "WARN",
		ERROR: "ERROR",
	}

	timestamp := time.Now().Format("2006/01/02 15:04:05")
	levelName := levelNames[level]

	var fieldStr string
	if len(fields) > 0 {
		if jsonBytes, err := json.Marshal(fields[0]); err == nil {
			fieldStr = " " + string(jsonBytes)
		}
	}

	log.Printf("[%s] %s [%s] %s%s", timestamp, levelName, component, message, fieldStr)
}

// Debug logs a debug message
func (l *Logger) Debug(component, message string, fields ...map[string]interface{}) {
	l.Log(DEBUG, component, message, fields...)
}

// Info logs an info message
func (l *Logger) Info(component, message string, fields ...map[string]interface{}) {
	l.Log(INFO, component, message, fields...)
}

// Warn logs a warning message
func (l *Logger) Warn(component, message string, fields ...map[string]interface{}) {
	l.Log(WARN, component, message, fields...)
}

// Error logs an error message
func (l *Logger) Error(component, message string, fields ...map[string]interface{}) {
	l.Log(ERROR, component, message, fields...)
}

// Convenience functions that use the default logger

// Debug logs a debug message using the default logger
func Debug(component, message string, fields ...map[string]interface{}) {
	GetDefault().Debug(component, message, fields...)
}

// Info logs an info message using the default logger
func Info(component, message string, fields ...map[string]interface{}) {
	GetDefault().Info(component, message, fields...)
}

// Warn logs a warning message using the default logger
func Warn(component, message string, fields ...map[string]interface{}) {
	GetDefault().Warn(component, message, fields...)
}

// Error logs an error message using the default logger
func Error(component, message string, fields ...map[string]interface{}) {
	GetDefault().Error(component, message, fields...)
}
