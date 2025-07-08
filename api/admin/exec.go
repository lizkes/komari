package admin

import (
	"github.com/gin-gonic/gin"
	"github.com/komari-monitor/komari/api"
	"github.com/komari-monitor/komari/database/logOperation"
	"github.com/komari-monitor/komari/database/tasks"
	komari_grpc "github.com/komari-monitor/komari/grpc"
	"github.com/komari-monitor/komari/utils"
)

// 接受数据类型：
// - command: string
// - clients: []string (客户端 UUID 列表)
func Exec(c *gin.Context) {
	var req struct {
		Command string   `json:"command" binding:"required"`
		Clients []string `json:"clients" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespondError(c, 400, "Invalid or missing request body: "+err.Error())
		return
	}

	// 获取gRPC连接的客户端
	grpcClients := komari_grpc.GetGRPCConnectedClients()
	var onlineClients []string

	// 检查请求的客户端是否在线
	for _, uuid := range req.Clients {
		if grpcClients[uuid] {
			onlineClients = append(onlineClients, uuid)
		} else {
			api.RespondError(c, 400, "Client not connected: "+uuid)
			return
		}
	}

	if len(onlineClients) == 0 {
		api.RespondError(c, 400, "No clients connected")
		return
	}

	// 创建任务
	taskId := utils.GenerateRandomString(16)
	if err := tasks.CreateTask(taskId, onlineClients, req.Command); err != nil {
		api.RespondError(c, 500, "Failed to create task: "+err.Error())
		return
	}

	// 通过gRPC发送任务到客户端
	for _, uuid := range onlineClients {
		err := komari_grpc.SendExecTaskToClient(uuid, taskId, req.Command)
		if err != nil {
			api.RespondError(c, 500, "Failed to send task to client "+uuid+": "+err.Error())
			return
		}
	}

	// 记录操作日志
	uuid, _ := c.Get("uuid")
	logOperation.Log(c.ClientIP(), uuid.(string), "REC, task id: "+taskId, "warn")

	api.RespondSuccess(c, gin.H{
		"task_id": taskId,
		"clients": onlineClients,
	})
}

func contain(clients []string, uuid string) bool {
	for _, client := range clients {
		if client == uuid {
			return true
		}
	}
	return false
}
