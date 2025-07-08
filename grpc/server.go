package grpc

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"

	"github.com/komari-monitor/komari/api"
	"github.com/komari-monitor/komari/common"
	"github.com/komari-monitor/komari/database/clients"
	"github.com/komari-monitor/komari/database/models"
	"github.com/komari-monitor/komari/database/tasks"
	"github.com/komari-monitor/komari/proto"
	"github.com/komari-monitor/komari/utils"
	"github.com/komari-monitor/komari/utils/notification"
	"github.com/komari-monitor/komari/ws"
	"github.com/patrickmn/go-cache"
)

// MonitorServer 实现 MonitorService 接口
type MonitorServer struct {
	proto.UnimplementedMonitorServiceServer

	// 连接管理
	clientStreams map[string]proto.MonitorService_StreamMonitorServer
	streamsMutex  sync.RWMutex
}

// NewMonitorServer 创建新的监控服务器
func NewMonitorServer() *MonitorServer {
	return &MonitorServer{
		clientStreams: make(map[string]proto.MonitorService_StreamMonitorServer),
	}
}

// StreamMonitor 实现双向流式监控通信
func (s *MonitorServer) StreamMonitor(stream proto.MonitorService_StreamMonitorServer) error {
	var clientUUID string
	var authenticated bool

	defer func() {
		if authenticated && clientUUID != "" {
			s.removeClientStream(clientUUID)
			// 发送离线通知
			notification.OfflineNotification(clientUUID)
			log.Printf("客户端 %s 断开连接", clientUUID)
		}
	}()

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			log.Println("客户端关闭连接")
			return nil
		}
		if err != nil {
			log.Printf("接收消息错误: %v", err)
			return err
		}

		switch msg := req.Message.(type) {
		case *proto.MonitorRequest_Auth:
			// 处理认证
			uuid, success, errMsg := s.authenticateClient(msg.Auth.Token)
			if success {
				clientUUID = uuid
				authenticated = true

				// 检查是否已有连接
				if s.hasClientStream(clientUUID) {
					response := &proto.MonitorResponse{
						Message: &proto.MonitorResponse_Error{
							Error: &proto.ErrorResponse{
								Status: "error",
								Error:  "Token already in use",
							},
						},
					}
					stream.Send(response)
					return fmt.Errorf("客户端 %s 重复连接", clientUUID)
				}

				// 保存客户端流
				s.setClientStream(clientUUID, stream)

				// 发送认证成功响应
				response := &proto.MonitorResponse{
					Message: &proto.MonitorResponse_AuthResponse{
						AuthResponse: &proto.AuthResponse{
							Success: true,
						},
					},
				}
				stream.Send(response)

				// 发送上线通知
				go notification.OnlineNotification(clientUUID)
				log.Printf("客户端 %s 认证成功", clientUUID)
			} else {
				// 发送认证失败响应
				response := &proto.MonitorResponse{
					Message: &proto.MonitorResponse_AuthResponse{
						AuthResponse: &proto.AuthResponse{
							Success:      false,
							ErrorMessage: errMsg,
						},
					},
				}
				stream.Send(response)
				return fmt.Errorf("客户端认证失败: %s", errMsg)
			}

		case *proto.MonitorRequest_Report:
			if !authenticated {
				s.sendError(stream, "未认证")
				continue
			}

			// 处理监控报告
			err := s.handleMonitorReport(clientUUID, msg.Report)
			if err != nil {
				log.Printf("处理监控报告错误: %v", err)
				s.sendError(stream, "处理监控报告失败")
			}

		case *proto.MonitorRequest_PingResult:
			if !authenticated {
				s.sendError(stream, "未认证")
				continue
			}

			// 处理ping结果
			err := s.handlePingResult(clientUUID, msg.PingResult)
			if err != nil {
				log.Printf("处理ping结果错误: %v", err)
				s.sendError(stream, "处理ping结果失败")
			}

		default:
			log.Printf("未知消息类型: %T", msg)
			s.sendError(stream, "未知消息类型")
		}
	}
}

// authenticateClient 认证客户端
func (s *MonitorServer) authenticateClient(token string) (uuid string, success bool, errMsg string) {
	if token == "" {
		return "", false, "Token不能为空"
	}

	uuid, err := clients.GetClientUUIDByToken(token)
	if err != nil {
		return "", false, "无效的token"
	}

	return uuid, true, ""
}

// handleMonitorReport 处理监控报告
func (s *MonitorServer) handleMonitorReport(clientUUID string, report *proto.MonitorReport) error {
	// 转换为common.Report格式
	commonReport, err := s.convertToCommonReport(clientUUID, report)
	if err != nil {
		return err
	}

	// 处理基础信息上报
	if report.Type == "basic_info" && report.Message != "" {
		var basicInfo map[string]interface{}
		if err := json.Unmarshal([]byte(report.Message), &basicInfo); err == nil {
			basicInfo["uuid"] = clientUUID
			if err := clients.SaveClientInfo(basicInfo); err != nil {
				log.Printf("保存客户端基础信息失败: %v", err)
			}
		}
	}

	// 保存报告到缓存
	err = s.saveClientReport(clientUUID, commonReport)
	if err != nil {
		return err
	}

	// 更新最新报告
	ws.SetLatestReport(clientUUID, &commonReport)

	return nil
}

// handlePingResult 处理ping结果
func (s *MonitorServer) handlePingResult(clientUUID string, result *proto.PingResult) error {
	pingRecord := models.PingRecord{
		Client: clientUUID,
		TaskId: uint(result.TaskId),
		Value:  int(result.Value),
		Time:   models.FromTime(result.FinishedAt.AsTime()),
	}

	return tasks.SavePingRecord(pingRecord)
}

