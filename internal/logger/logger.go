package logger

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

type LogLevel string

const (
	LogInfo  LogLevel = "INFO"
	LogWarn  LogLevel = "WARN"
	LogError LogLevel = "ERROR"
	LogPkt   LogLevel = "PKT"
)

type Logger struct {
	mu          sync.Mutex
	stdWriter   io.Writer
	fileWriter  *bufio.Writer
	file        *os.File
	logPackets  bool
	flushTicker *time.Ticker
	done        chan struct{}
	logCallback func(string)
}

func New(logPackets bool, logFile string) (*Logger, error) {
	l := &Logger{
		stdWriter:  os.Stdout,
		logPackets: logPackets,
		done:       make(chan struct{}),
	}

	if logPackets && logFile != "" {
		file, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			l.Warn("Failed to open log file %s: %v, packet logging to file disabled", logFile, err)
		} else {
			l.file = file
			l.fileWriter = bufio.NewWriterSize(file, 4096)

			// Start periodic flush
			l.flushTicker = time.NewTicker(time.Second)
			go l.flushLoop()
		}
	}

	return l, nil
}

func (l *Logger) flushLoop() {
	for {
		select {
		case <-l.flushTicker.C:
			l.mu.Lock()
			if l.fileWriter != nil {
				l.fileWriter.Flush()
			}
			l.mu.Unlock()
		case <-l.done:
			return
		}
	}
}

func (l *Logger) Close() {
	if l.flushTicker != nil {
		l.flushTicker.Stop()
		close(l.done)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.fileWriter != nil {
		l.fileWriter.Flush()
	}
	if l.file != nil {
		l.file.Close()
	}
}

func (l *Logger) log(level LogLevel, format string, args ...interface{}) {
	timestamp := time.Now().Format(time.RFC3339Nano)
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("%s [%s] %s\n", timestamp, level, msg)

	l.mu.Lock()
	defer l.mu.Unlock()

	fmt.Fprint(l.stdWriter, line)

	if l.logCallback != nil {
		l.logCallback(line)
	}
}

func (l *Logger) Info(format string, args ...interface{}) {
	l.log(LogInfo, format, args...)
}

func (l *Logger) Warn(format string, args ...interface{}) {
	l.log(LogWarn, format, args...)
}

func (l *Logger) Error(format string, args ...interface{}) {
	l.log(LogError, format, args...)
}

func (l *Logger) LogPacket(direction string, data []byte, source string) {
	// If neither packet logging nor callback is enabled, return early
	if !l.logPackets && l.logCallback == nil {
		return
	}

	timestamp := time.Now().Format(time.RFC3339Nano)
	hexStr := hex.EncodeToString(data)

	// Format hex with spaces
	var formattedHex string
	for i := 0; i < len(hexStr); i += 2 {
		if i > 0 {
			formattedHex += " "
		}
		if i+2 <= len(hexStr) {
			formattedHex += hexStr[i : i+2]
		}
	}

	var line string
	if source != "" {
		line = fmt.Sprintf("%s [%s] [%s] %s (%d bytes) from %s\n",
			timestamp, LogPkt, direction, formattedHex, len(data), source)
	} else {
		line = fmt.Sprintf("%s [%s] [%s] %s (%d bytes)\n",
			timestamp, LogPkt, direction, formattedHex, len(data))
	}

	// Get callback reference while holding lock
	l.mu.Lock()
	callback := l.logCallback

	// Only write to stdout/file if enabled
	if l.logPackets {
		fmt.Fprint(l.stdWriter, line)

		if l.fileWriter != nil {
			_, _ = l.fileWriter.WriteString(line)
		}
	}
	l.mu.Unlock()

	// Call callback outside of lock to prevent deadlock
	if callback != nil {
		callback(line)
	}
}

// SetOutput sets the output writer (for testing)
func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stdWriter = w
}

// IsPacketLoggingEnabled returns whether packet logging is enabled
func (l *Logger) IsPacketLoggingEnabled() bool {
	return l.logPackets
}

// SetLogCallback sets a callback function that receives all log entries
func (l *Logger) SetLogCallback(cb func(string)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logCallback = cb
}
