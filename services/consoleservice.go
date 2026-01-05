package services

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// ConsoleLog 控制台日志条目
type ConsoleLog struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"` // INFO, WARN, ERROR
	Message   string    `json:"message"`
}

// ConsoleService 控制台日志服务
type ConsoleService struct {
	logs      []ConsoleLog
	mutex     sync.RWMutex
	maxLogs   int
	writer    *consoleWriter
	oldStdout *os.File
	oldStderr *os.File
}

// consoleWriter 自定义 writer，同时写入控制台和缓存
type consoleWriter struct {
	service *ConsoleService
	level   string
	output  io.Writer
}

func (w *consoleWriter) Write(p []byte) (n int, err error) {
	// 写入原始输出
	n, err = w.output.Write(p)

	// 过滤掉 GetLogs 返回的 JSON 数据，避免反馈循环
	msg := string(p)
	if !w.service.shouldSkipLog(msg) {
		// 添加到日志缓存
		w.service.addLog(w.level, msg)
	}

	return n, err
}

func NewConsoleService() *ConsoleService {
	cs := &ConsoleService{
		logs:    make([]ConsoleLog, 0, 1000),
		maxLogs: 1000, // 最多保留 1000 条日志
	}

	// 捕获标准输出和标准错误
	cs.captureStdout()

	return cs
}

// captureStdout 捕获标准输出和标准错误
func (cs *ConsoleService) captureStdout() {
	// 保存原始输出
	cs.oldStdout = os.Stdout
	cs.oldStderr = os.Stderr

	// 创建管道
	stdoutReader, stdoutWriter, _ := os.Pipe()
	stderrReader, stderrWriter, _ := os.Pipe()

	// 替换标准输出
	os.Stdout = stdoutWriter
	os.Stderr = stderrWriter
	log.SetOutput(stdoutWriter)

	// 启动 goroutine 读取管道内容
	go cs.readPipe(stdoutReader, "INFO", cs.oldStdout)
	go cs.readPipe(stderrReader, "ERROR", cs.oldStderr)
}

// readPipe 读取管道内容
func (cs *ConsoleService) readPipe(reader *os.File, level string, output *os.File) {
	// 使用更大的缓冲区减少消息碎片化
	buf := make([]byte, 16384)
	for {
		n, err := reader.Read(buf)
		if err != nil {
			if err != io.EOF {
				fmt.Fprintf(output, "读取管道失败: %v\n", err)
			}
			return
		}

		if n > 0 {
			msg := string(buf[:n])
			// 写入原始输出
			output.Write(buf[:n])
			// 过滤掉 GetLogs 返回的 JSON 数据，避免反馈循环
			// 这些数据包含大量转义的 JSON，会导致日志爆炸
			if cs.shouldSkipLog(msg) {
				continue
			}
			// 添加到日志缓存
			cs.addLog(level, msg)
		}
	}
}

// shouldSkipLog 检查是否应该跳过此日志（避免 GetLogs 响应的反馈循环）
func (cs *ConsoleService) shouldSkipLog(message string) bool {
	// 跳过 ConsoleService.GetLogs 的 Wails binding 调用日志
	// 这些日志会在每次前端轮询时产生，导致日志爆炸
	if strings.Contains(message, "ConsoleService.GetLogs") {
		return true
	}
	// 跳过包含大量转义反斜杠的内容（GetLogs JSON 响应的特征）
	// 正常日志不会有连续4个以上的反斜杠
	if strings.Contains(message, `\\\\`) {
		return true
	}
	// 跳过看起来像 ConsoleLog JSON 数组的内容
	if strings.Contains(message, `"timestamp":`) && strings.Contains(message, `"level":`) && strings.Contains(message, `"message":`) {
		return true
	}
	// 跳过 Wails 框架的 Asset Request 日志（静态资源请求）
	// 这些日志量很大且没有调试价值
	if strings.Contains(message, "Asset Request:") {
		return true
	}
	// 跳过 Wails 框架的 Binding call 日志（降级为 DEBUG，不显示）
	// 这些日志在每次前端调用后端服务时产生，频率很高
	if strings.Contains(message, "Binding call started:") || strings.Contains(message, "Binding call complete:") {
		return true
	}
	return false
}

// addLog 添加日志到缓存
func (cs *ConsoleService) addLog(level, message string) {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	// 尝试从消息内容解析真实的日志级别
	detectedLevel := detectLogLevel(message, level)

	log := ConsoleLog{
		Timestamp: time.Now(),
		Level:     detectedLevel,
		Message:   message,
	}

	cs.logs = append(cs.logs, log)

	// 限制日志数量
	if len(cs.logs) > cs.maxLogs {
		cs.logs = cs.logs[len(cs.logs)-cs.maxLogs:]
	}

	// 清理3天前的日志
	cs.cleanOldLogs()
}