// convertToCommonReport 转换proto报告为common报告
func (s *MonitorServer) convertToCommonReport(clientUUID string, report *proto.MonitorReport) (common.Report, error) {
	commonReport := common.Report{
		UUID:      clientUUID,
		UpdatedAt: time.Now(),
		Method:    report.Method,
		Message:   report.Message,
	}

	if report.UpdatedAt != nil {
		commonReport.UpdatedAt = report.UpdatedAt.AsTime()
	}

	// 转换CPU信息
	if report.Cpu != nil {
		commonReport.CPU = common.CPUReport{
			Name:  report.Cpu.Name,
			Cores: int(report.Cpu.Cores),
			Arch:  report.Cpu.Arch,
			Usage: report.Cpu.Usage,
		}
	}

	// 转换内存信息
	if report.Ram != nil {
		commonReport.Ram = common.RamReport{
			Total: report.Ram.Total,
			Used:  report.Ram.Used,
		}
	}

	// 转换交换分区信息
	if report.Swap != nil {
		commonReport.Swap = common.RamReport{
			Total: report.Swap.Total,
			Used:  report.Swap.Used,
		}
	}

	// 转换负载信息
	if report.Load != nil {
		commonReport.Load = common.LoadReport{
			Load1:  report.Load.Load1,
			Load5:  report.Load.Load5,
			Load15: report.Load.Load15,
		}
	}

	// 转换磁盘信息
	if report.Disk != nil {
		commonReport.Disk = common.DiskReport{
			Total: report.Disk.Total,
			Used:  report.Disk.Used,
		}
	}

	// 转换网络信息
	if report.Network != nil {
		commonReport.Network = common.NetworkReport{
			Up:        report.Network.Up,
			Down:      report.Network.Down,
			TotalUp:   report.Network.TotalUp,
			TotalDown: report.Network.TotalDown,
		}
	}

	// 转换连接信息
	if report.Connections != nil {
		commonReport.Connections = common.ConnectionsReport{
			TCP: int(report.Connections.Tcp),
			UDP: int(report.Connections.Udp),
		}
	}

	// 其他字段
	commonReport.Uptime = report.Uptime
	commonReport.Process = int(report.Process)

	return commonReport, nil
}

// saveClientReport 保存客户端报告
func (s *MonitorServer) saveClientReport(uuid string, report common.Report) error {
	reports, _ := api.Records.Get(uuid)
	if reports == nil {
		reports = []common.Report{}
	}
	if report.CPU.Usage < 0.01 {
		report.CPU.Usage = 0.01
	}
	reports = append(reports.([]common.Report), report)
	api.Records.Set(uuid, reports, cache.DefaultExpiration)

	return nil
}

// sendError 发送错误消息
func (s *MonitorServer) sendError(stream proto.MonitorService_StreamMonitorServer, errMsg string) {
	response := &proto.MonitorResponse{
		Message: &proto.MonitorResponse_Error{
			Error: &proto.ErrorResponse{
				Status: "error",
				Error:  errMsg,
			},
		},
	}
	stream.Send(response)
}

// 连接管理方法
func (s *MonitorServer) setClientStream(clientUUID string, stream proto.MonitorService_StreamMonitorServer) {
	s.streamsMutex.Lock()
	defer s.streamsMutex.Unlock()
	s.clientStreams[clientUUID] = stream
}

func (s *MonitorServer) removeClientStream(clientUUID string) {
	s.streamsMutex.Lock()
	defer s.streamsMutex.Unlock()
	delete(s.clientStreams, clientUUID)
}

func (s *MonitorServer) hasClientStream(clientUUID string) bool {
	s.streamsMutex.RLock()
	defer s.streamsMutex.RUnlock()
	_, exists := s.clientStreams[clientUUID]
	return exists
}

func (s *MonitorServer) getClientStream(clientUUID string) (proto.MonitorService_StreamMonitorServer, bool) {
	s.streamsMutex.RLock()
	defer s.streamsMutex.RUnlock()
	stream, exists := s.clientStreams[clientUUID]
	return stream, exists
}

// SendTaskToClient 向指定客户端发送任务
func (s *MonitorServer) SendTaskToClient(clientUUID string, task *proto.TaskRequest) error {
	stream, exists := s.getClientStream(clientUUID)
	if !exists {
		return fmt.Errorf("客户端 %s 未连接", clientUUID)
	}

	response := &proto.MonitorResponse{
		Message: &proto.MonitorResponse_TaskRequest{
			TaskRequest: task,
		},
	}

	return stream.Send(response)
}

// GetConnectedClients 获取已连接的客户端列表
func (s *MonitorServer) GetConnectedClients() []string {
	s.streamsMutex.RLock()
	defer s.streamsMutex.RUnlock()

	clients := make([]string, 0, len(s.clientStreams))
	for uuid := range s.clientStreams {
		clients = append(clients, uuid)
	}
	return clients
}

// StartGRPCServer 启动gRPC服务器
func StartGRPCServer(port string) (*grpc.Server, error) {
	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return nil, fmt.Errorf("监听端口失败: %v", err)
	}

	grpcServer := grpc.NewServer()
	monitorServer := NewMonitorServer()

	proto.RegisterMonitorServiceServer(grpcServer, monitorServer)

	// 设置全局服务器实例
	SetGlobalMonitorServer(monitorServer)

	// 为ping调度注入gRPC函数
	utils.SetGRPCFunctions(SendPingTaskToClient, GetGRPCConnectedClients)

	log.Printf("gRPC服务器启动在端口 %s", port)

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("gRPC服务器启动失败: %v", err)
		}
	}()

	return grpcServer, nil
}
