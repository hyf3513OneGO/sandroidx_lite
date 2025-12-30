package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"sandroidx.com/sandroidx_lite/configs"
	"sandroidx.com/sandroidx_lite/models"
	"sandroidx.com/sandroidx_lite/services"
)

// AgentHandler Agent 处理器
type AgentHandler struct {
	agentService services.AgentService
	logService   services.AdbCommandLogService
}

// NewAgentHandler 创建新的 Agent 处理器
func NewAgentHandler(agentService services.AgentService) *AgentHandler {
	return &AgentHandler{
		agentService: agentService,
		logService:   services.NewAdbCommandLogService(),
	}
}

// RegisterRoutes 注册路由
func (h *AgentHandler) RegisterRoutes(router *gin.RouterGroup) {
	agents := router.Group("/agents")
	{
		agents.POST("/create", h.CreateAgent)
		agents.GET("/list", h.ListAgents)
		agents.GET("/detail/:id", h.GetAgent)
		agents.GET("/metrics/:id", h.GetAgentMetrics)
		agents.POST("/start/:id", h.StartAgent)
		agents.POST("/stop/:id", h.StopAgent)
		agents.POST("/delete/:id", h.DeleteAgent)
		agents.POST("/exec/:id", h.ExecCommand)
		agents.POST("/execute-running/:id", h.ExecuteRunning)
		agents.POST("/mark-execution-start/:id", h.MarkExecutionStart)
		agents.POST("/switch-sandbox/:id", h.SwitchSandbox)
		agents.GET("/shell/:id", h.ShellWebSocket)
	}
}

// CreateAgentRequest 创建 Agent 请求
type CreateAgentRequest struct {
	Image                string                   `json:"image" binding:"required"`
	Mounts               []MountSpecRequest       `json:"mounts"`
	RequiredEnvVariables []string                 `json:"required_env_variables"`
	SetupCommands        []CommandRequest         `json:"setup_commands"`
	RunningVariables     []RunningVariableRequest `json:"running_variables"`
	RunningCommands      []CommandRequest         `json:"running_commands"`
	Envs                 map[string]string        `json:"envs"` // 用户提供的环境变量
}

// MountSpecRequest 挂载规格请求
type MountSpecRequest struct {
	Volume        string `json:"volume"`                            // 卷ID，为空则创建新卷
	ContainerPath string `json:"container_path" binding:"required"` // 容器内路径
	ReadOnly      bool   `json:"read_only"`                         // 是否只读
}

// CommandRequest 命令请求
type CommandRequest struct {
	Workdir string `json:"workdir"`
	Run     string `json:"run" binding:"required"`
}

// RunningVariableRequest 运行变量请求
type RunningVariableRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
}

// CreateAgent 创建 Agent
// @Summary 创建 Agent（异步）
// @Description 创建一个新的 Agent 实例。此接口是异步的，会立即返回创建中的Agent（状态为creating），实际容器创建在后台执行。可通过 GET /api/v1/agents/detail/:id 查询创建进度。
// @Tags agents
// @Accept json
// @Produce json
// @Param request body CreateAgentRequest true "创建 Agent 请求"
// @Success 200 {object} models.Agent "立即返回，状态为creating"
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /agents/create [post]
func (h *AgentHandler) CreateAgent(c *gin.Context) {
	var req CreateAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("绑定请求参数失败: %v", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "无效的请求参数: " + err.Error()})
		return
	}

	// 转换为 service 层的结构
	spec := services.AgentCreateSpec{
		Image:                req.Image,
		RequiredEnvVariables: req.RequiredEnvVariables,
		Envs:                 req.Envs,
	}

	// 转换 Mounts
	if req.Mounts != nil {
		for _, m := range req.Mounts {
			spec.Mounts = append(spec.Mounts, services.MountSpec{
				Volume:        m.Volume,
				ContainerPath: m.ContainerPath,
				ReadOnly:      m.ReadOnly,
			})
		}
	}

	// 转换 SetupCommands
	if req.SetupCommands != nil {
		for _, cmd := range req.SetupCommands {
			spec.SetupCommands = append(spec.SetupCommands, models.Command{
				Workdir: cmd.Workdir,
				Run:     cmd.Run,
			})
		}
	}

	// 转换 RunningVariables
	if req.RunningVariables != nil {
		for _, rv := range req.RunningVariables {
			spec.RunningVariables = append(spec.RunningVariables, models.RunningVariable{
				Name:        rv.Name,
				Description: rv.Description,
				Type:        rv.Type,
				Required:    rv.Required,
			})
		}
	}

	// 转换 RunningCommands
	if req.RunningCommands != nil {
		for _, cmd := range req.RunningCommands {
			spec.RunningCommands = append(spec.RunningCommands, models.Command{
				Workdir: cmd.Workdir,
				Run:     cmd.Run,
			})
		}
	}

	agent, err := h.agentService.CreateAgent(c.Request.Context(), spec)
	if err != nil {
		log.Printf("创建 Agent 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "创建 Agent 失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, agent)
}

