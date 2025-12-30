package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
	"sandroidx.com/sandroidx_lite/models"
	"sandroidx.com/sandroidx_lite/services"
)

var shareScrcpyUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// AgentShareHandler Agent 分享（对外只读）处理器
type AgentShareHandler struct {
	shareService   services.AgentShareService
	agentService   services.AgentService
	scrcpy         services.ScrcpyService
	sandboxService services.SandboxService
}

func NewAgentShareHandler(shareService services.AgentShareService, agentService services.AgentService, scrcpy services.ScrcpyService, sandboxService services.SandboxService) *AgentShareHandler {
	return &AgentShareHandler{
		shareService:   shareService,
		agentService:   agentService,
		scrcpy:         scrcpy,
		sandboxService: sandboxService,
	}
}

// RegisterRoutes 注册路由
// - authRequired: 需要登录才能创建分享
// - public: 公共分享访问（无需登录）
func (h *AgentShareHandler) RegisterRoutes(authRequired *gin.RouterGroup, public *gin.RouterGroup) {
	// 创建分享（受控）
	authRequired.POST("/agents/share/:id", h.CreateShare)
	// 分享管理（受控）
	authRequired.GET("/agents/share/summary", h.ShareSummary)
	authRequired.GET("/agents/share/list", h.ListShares)
	authRequired.GET("/agents/:id/shares", h.ListSharesByAgent)
	authRequired.DELETE("/agents/share/:token", h.RevokeShare)

	// 公共访问
	public.GET("/share/agents/:token", h.GetSharedAgent)
	public.POST("/share/agents/:token/execute-running", h.ExecuteSharedRunning)
	public.GET("/share/agents/:token/execute-running/ws", h.ExecuteSharedRunningWS)
	public.GET("/share/agents/:token/shell", h.ShareShellWebSocketReadonly)
	public.GET("/share/agents/:token/scrcpy", h.ShareScrcpyWebSocket)
	public.GET("/share/agents/:token/scrcpy/resolution", h.ShareScrcpyResolution)
	public.GET("/share/agents/:token/scrcpy/status", h.ShareScrcpyStatus)
	public.POST("/share/agents/:token/exec", h.ShareAdbExec)
}

type CreateShareRequest struct {
	// TTLHours <= 0 表示不过期（不建议）
	TTLHours int `json:"ttl_hours"`
}

// ShareSummary 返回每个 Agent 的分享数量（只统计未过期的分享）
func (h *AgentShareHandler) ShareSummary(c *gin.Context) {
	type row struct {
		AgentID string `json:"agent_id"`
		Cnt     int    `json:"cnt"`
	}
	now := time.Now()
	var rows []row
	if err := models.DB.
		Model(&models.AgentShare{}).
		Select("agent_id, count(*) as cnt").
		Where("expires_at IS NULL OR expires_at > ?", now).
		Group("agent_id").
		Scan(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	m := make(map[string]int, len(rows))
	for _, r := range rows {
		if r.AgentID != "" {
			m[r.AgentID] = r.Cnt
		}
	}
	c.JSON(http.StatusOK, gin.H{"summary": m})
}

// ListShares 列出所有分享（包含过期状态）
func (h *AgentShareHandler) ListShares(c *gin.Context) {
	var shares []models.AgentShare
	if err := models.DB.Order("created_at DESC").Find(&shares).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	now := time.Now()
	out := make([]gin.H, 0, len(shares))
	for _, s := range shares {
		expired := false
		if s.ExpiresAt != nil && now.After(*s.ExpiresAt) {
			expired = true
		}
		out = append(out, gin.H{
			"token":      s.Token,
			"agent_id":   s.AgentID,
			"expires_at": s.ExpiresAt,
			"created_at": s.CreatedAt,
			"expired":    expired,
			"share_path": fmt.Sprintf("/share/agents/%s", s.Token),
		})
	}
	c.JSON(http.StatusOK, gin.H{"shares": out})
}

// ListSharesByAgent 列出指定 Agent 的分享链接记录
func (h *AgentShareHandler) ListSharesByAgent(c *gin.Context) {
	agentID := c.Param("id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Agent ID 不能为空"})
		return
	}
	var shares []models.AgentShare
	if err := models.DB.Where("agent_id = ?", agentID).Order("created_at DESC").Find(&shares).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	now := time.Now()
	out := make([]gin.H, 0, len(shares))
	for _, s := range shares {
		expired := false
		if s.ExpiresAt != nil && now.After(*s.ExpiresAt) {
			expired = true
		}
		out = append(out, gin.H{
			"token":      s.Token,
			"agent_id":   s.AgentID,
			"expires_at": s.ExpiresAt,
			"created_at": s.CreatedAt,
			"expired":    expired,
			"share_path": fmt.Sprintf("/share/agents/%s", s.Token),
		})
	}
	c.JSON(http.StatusOK, gin.H{"shares": out})
}