// detectLogLevel 从消息内容检测实际的日志级别
// 很多框架（如 Wails）将日志写入 stderr，但消息中包含实际的级别标识
func detectLogLevel(message, fallback string) string {
	// 只检查消息的前200个字符，避免在大量 JSON 数据中误匹配
	// 同时避免匹配 JSON 中的 "level":"ERROR" 等字段
	checkLen := len(message)
	if checkLen > 200 {
		checkLen = 200
	}
	prefix := message[:checkLen]

	// 如果消息看起来像 JSON 数据（包含大量转义或 JSON 结构），使用默认级别
	// 这避免了 GetLogs 返回的 JSON 数据被错误分类的问题
	if strings.Contains(prefix, `\"level\"`) || strings.Contains(prefix, `"level":`) {
		return fallback
	}

	msg := strings.ToUpper(prefix)

	// 检查常见的日志级别模式
	// Wails/zerolog 格式: "Jan 5 12:53:01.111 INF ..."
	// 标准格式: "[INFO]", "[ERROR]", "INFO:", "ERROR:"
	errorPatterns := []string{
		" ERR ", " ERROR ", "[ERR]", "[ERROR]", "ERROR:", "ERR:",
		" FTL ", " FATAL ", "[FTL]", "[FATAL]", "FATAL:", "FTL:",
		" PNC ", " PANIC ", "[PNC]", "[PANIC]", "PANIC:", "PNC:",
	}
	warnPatterns := []string{
		" WRN ", " WARN ", " WARNING ", "[WRN]", "[WARN]", "[WARNING]",
		"WARN:", "WRN:", "WARNING:",
	}
	infoPatterns := []string{
		" INF ", " INFO ", "[INF]", "[INFO]", "INFO:", "INF:",
		" DBG ", " DEBUG ", "[DBG]", "[DEBUG]", "DEBUG:", "DBG:",
		" TRC ", " TRACE ", "[TRC]", "[TRACE]", "TRACE:", "TRC:",
	}

	// 优先检测 ERROR（最严重）
	for _, pattern := range errorPatterns {
		if strings.Contains(msg, pattern) {
			return "ERROR"
		}
	}

	// 其次检测 WARN
	for _, pattern := range warnPatterns {
		if strings.Contains(msg, pattern) {
			return "WARN"
		}
	}

	// 最后检测 INFO/DEBUG/TRACE（都归类为 INFO）
	for _, pattern := range infoPatterns {
		if strings.Contains(msg, pattern) {
			return "INFO"
		}
	}

	// 如果消息中没有检测到级别，使用传入的默认值
	return fallback
}

// cleanOldLogs 清理3天前的日志
func (cs *ConsoleService) cleanOldLogs() {
	// 无需加锁，因为调用者 addLog 已经加锁
	threeDaysAgo := time.Now().Add(-72 * time.Hour)

	// 找到第一个在3天内的日志索引
	cutoffIndex := 0
	for i, log := range cs.logs {
		if log.Timestamp.After(threeDaysAgo) {
			cutoffIndex = i
			break
		}
	}

	// 如果有旧日志需要清理
	if cutoffIndex > 0 {
		cs.logs = cs.logs[cutoffIndex:]
		fmt.Printf("[ConsoleService] 清理了 %d 条超过3天的日志\n", cutoffIndex)
	}
}

// GetLogs 获取所有日志
func (cs *ConsoleService) GetLogs() []ConsoleLog {
	cs.mutex.RLock()
	defer cs.mutex.RUnlock()

	// 返回副本
	result := make([]ConsoleLog, len(cs.logs))
	copy(result, cs.logs)
	return result
}

// GetRecentLogs 获取最近 N 条日志
func (cs *ConsoleService) GetRecentLogs(count int) []ConsoleLog {
	cs.mutex.RLock()
	defer cs.mutex.RUnlock()

	if count <= 0 {
		count = 100
	}

	if count > len(cs.logs) {
		count = len(cs.logs)
	}

	// 返回最后 N 条
	result := make([]ConsoleLog, count)
	copy(result, cs.logs[len(cs.logs)-count:])
	return result
}

// ClearLogs 清空日志
func (cs *ConsoleService) ClearLogs() {
	cs.mutex.Lock()
	defer cs.mutex.Unlock()

	cs.logs = make([]ConsoleLog, 0, 1000)
}