// GetAgent 获取 Agent
// @Summary 获取 Agent
// @Description 根据 ID 获取 Agent 信息
// @Tags agents
// @Produce json
// @Param id path string true "Agent ID"
// @Success 200 {object} models.Agent
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /agents/detail/{id} [get]
func (h *AgentHandler) GetAgent(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Agent ID 不能为空"})
		return
	}

	agent, err := h.agentService.GetAgentWithVolumes(id)
	if err != nil {
		if err.Error() == "Agent 不存在" {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("查询 Agent 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "查询 Agent 失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, agent)
}

// ListAgents 列出所有 Agents
// @Summary 列出所有 Agents
// @Description 获取所有 Agent 列表
// @Tags agents
// @Produce json
// @Success 200 {array} models.Agent
// @Failure 500 {object} ErrorResponse
// @Router /agents/list [get]
func (h *AgentHandler) ListAgents(c *gin.Context) {
	agents, err := h.agentService.ListAgents()
	if err != nil {
		log.Printf("查询 Agents 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "查询 Agents 失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, agents)
}

// DeleteAgent 删除 Agent
// @Summary 删除 Agent
// @Description 根据 ID 删除 Agent，可选择删除哪些Volume
// @Tags agents
// @Produce json
// @Param id path string true "Agent ID"
// @Param volume_ids query string false "要删除的Volume ID列表，逗号分隔，如: volume_xxx,volume_yyy"
// @Success 200 {object} SuccessResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /agents/delete/{id} [post]
func (h *AgentHandler) DeleteAgent(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Agent ID 不能为空"})
		return
	}

	// 获取要删除的Volume ID列表
	volumeIDsStr := c.Query("volume_ids")
	var volumesToDelete []string
	if volumeIDsStr != "" {
		// 用逗号分隔
		volumesToDelete = strings.Split(volumeIDsStr, ",")
		// 去除空白
		filtered := make([]string, 0)
		for _, vid := range volumesToDelete {
			vid = strings.TrimSpace(vid)
			if vid != "" {
				filtered = append(filtered, vid)
			}
		}
		volumesToDelete = filtered
	}

	if err := h.agentService.DeleteAgent(c.Request.Context(), id, volumesToDelete); err != nil {
		if err.Error() == "Agent 不存在" {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("删除 Agent 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "删除 Agent 失败: " + err.Error()})
		return
	}

	message := "Agent 已删除"
	if len(volumesToDelete) > 0 {
		message = fmt.Sprintf("Agent 已删除，尝试删除 %d 个Volume", len(volumesToDelete))
	}
	c.JSON(http.StatusOK, SuccessResponse{Message: message})
}

// StartAgent 启动 Agent
// @Summary 启动 Agent
// @Description 根据 ID 启动 Agent
// @Tags agents
// @Produce json
// @Param id path string true "Agent ID"
// @Success 200 {object} SuccessResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /agents/start/{id} [post]
func (h *AgentHandler) StartAgent(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Agent ID 不能为空"})
		return
	}

	if err := h.agentService.StartAgent(c.Request.Context(), id); err != nil {
		if err.Error() == "Agent 不存在" {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("启动 Agent 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "启动 Agent 失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{Message: "Agent 已启动"})
}

// StopAgent 停止 Agent
// @Summary 停止 Agent
// @Description 根据 ID 停止 Agent
// @Tags agents
// @Produce json
// @Param id path string true "Agent ID"
// @Success 200 {object} SuccessResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /agents/stop/{id} [post]
func (h *AgentHandler) StopAgent(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Agent ID 不能为空"})
		return
	}

	if err := h.agentService.StopAgent(c.Request.Context(), id); err != nil {
		if err.Error() == "Agent 不存在" {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("停止 Agent 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "停止 Agent 失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{Message: "Agent 已停止"})
}

// GetAgentMetrics 获取 Agent 性能指标
// @Summary 获取 Agent 性能指标
// @Tags agents
// @Produce json
// @Param id path string true "Agent ID"
// @Success 200 {object} services.AgentMetrics
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /agents/metrics/{id} [get]
func (h *AgentHandler) GetAgentMetrics(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Agent ID 不能为空"})
		return
	}

	metrics, err := h.agentService.GetAgentMetrics(c.Request.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "不存在") {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("获取性能指标失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "获取性能指标失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, metrics)
}

// SuccessResponse 成功响应
type SuccessResponse struct {
	Message string `json:"message"`
}

// ErrorResponse 错误响应
type ErrorResponse struct {
	Error string `json:"error"`
}

// ExecuteRunningResponse 运行命令的返回结果
type ExecuteRunningResponse struct {
	Results []services.ExecResult `json:"results"`
}

// ExecCommandRequest 执行命令请求
type ExecCommandRequest struct {
	Command    []string `json:"command" binding:"required"` // 要执行的命令
	WorkingDir string   `json:"working_dir"`                // 工作目录
	Env        []string `json:"env"`                        // 环境变量
	Timeout    int      `json:"timeout"`                    // 超时时间（秒），0表示不限制
}

// ExecuteRunningRequest 根据 running_commands 执行的请求
type ExecuteRunningRequest struct {
	Variables map[string]string `json:"variables"`
}

// ExecCommand 在 Agent 中执行命令
// @Summary 在 Agent 中执行命令
// @Description 在指定 Agent 容器中执行命令并返回结果
// @Tags agents
// @Accept json
// @Produce json
// @Param id path string true "Agent ID"
// @Param request body ExecCommandRequest true "执行命令请求"
// @Success 200 {object} services.ExecResult
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /agents/exec/{id} [post]
func (h *AgentHandler) ExecCommand(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Agent ID 不能为空"})
		return
	}

	var req ExecCommandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "无效的请求参数: " + err.Error()})
		return
	}

	// 处理命令：如果只有一个元素且包含空格，自动用 sh -c 包装
	command := req.Command
	if len(command) == 1 && strings.Contains(command[0], " ") {
		// 将单个包含空格的命令转换为 sh -c 格式
		command = []string{"sh", "-c", command[0]}
	}

	// 创建执行配置
	execConfig := &services.ExecConfig{
		Command:      command,
		WorkingDir:   req.WorkingDir,
		Env:          req.Env,
		AttachStdout: true,
		AttachStderr: true,
	}

	// 设置超时
	ctx := c.Request.Context()
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(req.Timeout)*time.Second)
		defer cancel()
	}

	// 执行命令
	result, err := h.agentService.ExecCommand(ctx, id, execConfig)
	if err != nil {
		if strings.Contains(err.Error(), "不存在") {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		if strings.Contains(err.Error(), "没有关联的容器") || strings.Contains(err.Error(), "状态不是运行中") {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("执行命令失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "执行命令失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// ExecuteRunning 执行 Agent 预置的 running_commands（变量替换后依次执行）
// @Summary 执行 Agent 的运行命令
// @Description 根据传入的变量值替换 running_commands 并依次执行
// @Tags agents
// @Accept json
// @Produce json
// @Param id path string true "Agent ID"
// @Param request body ExecuteRunningRequest true "变量值"
// @Success 200 {object} ExecuteRunningResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /agents/execute-running/{id} [post]
func (h *AgentHandler) ExecuteRunning(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Agent ID 不能为空"})
		return
	}

	var req ExecuteRunningRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "无效的请求参数: " + err.Error()})
		return
	}

	agent, err := h.agentService.GetAgent(id)
	if err != nil {
		if strings.Contains(err.Error(), "不存在") {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	if len(agent.RunningCommands) == 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "该 Agent 未配置 running_commands"})
		return
	}

	// 在执行 running_commands 之前，插入一条特殊记录到 adb_command_logs
	// 用于标识这次执行的开始
	if agent.MappingID != "" {
		now := time.Now()
		// 构造提示信息，包含变量信息
		var promptInfo strings.Builder
		promptInfo.WriteString("=== Agent 执行开始 ===")
		if len(req.Variables) > 0 {
			promptInfo.WriteString("\n变量:")
			for key, val := range req.Variables {
				// 只显示前100个字符，避免过长
				displayVal := val
				if len(displayVal) > 100 {
					displayVal = displayVal[:100] + "..."
				}
				promptInfo.WriteString(fmt.Sprintf("\n  %s = %s", key, displayVal))
			}
		}

		// 获取 GatewayID，添加安全检查
		gatewayID := "gw-local" // 默认值
		if configs.AppConfig != nil && configs.AppConfig.AdbGateway.GatewayConfig.GatewayID != "" {
			gatewayID = configs.AppConfig.AdbGateway.GatewayConfig.GatewayID
		}

		executionStartLog := &models.AdbCommandLog{
			Time:         now,
			From:         "agent",
			To:           "sandbox",
			AdbCommand:   promptInfo.String(),
			ConnectionID: fmt.Sprintf("agent-exec-start-%d", now.Unix()),
			MappingID:    agent.MappingID,
			ProjectID:    "", // 可以从 agent 获取，如果有的话
			FromID:       agent.ID,
			ToID:         "", // sandbox_id 会在前端通过 agent.value.sandbox_id 获取
			GatewayID:    gatewayID,
		}

		// 异步保存，不阻塞执行流程
		// 在 goroutine 中捕获 logService 的副本，避免空指针引用
		logService := h.logService
		go func() {
			if logService != nil {
				if err := logService.SaveCommandLog(executionStartLog); err != nil {
					log.Printf("保存 Agent 执行开始标记失败: %v", err)
				}
			} else {
				log.Printf("警告: logService 未初始化，无法保存 Agent 执行开始标记")
			}
		}()
	}

	ctx := c.Request.Context()
	results := make([]services.ExecResult, 0, len(agent.RunningCommands))

	for _, cmd := range agent.RunningCommands {
		run := cmd.Run
		for key, val := range req.Variables {
			if key == "" {
				continue
			}
			run = strings.ReplaceAll(run, key, val)
		}

		execConfig := &services.ExecConfig{
			Command:      []string{"sh", "-c", run},
			WorkingDir:   cmd.Workdir,
			AttachStdout: true,
			AttachStderr: true,
		}

		result, err := h.agentService.ExecCommand(ctx, id, execConfig)
		if err != nil {
			if strings.Contains(err.Error(), "不存在") {
				c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "执行失败: " + err.Error()})
			return
		}

		results = append(results, *result)
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}

