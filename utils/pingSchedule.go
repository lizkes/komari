package utils

import (
	"sync"
	"time"

	"github.com/komari-monitor/komari/database/models"
)

// PingTaskManager 管理定时器和任务
type PingTaskManager struct {
	mu       sync.Mutex
	tickers  map[int]*time.Ticker
	tasks    map[int][]models.PingTask
	stopChan chan struct{}
	// gRPC任务发送函数，由外部注入
	sendPingTaskFunc func(clientUUID string, taskID uint32, pingType, target string) error
	getClientsFunc   func() map[string]bool
}

var manager = &PingTaskManager{
	tickers:  make(map[int]*time.Ticker),
	tasks:    make(map[int][]models.PingTask),
	stopChan: make(chan struct{}),
}

// SetGRPCFunctions 设置gRPC相关函数（由grpc包调用）
func SetGRPCFunctions(sendPingTask func(string, uint32, string, string) error, getClients func() map[string]bool) {
	manager.sendPingTaskFunc = sendPingTask
	manager.getClientsFunc = getClients
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

// executePingTask 执行单个PingTask
func executePingTask(task models.PingTask) {
	// 如果gRPC函数未设置，跳过
	if manager.sendPingTaskFunc == nil || manager.getClientsFunc == nil {
		return
	}

	// 获取gRPC连接的客户端
	grpcClients := manager.getClientsFunc()

	for _, clientUUID := range task.Clients {
		if grpcClients[clientUUID] {
			// 通过gRPC发送ping任务
			err := manager.sendPingTaskFunc(clientUUID, uint32(task.Id), task.Type, task.Target)
			if err != nil {
				// 静默失败，继续处理其他客户端
				continue
			}
		}
	}
}

// ReloadPingSchedule 加载或重载时间表
func ReloadPingSchedule(pingTasks []models.PingTask) error {
	return manager.Reload(pingTasks)
}