// RevokeShare 撤销一个分享 token
func (h *AgentShareHandler) RevokeShare(c *gin.Context) {
	token := c.Param("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "token 不能为空"})
		return
	}
	if err := models.DB.Delete(&models.AgentShare{}, "token = ?", token).Error; err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, SuccessResponse{Message: "已撤销分享"})
}

// CreateShare 创建一个分享 token（需登录）
// @Tags share
// @Accept json
// @Produce json
// @Param id path string true "Agent ID"
// @Param request body CreateShareRequest false "创建分享参数"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /agents/share/{id} [post]
func (h *AgentShareHandler) CreateShare(c *gin.Context) {
	agentID := c.Param("id")
	if agentID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Agent ID 不能为空"})
		return
	}

	// 确保 agent 存在
	if _, err := h.agentService.GetAgent(agentID); err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
		return
	}

	var req CreateShareRequest
	_ = c.ShouldBindJSON(&req) // 可选参数

	ttl := time.Duration(req.TTLHours) * time.Hour
	if req.TTLHours == 0 {
		ttl = 7 * 24 * time.Hour
	}

	share, err := h.shareService.Create(c.Request.Context(), agentID, ttl)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	// 生成相对链接，前端用 window.location.origin 拼接
	path := fmt.Sprintf("/share/agents/%s", share.Token)
	c.JSON(http.StatusOK, gin.H{
		"token":      share.Token,
		"agent_id":   share.AgentID,
		"expires_at": share.ExpiresAt,
		"share_path": path,
	})
}

// GetSharedAgent 返回对外分享的 Agent 信息（只读）
func (h *AgentShareHandler) GetSharedAgent(c *gin.Context) {
	token := c.Param("token")
	share, err := h.shareService.GetValid(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "分享不存在或已失效"})
		return
	}

	agent, err := h.agentService.GetAgent(share.AgentID)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Agent 不存在或已删除"})
		return
	}

	// 只返回必要字段，避免泄露 env 等敏感信息
	c.JSON(http.StatusOK, gin.H{
		"share": gin.H{
			"token":      share.Token,
			"agent_id":   share.AgentID,
			"expires_at": share.ExpiresAt,
		},
		"agent": gin.H{
			"id":                agent.ID,
			"image":             agent.Image,
			"status":            agent.Status,
			"running_variables": agent.RunningVariables,
			"running_commands":  agent.RunningCommands,
			// 注意：sandbox_id 不直接暴露（scrcpy/shell 走 token 绑定）
		},
	})
}

type ShareExecuteRunningRequest struct {
	Variables map[string]string `json:"variables"`
}

// ExecuteSharedRunning 通过分享 token 执行 Agent 的 running_commands（仅允许变量替换）
// 安全：对变量值做 shell 级转义，防止注入；不接受自定义命令列表。
func (h *AgentShareHandler) ExecuteSharedRunning(c *gin.Context) {
	token := c.Param("token")
	share, err := h.shareService.GetValid(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "分享不存在或已失效"})
		return
	}

	var req ShareExecuteRunningRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "无效的请求参数: " + err.Error()})
		return
	}

	agent, err := h.agentService.GetAgent(share.AgentID)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Agent 不存在或已删除"})
		return
	}
	if len(agent.RunningCommands) == 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "该 Agent 未配置 running_commands"})
		return
	}

	ctx := c.Request.Context()
	results := make([]services.ExecResult, 0, len(agent.RunningCommands))

	for _, cmd := range agent.RunningCommands {
		run := cmd.Run
		for key, val := range req.Variables {
			if key == "" {
				continue
			}
			// 核心：对变量值做 shell 安全转义，避免注入
			run = strings.ReplaceAll(run, key, shellEscapeSingleQuoted(val))
		}

		execConfig := &services.ExecConfig{
			Command:      []string{"sh", "-c", run},
			WorkingDir:   cmd.Workdir,
			AttachStdout: true,
			AttachStderr: true,
		}

		result, err := h.agentService.ExecCommand(ctx, agent.ID, execConfig)
		if err != nil {
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "执行失败: " + err.Error()})
			return
		}
		results = append(results, *result)
	}

	c.JSON(http.StatusOK, gin.H{"results": results})
}

