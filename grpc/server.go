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

// MonitorServer gRPC监控服务器
type MonitorServer struct {
	proto.UnimplementedMonitorServiceServer

	// 连接管理
	clientStreams map[string]proto.MonitorService_StreamMonitorServer
	streamsMutex  sync.RWMutex

	// 部分数据缓存
	partialReports map[string]*common.Report
	partialMutex   sync.RWMutex

	// 客户端基础信息缓存，避免频繁查数据库
	clientBasicInfo map[string]*models.Client
	basicInfoMutex  sync.RWMutex
}

// NewMonitorServer 创建新的监控服务器实例
func NewMonitorServer() *MonitorServer {
	return &MonitorServer{
		clientStreams:   make(map[string]proto.MonitorService_StreamMonitorServer),
		partialReports:  make(map[string]*common.Report),
		clientBasicInfo: make(map[string]*models.Client),
	}
}

// StreamMonitor 实现双向流式监控通信
func (s *MonitorServer) StreamMonitor(stream proto.MonitorService_StreamMonitorServer) error {
	var clientUUID string
	var authenticated bool

	defer func() {
		if authenticated && clientUUID != "" {
			clientName := s.getClientDisplayName(clientUUID)
			log.Printf("客户端 %s 连接即将断开，开始清理", clientName)
			s.removeClientStream(clientUUID)
			// 发送离线通知
			notification.OfflineNotification(clientUUID)
			log.Printf("客户端 %s 断开连接，清理完成", clientName)
		} else {
			log.Printf("未认证的连接断开")
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

				// 智能处理重复连接：如果已有连接，先清理旧连接
				if s.hasClientStream(clientUUID) {
					clientName := s.getClientDisplayName(clientUUID)
					log.Printf("检测到客户端 %s 重复连接，清理旧连接并接受新连接", clientName)

					// 获取旧连接
					oldStream, exists := s.getClientStream(clientUUID)
					if exists {
						// 向旧连接发送断开通知
						disconnectResponse := &proto.MonitorResponse{
							Message: &proto.MonitorResponse_Error{
								Error: &proto.ErrorResponse{
									Status: "error",
									Error:  "Connection replaced by new client",
								},
							},
						}
						oldStream.Send(disconnectResponse)
					}

					// 强制清理旧连接
					s.removeClientStream(clientUUID)
					log.Printf("已清理客户端 %s 的旧连接", clientName)
				}

				// 保存新的客户端流
				s.setClientStream(clientUUID, stream)

				// 加载并缓存客户端基础信息
				s.loadClientBasicInfo(clientUUID)

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
				clientName := s.getClientDisplayName(clientUUID)
				log.Printf("客户端 %s 认证成功并已连接", clientName)
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

// handleMonitorReport 处理客户端监控报告（支持部分数据合并）
func (s *MonitorServer) handleMonitorReport(clientUUID string, report *proto.MonitorReport) error {
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

	// 获取或创建客户端的部分报告缓存
	s.partialMutex.Lock()
	defer s.partialMutex.Unlock()

	if s.partialReports[clientUUID] == nil {
		s.partialReports[clientUUID] = &common.Report{
			UUID:      clientUUID,
			UpdatedAt: time.Now(),
		}
	}

	currentReport := s.partialReports[clientUUID]

	// 根据报告类型合并数据
	reportType := report.Type

	switch reportType {
	case "network_only":
		// 仅更新网络相关数据
		if report.Network != nil {
			currentReport.Network = common.NetworkReport{
				Up:        report.Network.Up,
				Down:      report.Network.Down,
				TotalUp:   report.Network.TotalUp,
				TotalDown: report.Network.TotalDown,
			}
		}
		if report.Connections != nil {
			currentReport.Connections = common.ConnectionsReport{
				TCP: int(report.Connections.Tcp),
				UDP: int(report.Connections.Udp),
			}
		}
	case "general_only":
		// 更新常规监控数据
		if report.Cpu != nil {
			currentReport.CPU = common.CPUReport{
				Name:  report.Cpu.Name,
				Cores: int(report.Cpu.Cores),
				Arch:  report.Cpu.Arch,
				Usage: report.Cpu.Usage,
			}
		}

		// 获取客户端基础信息以补充total字段（优先使用缓存）
		var clientInfo models.Client
		if cachedClient, exists := s.getCachedClientBasicInfo(clientUUID); exists {
			clientInfo = *cachedClient
		} else {
			// 缓存未命中，回退到查数据库
			var err error
			clientInfo, err = clients.GetClientBasicInfo(clientUUID)
			if err != nil {
				log.Printf("获取客户端基础信息失败: %v", err)
			}
		}

		if report.Ram != nil {
			currentReport.Ram = common.RamReport{
				Total: clientInfo.MemTotal, // 从基础信息补充
				Used:  report.Ram.Used,
			}
		}
		if report.Swap != nil {
			currentReport.Swap = common.RamReport{
				Total: clientInfo.SwapTotal, // 从基础信息补充
				Used:  report.Swap.Used,
			}
		}
		if report.Load != nil {
			currentReport.Load = common.LoadReport{
				Load1:  report.Load.Load1,
				Load5:  report.Load.Load5,
				Load15: report.Load.Load15,
			}
		}
		if report.Disk != nil {
			currentReport.Disk = common.DiskReport{
				Total: clientInfo.DiskTotal, // 从基础信息补充
				Used:  report.Disk.Used,
			}
		}
		currentReport.Uptime = report.Uptime
		currentReport.Process = int(report.Process)
	default:
		// 完整报告，转换所有数据
		fullReport, err := s.convertToCommonReport(clientUUID, report)
		if err != nil {
			return err
		}
		*currentReport = fullReport
	}

	// 更新时间戳和其他通用字段
	currentReport.UpdatedAt = time.Now()
	currentReport.Method = report.Method
	currentReport.Message = report.Message
	if report.UpdatedAt != nil {
		currentReport.UpdatedAt = report.UpdatedAt.AsTime()
	}

	// 保存到缓存和数据库
	if err := s.saveClientReport(clientUUID, *currentReport); err != nil {
		log.Printf("保存客户端报告失败: %v", err)
		return err
	}

	// 更新WebSocket缓存
	ws.SetLatestReport(clientUUID, currentReport)

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

	if _, exists := s.clientStreams[clientUUID]; exists {
		clientName := s.getClientDisplayName(clientUUID)
		delete(s.clientStreams, clientUUID)
		log.Printf("已从连接池移除客户端 %s", clientName)

		// 清理部分报告缓存
		s.partialMutex.Lock()
		if s.partialReports[clientUUID] != nil {
			delete(s.partialReports, clientUUID)
			log.Printf("已清理客户端 %s 的部分报告缓存", clientName)
		}
		s.partialMutex.Unlock()

		// 清理基础信息缓存
		s.basicInfoMutex.Lock()
		if s.clientBasicInfo[clientUUID] != nil {
			delete(s.clientBasicInfo, clientUUID)
		}
		s.basicInfoMutex.Unlock()
	} else {
		clientName := s.getClientDisplayName(clientUUID)
		log.Printf("警告：尝试移除不存在的客户端连接 %s", clientName)
	}
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
		clientName := s.getClientDisplayName(clientUUID)
		return fmt.Errorf("客户端 %s 未连接", clientName)
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

// ForceCleanupClientConnection 强制清理指定客户端的连接
func (s *MonitorServer) ForceCleanupClientConnection(clientUUID string) {
	clientName := s.getClientDisplayName(clientUUID)
	log.Printf("强制清理客户端 %s 的连接", clientName)
	s.removeClientStream(clientUUID)
	// 不发送离线通知，因为这是强制清理
}

// GetConnectionCount 获取当前连接数（用于监控）
func (s *MonitorServer) GetConnectionCount() int {
	s.streamsMutex.RLock()
	defer s.streamsMutex.RUnlock()
	return len(s.clientStreams)
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
	utils.SetGRPCFunctions(SendPingTaskToClient, GetGRPCConnectedClients, GetClientIPInfo, GetClientName)

	ws.SetGRPCConnectedClientsFunc(GetGRPCConnectedClients)

	log.Printf("gRPC服务器启动在端口 %s", port)

	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("gRPC服务器启动失败: %v", err)
		}
	}()

	return grpcServer, nil
}

// loadClientBasicInfo 加载并缓存客户端基础信息
func (s *MonitorServer) loadClientBasicInfo(clientUUID string) {
	client, err := clients.GetClientBasicInfo(clientUUID)
	if err != nil {
		return // 加载失败时不缓存，后续会回退到查数据库
	}

	s.basicInfoMutex.Lock()
	defer s.basicInfoMutex.Unlock()
	s.clientBasicInfo[clientUUID] = &client
}

// getCachedClientBasicInfo 从缓存获取客户端基础信息
func (s *MonitorServer) getCachedClientBasicInfo(clientUUID string) (*models.Client, bool) {
	s.basicInfoMutex.RLock()
	defer s.basicInfoMutex.RUnlock()
	client, exists := s.clientBasicInfo[clientUUID]
	return client, exists
}

// getCachedClientName 从缓存获取客户端名称
func (s *MonitorServer) getCachedClientName(clientUUID string) string {
	if client, exists := s.getCachedClientBasicInfo(clientUUID); exists {
		return client.Name
	}
	return ""
}

// getClientDisplayName 获取用于日志显示的客户端名称
func (s *MonitorServer) getClientDisplayName(clientUUID string) string {
	if name := s.getCachedClientName(clientUUID); name != "" {
		return name
	}
	// 如果没有名称，显示UUID的前8位
	if len(clientUUID) >= 8 {
		return clientUUID[:8]
	}
	return clientUUID
}
