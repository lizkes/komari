package utils

import (
	"log"
	"os"
	"strings"
)

// LogLevel 日志级别
type LogLevel int

const (
	INFO LogLevel = iota
	DEBUG
)

var (
	currentLevel LogLevel = INFO
	timeFormat   string   = "2006-01-02 15:04:05"
)

func init() {
	// 从环境变量读取日志级别
	if level := os.Getenv("KOMARI_LOG_LEVEL"); level != "" {
		SetLevelFromString(level)
	}
}

// SetLevel 设置日志级别
func SetLevel(level LogLevel) {
	currentLevel = level
}

// SetLevelFromString 从字符串设置日志级别
func SetLevelFromString(level string) {
	switch strings.ToLower(level) {
	case "debug":
		currentLevel = DEBUG
	case "info":
		currentLevel = INFO
	default:
		currentLevel = INFO
	}
}

// GetLevelString 获取当前日志级别字符串
func GetLevelString() string {
	switch currentLevel {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	default:
		return "INFO"
	}
}

// LogInfo 输出INFO级别日志
func LogInfo(format string, args ...interface{}) {
	if currentLevel >= INFO {
		log.Printf("[INFO] "+format, args...)
	}
}

// LogDebug 输出DEBUG级别日志
func LogDebug(format string, args ...interface{}) {
	if currentLevel >= DEBUG {
		log.Printf("[DEBUG] "+format, args...)
	}
}

// LogError 输出错误日志（总是显示）
func LogError(format string, args ...interface{}) {
	log.Printf("[ERROR] "+format, args...)
}

// LogWarn 输出警告日志
func LogWarn(format string, args ...interface{}) {
	if currentLevel >= INFO {
		log.Printf("[WARN] "+format, args...)
	}
}

// IsDebugEnabled 检查是否启用了DEBUG级别
func IsDebugEnabled() bool {
	return currentLevel >= DEBUG
}