// shellEscapeSingleQuoted 将任意字符串变成可安全放进 sh 命令里的单引号字符串字面量
// 例: abc'def => 'abc'"'"'def'
func shellEscapeSingleQuoted(s string) string {
	str := s
	// 禁止 NUL（会截断 C 字符串），其余保持原样
	str = strings.ReplaceAll(str, "\x00", "")
	parts := strings.Split(str, "'")
	if len(parts) == 1 {
		return "'" + str + "'"
	}
	// 用 '"'"' 作为拼接手法
	return "'" + strings.Join(parts, `'"'"'`) + "'"
}

// ExecuteSharedRunningWS 流式执行 running_commands，并通过 WebSocket 实时推送输出
// 协议：
// - 客户端连接后发送一条 JSON：{"variables":{...}}
// - 服务端推送：
//   - {"type":"start","index":1,"total":N,"cmd":"..."}
//   - {"type":"output","index":1,"data":"..."}
//   - {"type":"exit","index":1,"exit_code":0}
//   - {"type":"done"}
func (h *AgentShareHandler) ExecuteSharedRunningWS(c *gin.Context) {
	token := c.Param("token")
	share, err := h.shareService.GetValid(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "分享不存在或已失效"})
		return
	}

	agent, err := h.agentService.GetAgent(share.AgentID)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Agent 不存在或已删除"})
		return
	}
	if len(agent.RunningCommands) == 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "该 Agent 未配置 running_commands"})
		return
	}

	ws, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[share-exec-ws] WebSocket 升级失败: %v", err)
		return
	}
	defer ws.Close()

	// 读取首条消息（变量）
	_, msg, err := ws.ReadMessage()
	if err != nil {
		return
	}
	var req ShareExecuteRunningRequest
	if err := json.Unmarshal(msg, &req); err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","message":"invalid request"}`))
		return
	}

	ctx := c.Request.Context()
	total := len(agent.RunningCommands)

	for i, cmd := range agent.RunningCommands {
		index := i + 1
		run := cmd.Run
		for key, val := range req.Variables {
			if key == "" {
				continue
			}
			run = strings.ReplaceAll(run, key, shellEscapeSingleQuoted(val))
		}

		preview := run
		if cmd.Workdir != "" {
			preview = fmt.Sprintf("cd %s && %s", cmd.Workdir, run)
		}
		startMsg, _ := json.Marshal(gin.H{
			"type":  "start",
			"index": index,
			"total": total,
			"cmd":   preview,
		})
		if err := ws.WriteMessage(websocket.TextMessage, startMsg); err != nil {
			return
		}

		// 使用 exec：TTY=true，避免 docker multiplex 解析复杂度（stdout/stderr 合并）
		execCfg := &services.ExecConfig{
			Command:      []string{"sh", "-c", run},
			WorkingDir:   cmd.Workdir,
			AttachStdout: true,
			AttachStderr: true,
			Tty:          true,
		}

		execID, err := h.agentService.CreateExec(ctx, agent.ID, execCfg)
		if err != nil {
			_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"type":"error","message":"create exec failed: %s"}`, err.Error())))
			return
		}

		stream, err := h.agentService.StartExec(ctx, execID, execCfg)
		if err != nil {
			_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"type":"error","message":"start exec failed: %s"}`, err.Error())))
			return
		}

		buf := make([]byte, 4096)
		for {
			n, rerr := stream.Read(buf)
			if n > 0 {
				outMsg, _ := json.Marshal(gin.H{
					"type":  "output",
					"index": index,
					"data":  string(buf[:n]),
				})
				if err := ws.WriteMessage(websocket.TextMessage, outMsg); err != nil {
					_ = stream.Close()
					return
				}
			}
			if rerr != nil {
				_ = stream.Close()
				break
			}
		}

		// 等待退出码
		exitCode := 0
		for {
			inspect, ierr := h.agentService.InspectExec(ctx, execID)
			if ierr != nil {
				exitCode = -1
				break
			}
			if !inspect.Running {
				exitCode = inspect.ExitCode
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(150 * time.Millisecond):
			}
		}

		exitMsg, _ := json.Marshal(gin.H{
			"type":      "exit",
			"index":     index,
			"exit_code": exitCode,
		})
		if err := ws.WriteMessage(websocket.TextMessage, exitMsg); err != nil {
			return
		}
	}

	_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"done"}`))
}

// ShareScrcpyResolution 通过分享 token 获取设备分辨率
func (h *AgentShareHandler) ShareScrcpyResolution(c *gin.Context) {
	token := c.Param("token")
	share, err := h.shareService.GetValid(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "分享不存在或已失效"})
		return
	}

	agent, err := h.agentService.GetAgent(share.AgentID)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Agent 不存在或已删除"})
		return
	}

	sandboxID, err := h.resolveSandboxID(c.Request.Context(), agent)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	width, height, err := h.scrcpy.GetDeviceResolution(c.Request.Context(), sandboxID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: fmt.Sprintf("获取设备分辨率失败: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"width": width, "height": height})
}

// ShareScrcpyStatus 返回 scrcpy 会话状态（只读）
func (h *AgentShareHandler) ShareScrcpyStatus(c *gin.Context) {
	token := c.Param("token")
	share, err := h.shareService.GetValid(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "分享不存在或已失效"})
		return
	}
	agent, err := h.agentService.GetAgent(share.AgentID)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Agent 不存在或已删除"})
		return
	}
	sandboxID, err := h.resolveSandboxID(c.Request.Context(), agent)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	session, exists := h.scrcpy.GetSession(sandboxID)
	if !exists {
		c.JSON(http.StatusOK, gin.H{"active": false, "sandbox_id": sandboxID, "subscribers": 0})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"active":      true,
		"session_id":  session.ID,
		"sandbox_id":  sandboxID,
		"created_at":  session.CreatedAt,
		"subscribers": session.SubscriberCount(),
	})
}

// ShareScrcpyWebSocket 通过分享 token 订阅 scrcpy 视频流（只读）
func (h *AgentShareHandler) ShareScrcpyWebSocket(c *gin.Context) {
	token := c.Param("token")
	share, err := h.shareService.GetValid(c.Request.Context(), token)
	if err != nil {
		// ws 升级前返回 json
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "分享不存在或已失效"})
		return
	}

	agent, err := h.agentService.GetAgent(share.AgentID)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Agent 不存在或已删除"})
		return
	}

	sandboxID, err := h.resolveSandboxID(c.Request.Context(), agent)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	ws, err := shareScrcpyUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[share-scrcpy] WebSocket 升级失败: %v", err)
		return
	}
	defer ws.Close()

	log.Printf("[share-scrcpy] WebSocket 连接已建立: token=%s sandbox=%s", token, sandboxID)

	// 检查或启动 scrcpy 会话（复用 Sandbox handler 逻辑）
	session, exists := h.scrcpy.GetSession(sandboxID)
	if !exists {
		session, err = h.scrcpy.StartScrcpySession(c.Request.Context(), sandboxID)
		if err != nil {
			_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"type":"error","message":"启动 scrcpy 失败: %v"}`, err)))
			return
		}
	}

	subscriberID := uuid.New().String()
	videoChan := session.AddSubscriber(subscriberID)
	defer func() {
		session.RemoveSubscriber(subscriberID)
	}()

	initMsg := fmt.Sprintf(`{"type":"ready","session_id":"%s","sandbox_id":"%s","reconnected":%t}`,
		session.ID, session.SandboxID, exists)
	if err := ws.WriteMessage(websocket.TextMessage, []byte(initMsg)); err != nil {
		return
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, _, err := ws.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-done:
			return
		case data, ok := <-videoChan:
			if !ok {
				_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","message":"视频流已断开，scrcpy 会话已结束"}`))
				return
			}
			if err := ws.WriteMessage(websocket.BinaryMessage, data); err != nil {
				return
			}
		}
	}
}

