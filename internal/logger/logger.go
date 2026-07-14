package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	DEBUG = 0
	INFO  = 1
	WARN  = 2
	ERROR = 3
)

var (
	logLevel      int
	fileLogLevel  = DEBUG
	logMu         sync.Mutex
	logFile       *os.File
	logFilePath   string
	logFileBytes  int64
	lastFileSync  time.Time
	maxLogBytes   int64 = 64 << 20
	logFileCopies       = 5
)

func init() {
	env := strings.ToUpper(os.Getenv("LOG_LEVEL"))
	switch env {
	case "DEBUG":
		logLevel = DEBUG
	case "WARN":
		logLevel = WARN
	case "ERROR":
		logLevel = ERROR
	default:
		logLevel = INFO
	}
}

func log(msg string, level int) {
	logMu.Lock()
	defer logMu.Unlock()
	if level < logLevel && (logFile == nil || level < fileLogLevel) {
		return
	}
	now := time.Now()
	ts := now.Format("2006-01-02 15:04:05.000")
	prefix := "INFO"
	switch level {
	case DEBUG:
		prefix = "DEBUG"
	case WARN:
		prefix = "WARN"
	case ERROR:
		prefix = "ERROR"
	}
	line := fmt.Sprintf("[%s] [%s] %s\n", ts, prefix, msg)
	if level >= logLevel {
		fmt.Print(line)
	}
	if logFile != nil && level >= fileLogLevel {
		rotateIfNeededLocked(int64(len(line)))
		if logFile != nil {
			n, _ := logFile.WriteString(line)
			logFileBytes += int64(n)
			if level >= ERROR || now.Sub(lastFileSync) >= 5*time.Second {
				_ = logFile.Sync()
				lastFileSync = now
			}
		}
	}
}

func ConfigureFile(path string, level int) error {
	logMu.Lock()
	defer logMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if logFile != nil {
		_ = logFile.Close()
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	info, _ := f.Stat()
	logFile = f
	logFilePath = path
	fileLogLevel = level
	logFileBytes = 0
	if info != nil {
		logFileBytes = info.Size()
	}
	lastFileSync = time.Now()
	return nil
}

func CloseFile() {
	logMu.Lock()
	defer logMu.Unlock()
	if logFile != nil {
		_ = logFile.Sync()
		_ = logFile.Close()
		logFile = nil
	}
}

func rotateIfNeededLocked(incoming int64) {
	if logFile == nil || logFileBytes+incoming <= maxLogBytes {
		return
	}
	_ = logFile.Sync()
	_ = logFile.Close()
	for i := logFileCopies - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", logFilePath, i)
		newPath := fmt.Sprintf("%s.%d", logFilePath, i+1)
		_ = os.Rename(oldPath, newPath)
	}
	_ = os.Rename(logFilePath, logFilePath+".1")
	logFile, _ = os.OpenFile(logFilePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	logFileBytes = 0
}

func Debug(msg string) { log(msg, DEBUG) }
func Info(msg string)  { log(msg, INFO) }
func Warn(msg string)  { log(msg, WARN) }
func Error(msg string) { log(msg, ERROR) }

func Debugf(format string, a ...any) { log(fmt.Sprintf(format, a...), DEBUG) }
func Infof(format string, a ...any)  { log(fmt.Sprintf(format, a...), INFO) }
func Warnf(format string, a ...any)  { log(fmt.Sprintf(format, a...), WARN) }
func Errorf(format string, a ...any) { log(fmt.Sprintf(format, a...), ERROR) }

func Mask(s string, show int) string {
	if s == "" || len(s) <= show {
		return "****"
	}
	return s[:show] + "****"
}

func MaskPhone(phone string) string {
	if phone == "" || len(phone) < 7 {
		return "****"
	}
	return phone[:3] + "****" + phone[len(phone)-4:]
}
