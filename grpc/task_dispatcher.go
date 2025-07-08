package grpc

import (
	"fmt"

	"github.com/komari-monitor/komari/proto"
)

// 全局的监控服务器实例
var globalMonitorServer *MonitorServer

// SetGlobalMonitorServer 设置全局监控服务器实例
func SetGlobalMonitorServer(server *MonitorServer) {
	globalMonitorServer = server
}

// GetGlobalMonitorServer 获取全局监控服务器实例
func GetGlobalMonitorServer() *MonitorServer {
	return globalMonitorServer
}

// SendExecTaskToClient 向客户端发送执行任务
func SendExecTaskToClient(clientUUID, taskID, command string) error {
	if globalMonitorServer == nil {
		return fmt.Errorf("gRPC服务器未初始化")
	}

	task := &proto.TaskRequest{
		Task: &proto.TaskRequest_Exec{
			Exec: &proto.ExecTask{
				TaskId:  taskID,
				Command: command,
				Message: "exec",
			},
		},
	}

	return globalMonitorServer.SendTaskToClient(clientUUID, task)
}

// SendTerminalTaskToClient 向客户端发送终端任务
func SendTerminalTaskToClient(clientUUID, requestID string) error {
	if globalMonitorServer == nil {
		return fmt.Errorf("gRPC服务器未初始化")
	}

	task := &proto.TaskRequest{
		Task: &proto.TaskRequest_Terminal{
			Terminal: &proto.TerminalTask{
				RequestId: requestID,
				Message:   "terminal",
			},
		},
	}

	return globalMonitorServer.SendTaskToClient(clientUUID, task)
}

// SendPingTaskToClient 向客户端发送ping任务
func SendPingTaskToClient(clientUUID string, taskID uint32, pingType, target string) error {
	if globalMonitorServer == nil {
		return fmt.Errorf("gRPC服务器未初始化")
	}

	task := &proto.TaskRequest{
		Task: &proto.TaskRequest_Ping{
			Ping: &proto.PingTask{
				PingTaskId: taskID,
				PingType:   pingType,
				PingTarget: target,
				Message:    "ping",
			},
		},
	}

	return globalMonitorServer.SendTaskToClient(clientUUID, task)
}

// GetGRPCConnectedClients 获取通过gRPC连接的客户端列表
func GetGRPCConnectedClients() map[string]bool {
	if globalMonitorServer == nil {
		return make(map[string]bool)
	}

	clients := globalMonitorServer.GetConnectedClients()
	result := make(map[string]bool)
	for _, uuid := range clients {
		result[uuid] = true
	}
	return result
}

// IsClientConnectedViaGRPC 检查客户端是否通过gRPC连接
func IsClientConnectedViaGRPC(clientUUID string) bool {
	if globalMonitorServer == nil {
		return false
	}

	_, exists := globalMonitorServer.getClientStream(clientUUID)
	return exists
}