// MarkExecutionStartRequest 标记执行开始的请求
type MarkExecutionStartRequest struct {
	Variables map[string]string `json:"variables"`
	SandboxID string            `json:"sandbox_id"` // 可选的 sandbox_id
}

// MarkExecutionStart 只插入执行开始标记，不执行命令
// @Summary 标记 Agent 执行开始
// @Description 在 ADB 命令日志中插入一条执行开始标记，不执行实际命令
// @Tags agents
// @Accept json
// @Produce json
// @Param id path string true "Agent ID"
// @Param request body MarkExecutionStartRequest true "变量值"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Router /agents/mark-execution-start/{id} [post]
func (h *AgentHandler) MarkExecutionStart(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Agent ID 不能为空"})
		return
	}

	var req MarkExecutionStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "无效的请求参数: " + err.Error()})
		return
	}

	agent, err := h.agentService.GetAgent(id)
	if err != nil {
		if strings.Contains(err.Error(), "不存在") {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	// 插入执行开始标记
	if agent.MappingID != "" {
		now := time.Now()
		// 构造提示信息，包含变量信息
		var promptInfo strings.Builder
		promptInfo.WriteString("=== Agent 执行开始 ===")
		if len(req.Variables) > 0 {
			promptInfo.WriteString("\n变量:")
			for key, val := range req.Variables {
				// 只显示前100个字符，避免过长
				displayVal := val
				if len(displayVal) > 100 {
					displayVal = displayVal[:100] + "..."
				}
				promptInfo.WriteString(fmt.Sprintf("\n  %s = %s", key, displayVal))
			}
		}

		// 获取 GatewayID，添加安全检查
		gatewayID := "gw-local" // 默认值
		if configs.AppConfig != nil && configs.AppConfig.AdbGateway.GatewayConfig.GatewayID != "" {
			gatewayID = configs.AppConfig.AdbGateway.GatewayConfig.GatewayID
		}

		executionStartLog := &models.AdbCommandLog{
			Time:         now,
			From:         "agent",
			To:           "sandbox",
			AdbCommand:   promptInfo.String(),
			ConnectionID: fmt.Sprintf("agent-exec-start-%d", now.Unix()),
			MappingID:    agent.MappingID,
			ProjectID:    "",
			FromID:       agent.ID,
			ToID:         req.SandboxID, // 使用前端传入的 sandbox_id
			GatewayID:    gatewayID,
		}

		// 同步保存，确保标记已插入
		if h.logService != nil {
			if err := h.logService.SaveCommandLog(executionStartLog); err != nil {
				log.Printf("保存 Agent 执行开始标记失败: %v", err)
				c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "保存执行开始标记失败: " + err.Error()})
				return
			}
		} else {
			log.Printf("警告: logService 未初始化，无法保存 Agent 执行开始标记")
			// 如果 logService 未初始化，仍然返回成功，但不插入标记
		}
	} else {
		// 如果没有 mapping_id，返回警告但不报错
		log.Printf("警告: Agent %s 没有 mapping_id，无法插入执行开始标记", id)
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "执行开始标记已插入"})
}

