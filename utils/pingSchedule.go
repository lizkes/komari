package utils

import (
	"log"
	"net"
	"sync"
	"time"

	"github.com/komari-monitor/komari/database/models"
)

// IPProtocol 定义IP协议类型
type IPProtocol int

const (
	IPProtocolUnknown IPProtocol = iota
	IPProtocolIPv4
	IPProtocolIPv6
	IPProtocolBoth
)

// String 将IPProtocol转换为可读字符串
func (p IPProtocol) String() string {
	switch p {
	case IPProtocolIPv4:
		return "IPv4"
	case IPProtocolIPv6:
		return "IPv6"
	case IPProtocolBoth:
		return "IPv4+IPv6"
	case IPProtocolUnknown:
		return "Unknown"
	default:
		return "Unknown"
	}
}

// ClientIPInfo 客户端IP信息
type ClientIPInfo struct {
	IPv4 string
	IPv6 string
}

// 缓存配置常量
const (
	ProtocolCacheExpiration = 60 * time.Minute // 协议缓存60分钟
	MaxCacheSize            = 3000             // 最大缓存条目数
)

// CacheEntry 缓存条目
type CacheEntry struct {
	Value     IPProtocol
	ExpiresAt time.Time
}

// IPProtocolCache IP协议检测缓存管理器
type IPProtocolCache struct {
	protocolCache map[string]CacheEntry // 协议检测缓存（支持目标地址和客户端协议）
	mutex         sync.RWMutex
	stats         CacheStats
}

// CacheStats 缓存统计
type CacheStats struct {
	Hits        int64
	Misses      int64
	CleanupRuns int64
}

// 全局缓存实例
var ipProtocolCache = &IPProtocolCache{
	protocolCache: make(map[string]CacheEntry),
}

// init 初始化缓存清理定时器
func init() {
	// 每10分钟清理一次过期缓存
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			ipProtocolCache.cleanup()
		}
	}()
}

// getProtocolResult 获取缓存的协议检测结果
func (c *IPProtocolCache) getProtocolResult(target string) (IPProtocol, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	if entry, exists := c.protocolCache[target]; exists && time.Now().Before(entry.ExpiresAt) {
		c.stats.Hits++
		return entry.Value, true
	}
	c.stats.Misses++
	return IPProtocolUnknown, false
}

// setProtocolResult 设置协议检测结果缓存
func (c *IPProtocolCache) setProtocolResult(target string, protocol IPProtocol) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// 检查缓存大小限制
	if len(c.protocolCache) >= MaxCacheSize {
		// 简单的LRU：删除第一个过期的条目
		now := time.Now()
		for k, v := range c.protocolCache {
			if now.After(v.ExpiresAt) {
				delete(c.protocolCache, k)
				break
			}
		}
	}

	c.protocolCache[target] = CacheEntry{
		Value:     protocol,
		ExpiresAt: time.Now().Add(ProtocolCacheExpiration),
	}
}

// cleanup 清理过期缓存
func (c *IPProtocolCache) cleanup() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	now := time.Now()
	cleaned := 0

	// 清理过期的统一协议缓存（目标+客户端）
	for k, v := range c.protocolCache {
		if now.After(v.ExpiresAt) {
			delete(c.protocolCache, k)
			cleaned++
		}
	}

	if cleaned > 0 {
		// log.Printf("缓存清理完成: 统一协议缓存清理%d条", cleaned)
	}
	c.stats.CleanupRuns++
}

// DetectTargetIPProtocol 检测目标地址的IP协议类型（带统一缓存）
func DetectTargetIPProtocol(target string) IPProtocol {
	// 先尝试从统一缓存获取
	if protocol, hit := ipProtocolCache.getProtocolResult(target); hit {
		// log.Printf("目标协议缓存命中: %s -> %v", target, protocol)
		return protocol
	}

	// log.Printf("目标协议缓存未命中，开始检测: %s", target)

	// 执行完整的协议检测
	protocol := detectProtocolDirect(target)

	// 缓存结果到统一缓存
	ipProtocolCache.setProtocolResult(target, protocol)
	// log.Printf("目标协议检测完成: %s -> %v", target, protocol)
	return protocol
}