// ShareShellWebSocketReadonly 通过分享 token 连接 Agent 的只读 Shell（禁止写入）
func (h *AgentShareHandler) ShareShellWebSocketReadonly(c *gin.Context) {
	token := c.Param("token")
	share, err := h.shareService.GetValid(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "分享不存在或已失效"})
		return
	}

	// shell 参数（仍允许指定，但 share 端强制只读）
	shell := c.Query("shell")
	if shell == "" {
		shell = "/bin/sh"
	}

	ws, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[share-shell] WebSocket 升级失败: %v", err)
		return
	}
	defer ws.Close()

	ctx := context.Background()
	session, err := h.agentService.CreateShellSession(ctx, share.AgentID, shell)
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("错误: %v\r\n", err)))
		return
	}
	defer session.Conn.Close()

	done := make(chan struct{})
	var closeOnce sync.Once
	closeDone := func() {
		closeOnce.Do(func() { close(done) })
	}

	// WebSocket -> Container（只读：读到任何数据都丢弃；如有非空则主动关闭，防止绕过前端）
	go func() {
		defer closeDone()
		for {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if len(msg) > 0 {
				// 有输入尝试：直接断开
				_ = ws.WriteControl(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "readonly"),
					time.Now().Add(2*time.Second))
				return
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
					log.Printf("[share-shell] 读取容器输出失败: %v", err)
				}
				closeDone()
				return
			}
			if n > 0 {
				if err := ws.WriteMessage(websocket.TextMessage, buf[:n]); err != nil {
					closeDone()
					return
				}
			}
		}
	}()

	<-done
}

