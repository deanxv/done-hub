package logger

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"go-template/common/utils"

	"github.com/spf13/viper"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	loggerINFO  = "INFO"
	loggerWarn  = "WARN"
	loggerError = "ERR"
	loggerDEBUG = "DEBUG"
)
const (
	RequestIdKey = "X-Request-Id"
)

// LogEntry represents a single log entry in memory
type LogEntry struct {
	Timestamp time.Time
	Level     string
	Message   string
}

// LogHistory stores the latest logs in memory
type LogHistory struct {
	entries []LogEntry
	mutex   sync.RWMutex
	maxSize int
}

// Global log history instance with a default capacity of 1000 entries
var logHistory = &LogHistory{
	entries: make([]LogEntry, 0, 1000),
	maxSize: 1000,
}

// AddEntry adds a new log entry to the history
func (lh *LogHistory) AddEntry(level, message string) {
	lh.mutex.Lock()
	defer lh.mutex.Unlock()

	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
	}

	// Add the new entry
	lh.entries = append(lh.entries, entry)

	// If we've exceeded the max size, remove the oldest entry
	if len(lh.entries) > lh.maxSize {
		lh.entries = lh.entries[1:]
	}
}

// GetLatestEntries returns the latest n entries from the log history
func (lh *LogHistory) GetLatestEntries(n int) []LogEntry {
	lh.mutex.RLock()
	defer lh.mutex.RUnlock()

	if n <= 0 || n > len(lh.entries) {
		n = len(lh.entries)
	}

	// Get the latest n entries
	startIdx := len(lh.entries) - n
	if startIdx < 0 {
		startIdx = 0
	}

	// Create a copy of the entries to avoid race conditions
	result := make([]LogEntry, len(lh.entries)-startIdx)
	copy(result, lh.entries[startIdx:])

	return result
}

var Logger *zap.Logger

var defaultLogDir = "./logs"

func SetupLogger() {
	logDir := getLogDir()
	if logDir == "" {
		return
	}

	writeSyncer := getLogWriter(logDir)

	encoder := getEncoder()

	core := zapcore.NewCore(
		encoder,
		writeSyncer,
		zap.NewAtomicLevelAt(getLogLevel()),
	)
	Logger = zap.New(core, zap.AddCaller())
}

func getEncoder() zapcore.Encoder {
	encodeConfig := zap.NewProductionEncoderConfig()

	encodeConfig.EncodeTime = zapcore.TimeEncoderOfLayout("2006/01/02 - 15:04:05")
	encodeConfig.TimeKey = "time"
	encodeConfig.EncodeLevel = zapcore.CapitalLevelEncoder
	encodeConfig.EncodeCaller = zapcore.ShortCallerEncoder

	encodeConfig.EncodeDuration = zapcore.StringDurationEncoder

	return zapcore.NewConsoleEncoder(encodeConfig)
}

func getLogWriter(logDir string) zapcore.WriteSyncer {
	filename := utils.GetOrDefault("logs.filename", "go-template.log")
	logPath := filepath.Join(logDir, filename)

	maxsize := utils.GetOrDefault("logs.max_size", 100)
	maxAge := utils.GetOrDefault("logs.max_age", 7)
	maxBackup := utils.GetOrDefault("logs.max_backup", 10)
	compress := utils.GetOrDefault("logs.compress", false)

	lumberJackLogger := &lumberjack.Logger{
		Filename:   logPath,   // 文件位置
		MaxSize:    maxsize,   // 进行切割之前,日志文件的最大大小(MB为单位)
		MaxAge:     maxAge,    // 保留旧文件的最大天数
		MaxBackups: maxBackup, // 保留旧文件的最大个数
		Compress:   compress,  // 是否压缩/归档旧文件
	}

	return zapcore.NewMultiWriteSyncer(zapcore.AddSync(lumberJackLogger), zapcore.AddSync(os.Stderr))
}

func getLogLevel() zapcore.Level {
	logLevel := viper.GetString("log_level")
	switch logLevel {
	case "debug":
		return zap.DebugLevel
	case "info":
		return zap.InfoLevel
	case "warn":
		return zap.WarnLevel
	case "error":
		return zap.ErrorLevel
	case "panic":
		return zap.PanicLevel
	case "fatal":
		return zap.FatalLevel
	default:
		return zap.InfoLevel
	}

}

func getLogDir() string {
	logDir := viper.GetString("log_dir")
	if logDir == "" {
		logDir = defaultLogDir
	}

	var err error
	logDir, err = filepath.Abs(logDir)
	if err != nil {
		log.Fatal(err)
		return ""
	}

	if !utils.IsFileExist(logDir) {
		err = os.Mkdir(logDir, 0777)
		if err != nil {
			log.Fatal(err)
			return ""
		}
	}

	return logDir
}

func SysLog(s string) {
	message := "[SYS] | " + s

	// Add to in-memory log history
	logHistory.AddEntry(loggerINFO, message)

	// Write to log file
	entry := zapcore.Entry{
		Level:   zapcore.InfoLevel,
		Time:    time.Now(),
		Message: message,
	}

	// 使用 Logger 的核心来直接写入日志，绕过等级检查
	if ce := Logger.Core().With([]zapcore.Field{}); ce != nil {
		ce.Write(entry, nil)
	}
}

func SysError(s string) {
	message := "[SYS] | " + s

	// Add to in-memory log history
	logHistory.AddEntry(loggerError, message)

	// Write to log file
	Logger.Error(message)
}

func SysDebug(s string) {
	message := "[SYS] | " + s

	// Add to in-memory log history
	logHistory.AddEntry(loggerDEBUG, message)

	// Write to log file
	Logger.Debug(message)
}

func LogInfo(ctx context.Context, msg string) {
	logHelper(ctx, loggerINFO, msg)
}