// WebSocket 升级器
var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// 允许所有来源，生产环境应该限制
		return true
	},
}

// ShellWebSocket Agent 交互式 Shell WebSocket 接口
// @Summary Agent 交互式 Shell
// @Description 通过 WebSocket 连接到 Agent 的交互式 shell
// @Tags agents
// @Param id path string true "Agent ID"
// @Param shell query string false "Shell 类型 (默认: /bin/sh)"
// @Router /agents/shell/{id} [get]
func (h *AgentHandler) ShellWebSocket(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Agent ID 不能为空"})
		return
	}

	// 获取 shell 参数
	shell := c.Query("shell")
	if shell == "" {
		shell = "/bin/sh"
	}

	// 升级到 WebSocket
	ws, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket 升级失败: %v", err)
		return
	}
	defer ws.Close()

	// 创建 shell 会话
	ctx := context.Background()
	session, err := h.agentService.CreateShellSession(ctx, id, shell)
	if err != nil {
		log.Printf("创建 shell 会话失败: %v", err)
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("错误: %v\r\n", err)))
		return
	}
	defer session.Conn.Close()

	// 启动两个 goroutine 进行双向数据转发
	done := make(chan struct{})
	var closeOnce sync.Once
	closeDone := func() {
		closeOnce.Do(func() {
			close(done)
		})
	}

	// WebSocket -> Container
	go func() {
		defer closeDone()
		for {
			messageType, message, err := ws.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("WebSocket 读取错误: %v", err)
				}
				return
			}

			switch messageType {
			case websocket.TextMessage:
				// 处理特殊控制消息
				if len(message) > 0 && message[0] == '{' {
					var ctrlMsg map[string]interface{}
					if err := json.Unmarshal(message, &ctrlMsg); err == nil {
						if msgType, ok := ctrlMsg["type"].(string); ok {
							switch msgType {
							case "resize":
								// 处理终端大小调整
								if rows, ok := ctrlMsg["rows"].(float64); ok {
									if cols, ok := ctrlMsg["cols"].(float64); ok {
										if session.Resize != nil {
											session.Resize(uint(rows), uint(cols))
										}
									}
								}
								continue
							}
						}
					}
				}
				// 普通输入，写入容器
				if _, err = io.WriteString(session.Conn.(io.Writer), string(message)); err != nil {
					log.Printf("写入容器失败: %v", err)
					return
				}
			case websocket.BinaryMessage:
				// 支持二进制帧输入，直接写入
				if _, err = session.Conn.Write(message); err != nil {
					log.Printf("写入容器失败(二进制): %v", err)
					return
				}
			}
		}
	}()

	// Container -> WebSocket
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := session.Conn.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Printf("读取容器输出失败: %v", err)
				}
				closeDone()
				return
			}

			if n > 0 {
				if err := ws.WriteMessage(websocket.TextMessage, buf[:n]); err != nil {
					log.Printf("写入 WebSocket 失败: %v", err)
					closeDone()
					return
				}
			}
		}
	}()

	// 等待任一方向完成或出错
	<-done
	log.Printf("Shell 会话结束: Agent %s", id)
}