func (h *AgentShareHandler) resolveSandboxID(ctx context.Context, agent *models.Agent) (string, error) {
	if agent == nil {
		return "", fmt.Errorf("agent 为空")
	}
	if agent.MappingID == "" {
		return "", fmt.Errorf("该 Agent 未绑定 ADB 映射，无法定位 Sandbox")
	}

	// 1) 优先使用本地 DB 的 mappings.to_id
	// 注意：ADB Gateway 的 to_id 语义不保证等于本系统的 sandbox.id，可能是网关生成的任意标识。
	// 因此这里必须做一次“是否真有这个 sandbox”的校验，不通过则回退到 upstream 推导。
	var m models.Mapping
	if err := models.DB.WithContext(ctx).First(&m, "id = ?", agent.MappingID).Error; err == nil {
		if m.ToID != "" {
			// 1.1) 若 to_id 正好就是 sandbox.id
			var sb models.Sandbox
			if err := models.DB.WithContext(ctx).First(&sb, "id = ?", m.ToID).Error; err == nil && sb.ID != "" {
				return sb.ID, nil
			}
			// 1.2) 某些网关会把 to_id 设成 upstream 的 name（容器名），尝试按 container_name 反查
			if err := models.DB.WithContext(ctx).First(&sb, "container_name = ?", m.ToID).Error; err == nil && sb.ID != "" {
				return sb.ID, nil
			}
		}
		// 2) 若 ToID 为空，尝试从 upstream（name:5555）推导 container_name
		if m.Upstream != "" {
			name := strings.TrimSpace(strings.SplitN(m.Upstream, ":", 2)[0])
			if name != "" {
				var sb models.Sandbox
				if err := models.DB.WithContext(ctx).First(&sb, "container_name = ?", name).Error; err == nil {
					if sb.ID != "" {
						return sb.ID, nil
					}
				}
			}
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}

	return "", fmt.Errorf("无法定位该 Agent 对应的 Sandbox（映射未同步或未绑定）")
}

// ShareAdbExec 通过分享 token 执行 ADB 命令（仅用于解锁等安全操作）
func (h *AgentShareHandler) ShareAdbExec(c *gin.Context) {
	token := c.Param("token")
	share, err := h.shareService.GetValid(c.Request.Context(), token)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "分享不存在或已失效"})
		return
	}

	agent, err := h.agentService.GetAgent(share.AgentID)
	if err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "Agent 不存在或已删除"})
		return
	}

	sandboxID, err := h.resolveSandboxID(c.Request.Context(), agent)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	var req AdbExecRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "无效的请求参数: " + err.Error()})
		return
	}

	// 汇总命令列表：优先使用 commands，兼容单条 command
	commands := make([]string, 0, len(req.Commands)+1)
	if strings.TrimSpace(req.Command) != "" {
		commands = append(commands, strings.TrimSpace(req.Command))
	}
	for _, cmd := range req.Commands {
		if trimmed := strings.TrimSpace(cmd); trimmed != "" {
			commands = append(commands, trimmed)
		}
	}

	if len(commands) == 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "命令不能为空"})
		return
	}

	// 安全限制：只允许解锁相关的命令
	allowedPrefixes := []string{
		"shell input keyevent KEYCODE_WAKEUP",
		"shell wm dismiss-keyguard",
		"shell input keyevent KEYCODE_MENU",
		"shell input swipe",
	}
	for _, cmd := range commands {
		allowed := false
		for _, prefix := range allowedPrefixes {
			if strings.HasPrefix(cmd, prefix) {
				allowed = true
				break
			}
		}
		if !allowed {
			c.JSON(http.StatusForbidden, ErrorResponse{Error: fmt.Sprintf("分享模式下不允许执行该命令: %s", cmd)})
			return
		}
	}

	// 根据命令数量动态放宽超时时间：每条 30s，至少 30s
	timeout := time.Duration(len(commands)) * 30 * time.Second
	if timeout < 30*time.Second {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()

	adbDevice, err := h.sandboxService.GetAdbDeviceAddress(ctx, sandboxID)
	if err != nil {
		log.Printf("获取 ADB 设备地址失败: %v", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: fmt.Sprintf("获取 ADB 设备失败: %v", err)})
		return
	}

	// 确保 ADB 连接建立
	connectCmd := exec.CommandContext(ctx, "adb", "connect", adbDevice)
	if output, err := connectCmd.CombinedOutput(); err != nil {
		out := strings.TrimSpace(string(output))
		// best-effort：某些 serial（尤其是本地/已存在的映射）不需要 connect
		if !strings.Contains(out, "already connected") && !strings.Contains(out, "connected to") {
			log.Printf("[share-exec] 警告: adb connect 失败(忽略继续): device=%s err=%v out=%s", adbDevice, err, out)
		}
	}

	// 逐条执行命令，汇总结果
	results := make([]AdbCommandResult, 0, len(commands))
	var outputBuilder strings.Builder
	overallExit := 0
	for idx, cmdStr := range commands {
		cmdParts := strings.Fields(cmdStr)
		if len(cmdParts) == 0 {
			result := AdbCommandResult{
				Command:  cmdStr,
				ExitCode: -1,
				Error:    "命令为空，已跳过",
			}
			results = append(results, result)
			outputBuilder.WriteString(fmt.Sprintf("#%d $ <empty>\n命令为空，已跳过\n", idx+1))
			overallExit = -1
			continue
		}

		adbArgs := append([]string{"-s", adbDevice}, cmdParts...)
		cmd := exec.CommandContext(ctx, "adb", adbArgs...)

		output, err := cmd.CombinedOutput()
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
			overallExit = exitCode
		}

		result := AdbCommandResult{
			Command:  cmdStr,
			ExitCode: exitCode,
			Output:   string(output),
		}
		if err != nil {
			result.Error = fmt.Sprintf("命令执行失败: %v", err)
		}
		results = append(results, result)

		outputBuilder.WriteString(fmt.Sprintf("#%d $ adb %s\n", idx+1, strings.Join(cmdParts, " ")))
		outputBuilder.WriteString(string(output))
		if len(output) == 0 || output[len(output)-1] != '\n' {
			outputBuilder.WriteString("\n")
		}
		if result.Error != "" {
			outputBuilder.WriteString(result.Error + "\n")
		}
	}

	response := AdbExecResponse{
		ExitCode: overallExit,
		Output:   outputBuilder.String(),
		Results:  results,
	}

	for _, r := range results {
		if r.Error != "" {
			response.Error = "部分命令执行失败"
			break
		}
	}

	c.JSON(http.StatusOK, response)
}