// detectProtocolDirect 直接执行协议检测（无缓存）
func detectProtocolDirect(target string) IPProtocol {
	// 移除端口号（如果有）
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		host = target
	}

	// 先尝试直接解析为IP地址
	ip := net.ParseIP(host)
	if ip != nil {
		if ip.To4() != nil {
			return IPProtocolIPv4
		}
		return IPProtocolIPv6
	}

	// 不是IP地址，是域名，需要解析
	addrs, err := net.LookupHost(host)
	if err != nil {
		log.Printf("无法解析域名 %s: %v", host, err)
		return IPProtocolUnknown
	}

	hasIPv4 := false
	hasIPv6 := false
	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip.To4() != nil {
			hasIPv4 = true
		} else {
			hasIPv6 = true
		}
	}

	if hasIPv4 && hasIPv6 {
		return IPProtocolBoth
	} else if hasIPv4 {
		return IPProtocolIPv4
	} else if hasIPv6 {
		return IPProtocolIPv6
	}
	return IPProtocolUnknown
}

// getClientIPCapabilities 根据客户端IP信息获取IP协议能力
// 复用统一的协议检测逻辑，提高缓存效率
func getClientIPCapabilities(ipInfo ClientIPInfo) IPProtocol {
	var hasIPv4, hasIPv6 bool

	// 检测IPv4能力（复用协议检测和缓存）
	if ipInfo.IPv4 != "" && ipInfo.IPv4 != "0.0.0.0" {
		ipv4Protocol := DetectTargetIPProtocol(ipInfo.IPv4)
		hasIPv4 = (ipv4Protocol == IPProtocolIPv4)
	}

	// 检测IPv6能力（复用协议检测和缓存）
	if ipInfo.IPv6 != "" && ipInfo.IPv6 != "::" {
		ipv6Protocol := DetectTargetIPProtocol(ipInfo.IPv6)
		hasIPv6 = (ipv6Protocol == IPProtocolIPv6)
	}

	// 合并协议能力
	if hasIPv4 && hasIPv6 {
		return IPProtocolBoth
	} else if hasIPv4 {
		return IPProtocolIPv4
	} else if hasIPv6 {
		return IPProtocolIPv6
	}
	return IPProtocolUnknown
}

// GetClientIPCapabilitiesWithCache 获取客户端IP协议能力（带缓存）
func GetClientIPCapabilitiesWithCache(clientUUID string, getClientIPInfoFunc func(string) ClientIPInfo) IPProtocol {
	// 构造客户端协议缓存key
	cacheKey := "client:" + clientUUID

	// 先尝试从缓存获取
	if protocol, hit := ipProtocolCache.getProtocolResult(cacheKey); hit {
		// log.Printf("客户端协议缓存命中: %s -> %v", clientUUID, protocol)
		return protocol
	}

	// log.Printf("客户端协议缓存未命中，开始查询: %s", clientUUID)

	// 缓存未命中，调用函数获取IP信息
	ipInfo := getClientIPInfoFunc(clientUUID)

	// 计算IP协议能力
	protocol := getClientIPCapabilities(ipInfo)

	// 缓存结果到统一的协议缓存中
	ipProtocolCache.setProtocolResult(cacheKey, protocol)
	// log.Printf("客户端协议查询完成: %s -> %v", clientUUID, protocol)

	return protocol
}

// isClientCompatibleWithTarget 检查客户端是否兼容目标IP协议
func isClientCompatibleWithTarget(clientProtocol, targetProtocol IPProtocol) bool {
	if clientProtocol == IPProtocolUnknown || targetProtocol == IPProtocolUnknown {
		return true // 未知情况下允许尝试
	}

	if clientProtocol == IPProtocolBoth {
		return true // 双栈客户端支持所有目标
	}

	if targetProtocol == IPProtocolBoth {
		return true // 双栈目标任何客户端都可以尝试
	}

	return clientProtocol == targetProtocol
}

// PingTaskManager 管理定时器和任务
type PingTaskManager struct {
	mu       sync.Mutex
	tickers  map[int]*time.Ticker
	tasks    map[int][]models.PingTask
	stopChan chan struct{}
	// gRPC任务发送函数，由外部注入
	sendPingTaskFunc    func(clientUUID string, taskID uint32, pingType, target string) error
	getClientsFunc      func() map[string]bool
	getClientIPInfoFunc func(clientUUID string) ClientIPInfo // 新增：获取客户端IP信息的函数
	getClientNameFunc   func(clientUUID string) string       // 新增：获取客户端名称的函数
}