// SwitchSandboxRequest 切换 Sandbox 请求
type SwitchSandboxRequest struct {
	SandboxName string `json:"sandbox_name" binding:"required"`
}

// SwitchSandbox 切换 Agent 连接的 Sandbox
// @Summary 切换 Agent 连接的 Sandbox
// @Description 更新 Agent 的 ADB 映射，将 upstream 指向新的 Sandbox
// @Tags agents
// @Accept json
// @Produce json
// @Param id path string true "Agent ID"
// @Param request body SwitchSandboxRequest true "切换 Sandbox 请求"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /agents/switch-sandbox/{id} [post]
func (h *AgentHandler) SwitchSandbox(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Agent ID 不能为空"})
		return
	}

	var req SwitchSandboxRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "无效的请求参数: " + err.Error()})
		return
	}

	if err := h.agentService.SwitchSandbox(c.Request.Context(), id, req.SandboxName); err != nil {
		if strings.Contains(err.Error(), "不存在") || strings.Contains(err.Error(), "未找到") {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		if strings.Contains(err.Error(), "没有关联") || strings.Contains(err.Error(), "未配置") {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("切换 Sandbox 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "切换 Sandbox 失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{Message: fmt.Sprintf("已成功切换到 Sandbox: %s", req.SandboxName)})
}
