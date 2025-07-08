package ws

import (
	"sync"

	"github.com/gorilla/websocket"
	"github.com/komari-monitor/komari/common"
)

var (
	connectedClients = make(map[string]*SafeConn)
	ConnectedUsers   = []*websocket.Conn{}
	latestReport     = make(map[string]*common.Report)
	mu               = sync.RWMutex{}

	// 添加这行：全局函数指针，用于获取gRPC连接状态，避免循环导入
	GetGRPCConnectedClientsFunc func() map[string]bool
)

func GetConnectedClients() map[string]*SafeConn {
	mu.RLock()
	defer mu.RUnlock()
	clientsCopy := make(map[string]*SafeConn)
	for k, v := range connectedClients {
		clientsCopy[k] = v
	}
	return clientsCopy
}

func SetConnectedClients(uuid string, conn *SafeConn) {
	mu.Lock()
	defer mu.Unlock()
	connectedClients[uuid] = conn
}
func DeleteConnectedClients(uuid string) {
	mu.Lock()
	defer mu.Unlock()
	if conn, exists := connectedClients[uuid]; exists {
		conn.Close()
	}
	delete(connectedClients, uuid)
}
func GetLatestReport() map[string]*common.Report {
	mu.RLock()
	defer mu.RUnlock()
	reportCopy := make(map[string]*common.Report)
	for k, v := range latestReport {
		reportCopy[k] = v
	}
	return reportCopy
}
func SetLatestReport(uuid string, report *common.Report) {
	mu.Lock()
	defer mu.Unlock()
	latestReport[uuid] = report
}
func DeleteLatestReport(uuid string) {
	mu.Lock()
	defer mu.Unlock()
	delete(latestReport, uuid)
}

// SetGRPCConnectedClientsFunc 设置获取gRPC连接状态的函数
func SetGRPCConnectedClientsFunc(fn func() map[string]bool) {
	GetGRPCConnectedClientsFunc = fn
}

// GetOnlineClients 获取在线客户端列表，使用gRPC连接状态
func GetOnlineClients() []string {
	if GetGRPCConnectedClientsFunc != nil {
		grpcClients := GetGRPCConnectedClientsFunc()
		clients := make([]string, 0, len(grpcClients))
		for uuid := range grpcClients {
			clients = append(clients, uuid)
		}
		return clients
	}

	// 如果gRPC函数未设置，返回空列表
	return []string{}
}