func LogWarn(ctx context.Context, msg string) {
	logHelper(ctx, loggerWarn, msg)
}

func LogError(ctx context.Context, msg string) {
	logHelper(ctx, loggerError, msg)
}

func LogDebug(ctx context.Context, msg string) {
	logHelper(ctx, loggerDEBUG, msg)
}

func logHelper(ctx context.Context, level string, msg string) {
	id, ok := ctx.Value(RequestIdKey).(string)
	if !ok {
		id = "unknown"
	}

	logMsg := fmt.Sprintf("%s | %s \n", id, msg)

	// Add to in-memory log history
	logHistory.AddEntry(level, logMsg)

	// Write to log file
	switch level {
	case loggerINFO:
		Logger.Info(logMsg)
	case loggerWarn:
		Logger.Warn(logMsg)
	case loggerError:
		Logger.Error(logMsg)
	case loggerDEBUG:
		Logger.Debug(logMsg)
	default:
		Logger.Info(logMsg)
	}
}

func FatalLog(v ...any) {
	message := fmt.Sprintf("[FATAL] %v | %v \n", time.Now().Format("2006/01/02 - 15:04:05"), v)

	// Add to in-memory log history
	logHistory.AddEntry("FATAL", message)

	// Write to log file
	Logger.Fatal(message)
	// t := time.Now()
	// _, _ = fmt.Fprintf(gin.DefaultErrorWriter, "[FATAL] %v | %v \n", t.Format("2006/01/02 - 15:04:05"), v)
	os.Exit(1)
}

// LogQueryParams represents parameters for advanced log queries
type LogQueryParams struct {
	Count      int    `json:"count"`
	SearchTerm string `json:"search_term,omitempty"`
	UseRegex   bool   `json:"use_regex,omitempty"`
	FromFile   bool   `json:"from_file,omitempty"`
}

// LogQueryResult represents the result of a log query
type LogQueryResult struct {
	Logs       []LogEntry `json:"logs"`
	TotalCount int        `json:"total_count"`
	HasMore    bool       `json:"has_more"`
}

// GetLatestLogs gets the latest n logs from memory
func GetLatestLogs(n int) ([]LogEntry, error) {
	// Get the latest entries from the in-memory log history
	entries := logHistory.GetLatestEntries(n)

	return entries, nil
}

// QueryLogs performs advanced log queries
func QueryLogs(params LogQueryParams) (*LogQueryResult, error) {
	if params.FromFile {
		return queryLogsFromFile(params)
	}
	return queryLogsFromMemory(params)
}

// queryLogsFromMemory queries logs from in-memory storage
func queryLogsFromMemory(params LogQueryParams) (*LogQueryResult, error) {
	entries := logHistory.GetLatestEntries(params.Count)

	// Apply filters
	filtered := filterLogs(entries, params)

	return &LogQueryResult{
		Logs:       filtered,
		TotalCount: len(filtered),
		HasMore:    false, // Memory storage is limited, so no "more" concept
	}, nil
}

// queryLogsFromFile queries logs from log files
func queryLogsFromFile(params LogQueryParams) (*LogQueryResult, error) {
	logDir := getLogDir()
	if logDir == "" {
		return nil, fmt.Errorf("log directory not configured")
	}

	filename := utils.GetOrDefault("logs.filename", "go-template.log")
	logPath := filepath.Join(logDir, filename)

	// Read logs from file
	entries, err := readLogsFromFile(logPath, params)
	if err != nil {
		return nil, err
	}

	// Apply filters
	filtered := filterLogs(entries, params)

	// Limit results
	hasMore := len(filtered) > params.Count
	if hasMore {
		filtered = filtered[:params.Count]
	}

	return &LogQueryResult{
		Logs:       filtered,
		TotalCount: len(filtered),
		HasMore:    hasMore,
	}, nil
}

// readLogsFromFile reads logs from the log file
func readLogsFromFile(logPath string, params LogQueryParams) ([]LogEntry, error) {
	file, err := os.Open(logPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %v", err)
	}
	defer file.Close()

	var entries []LogEntry
	scanner := bufio.NewScanner(file)

	// Read file line by line
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		entry, err := parseLogLine(line)
		if err != nil {
			// Skip invalid lines
			continue
		}

		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading log file: %v", err)
	}

	// Reverse to get newest first
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	return entries, nil
}

// parseLogLine parses a single log line into LogEntry
func parseLogLine(line string) (LogEntry, error) {
	// Expected format: "2006/01/02 - 15:04:05	LEVEL	message"
	parts := strings.SplitN(line, "\t", 3)
	if len(parts) < 3 {
		return LogEntry{}, fmt.Errorf("invalid log line format")
	}

	// Parse timestamp
	timestamp, err := time.Parse("2006/01/02 - 15:04:05", parts[0])
	if err != nil {
		return LogEntry{}, fmt.Errorf("failed to parse timestamp: %v", err)
	}

	level := strings.ToLower(strings.TrimSpace(parts[1]))
	message := parts[2]

	return LogEntry{
		Timestamp: timestamp,
		Level:     level,
		Message:   message,
	}, nil
}

// filterLogs applies filters to log entries
func filterLogs(entries []LogEntry, params LogQueryParams) []LogEntry {
	var filtered []LogEntry

	for _, entry := range entries {
		// Search term filter
		if params.SearchTerm != "" {
			if params.UseRegex {
				matched, err := regexp.MatchString(params.SearchTerm, entry.Message)
				if err != nil || !matched {
					continue
				}
			} else {
				if !strings.Contains(strings.ToLower(entry.Message), strings.ToLower(params.SearchTerm)) {
					continue
				}
			}
		}

		filtered = append(filtered, entry)
	}

	return filtered
}