var manager = &PingTaskManager{
	tickers:  make(map[int]*time.Ticker),
	tasks:    make(map[int][]models.PingTask),
	stopChan: make(chan struct{}),
}

// SetGRPCFunctions 设置gRPC相关函数（由grpc包调用）
func SetGRPCFunctions(sendPingTask func(string, uint32, string, string) error, getClients func() map[string]bool, getClientIPInfo func(string) ClientIPInfo, getClientName func(string) string) {
	manager.sendPingTaskFunc = sendPingTask
	manager.getClientsFunc = getClients
	manager.getClientIPInfoFunc = getClientIPInfo
	manager.getClientNameFunc = getClientName
}

// Reload 重载时间表
func (m *PingTaskManager) Reload(pingTasks []models.PingTask) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 停止所有现有定时器
	for _, ticker := range m.tickers {
		ticker.Stop()
	}
	m.tickers = make(map[int]*time.Ticker)
	m.tasks = make(map[int][]models.PingTask)

	// 按Interval分组任务
	taskGroups := make(map[int][]models.PingTask)
	for _, task := range pingTasks {
		taskGroups[task.Interval] = append(taskGroups[task.Interval], task)
	}

	// 为每个唯一的Interval创建定时器
	for interval, tasks := range taskGroups {
		ticker := time.NewTicker(time.Duration(interval) * time.Second)
		m.tickers[interval] = ticker
		m.tasks[interval] = tasks

		go func(ticker *time.Ticker, tasks []models.PingTask) {
			for {
				select {
				case <-ticker.C:
					for _, task := range tasks {
						go executePingTask(task)
					}
				case <-m.stopChan:
					return
				}
			}
		}(ticker, tasks)
	}

	return nil
}

// executePingTask 执行单个PingTask（带IP协议智能过滤）
func executePingTask(task models.PingTask) {
	// 如果必要的函数未设置，跳过
	if manager.sendPingTaskFunc == nil || manager.getClientsFunc == nil {
		return
	}

	// 获取gRPC连接的客户端
	grpcClients := manager.getClientsFunc()

	// 检测目标的IP协议类型
	targetProtocol := DetectTargetIPProtocol(task.Target)
	if targetProtocol == IPProtocolUnknown {
		// log.Printf("无法确定ping目标 %s 的IP协议类型", task.Target)
	}

	compatibleClients := 0
	for _, clientUUID := range task.Clients {
		if !grpcClients[clientUUID] {
			continue // 客户端未连接，跳过
		}

		// 如果有IP信息获取函数，进行协议兼容性检查
		if manager.getClientIPInfoFunc != nil {
			clientProtocol := GetClientIPCapabilitiesWithCache(clientUUID, manager.getClientIPInfoFunc)

			if !isClientCompatibleWithTarget(clientProtocol, targetProtocol) {
				// 获取客户端名称用于日志显示
				clientName := clientUUID
				if manager.getClientNameFunc != nil {
					if name := manager.getClientNameFunc(clientUUID); name != "" {
						clientName = name
					}
				}
				log.Printf("跳过客户端 %s，IP协议不兼容：客户端(%s) vs 目标(%s) [目标: %s]",
					clientName, clientProtocol.String(), targetProtocol.String(), task.Target)
				continue
			}
		}

		// 通过gRPC发送ping任务
		err := manager.sendPingTaskFunc(clientUUID, uint32(task.Id), task.Type, task.Target)
		if err != nil {
			log.Printf("向客户端 %s 发送ping任务失败: %v", clientUUID, err)
			continue
		}
		compatibleClients++
	}

	if compatibleClients == 0 && len(task.Clients) > 0 {
		// log.Printf("警告：ping任务 %d (目标:%s) 没有兼容的在线客户端", task.Id, task.Target)
	}
}

// ReloadPingSchedule 加载或重载时间表
func ReloadPingSchedule(pingTasks []models.PingTask) error {
	return manager.Reload(pingTasks)
}
