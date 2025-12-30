package handlers

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"sandroidx.com/sandroidx_lite/configs"
	"sandroidx.com/sandroidx_lite/models"
	"sandroidx.com/sandroidx_lite/services"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许所有来源（生产环境应该限制）
	},
}

// SandboxAdbHandler Sandbox ADB Shell 处理器
type SandboxAdbHandler struct {
	dockerService  services.DockerService
	sandboxService services.SandboxService
	logService     services.AdbCommandLogService
}

// NewSandboxAdbHandler 创建新的 Sandbox ADB Shell 处理器
func NewSandboxAdbHandler(dockerService services.DockerService, sandboxService services.SandboxService) *SandboxAdbHandler {
	return &SandboxAdbHandler{
		dockerService:  dockerService,
		sandboxService: sandboxService,
		logService:     services.NewAdbCommandLogService(),
	}
}

// RegisterRoutes 注册路由
func (h *SandboxAdbHandler) RegisterRoutes(router *gin.RouterGroup) {
	router.GET("/sandboxes/shell/:id", h.AdbShellWebSocket)
	router.POST("/sandboxes/exec/:id", h.AdbExec)
}

// AdbShellWebSocket ADB Shell WebSocket 连接
// @Summary ADB Shell WebSocket 连接
// @Description 通过 WebSocket 与 Sandbox 进行交互式 ADB Shell 会话
// @Tags sandboxes
// @Param id path string true "Sandbox ID"
// @Router /sandboxes/shell/{id} [get]
func (h *SandboxAdbHandler) AdbShellWebSocket(c *gin.Context) {
	sandboxID := c.Param("id")
	if sandboxID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Sandbox ID 不能为空"})
		return
	}

	// 升级为 WebSocket 连接
	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket 升级失败: %v", err)
		return
	}
	defer ws.Close()

	log.Printf("WebSocket 连接已建立: sandbox=%s", sandboxID)

	// 与 /sandboxes/exec/:id 一致：使用宿主机 adb 通过 ADB Gateway 映射进入设备
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	// 获取 sandbox 信息（用于记录命令日志）
	var sandbox models.Sandbox
	if err := models.DB.First(&sandbox, "id = ?", sandboxID).Error; err != nil {
		msg := fmt.Sprintf("获取 Sandbox 信息失败: %v\r\n", err)
		_ = ws.WriteMessage(websocket.TextMessage, []byte(msg))
		return
	}

	adbDevice, err := h.sandboxService.GetAdbDeviceAddress(ctx, sandboxID)
	if err != nil {
		msg := fmt.Sprintf("获取 ADB 设备失败: %v\r\n", err)
		_ = ws.WriteMessage(websocket.TextMessage, []byte(msg))
		return
	}

	// 获取映射信息（用于记录命令日志）
	mappingID := sandbox.AgentUserMappingID
	if mappingID == "" {
		mappingID = sandbox.AdbMappingID
	}
	var mapping *models.Mapping
	if mappingID != "" {
		var m models.Mapping
		if err := models.DB.First(&m, "id = ?", mappingID).Error; err == nil {
			mapping = &m
		}
	}

	// 明确提示当前使用的连接方式，便于排查（宿主机 adb -s <listen>）
	_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("已选择 ADB 设备: %s\r\n", adbDevice)))

	// 自检：后端运行环境必须能找到 adb 可执行文件
	if _, err := exec.LookPath("adb"); err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("后端运行环境未找到 adb，可执行文件不在 PATH；请在运行后端的机器/容器里安装 adb 或把 adb 加入 PATH。\r\n"))
		return
	}

	// best-effort：确保连接建立
	connectCmd := exec.CommandContext(ctx, "adb", "connect", adbDevice)
	if output, err := connectCmd.CombinedOutput(); err != nil {
		out := strings.TrimSpace(string(output))
		if !strings.Contains(out, "already connected") && !strings.Contains(out, "connected to") {
			log.Printf("[sandboxes/shell] 警告: adb connect 失败(忽略继续): device=%s err=%v out=%s", adbDevice, err, out)
		}
	}

	cmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "shell")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("打开 stdin 失败: %v\r\n", err)))
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("打开 stdout 失败: %v\r\n", err)))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("打开 stderr 失败: %v\r\n", err)))
		return
	}

	if err := cmd.Start(); err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("启动 adb shell 失败: %v\r\n", err)))
		return
	}

	// 双向转发：WS <-> adb shell
	done := make(chan struct{})
	var closeOnce sync.Once
	closeDone := func() {
		closeOnce.Do(func() { close(done) })
	}

	// adb shell -> WS（stdout/stderr）
	pumpOut := func(r io.Reader) {
		buf := make([]byte, 8192)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				if wErr := ws.WriteMessage(websocket.TextMessage, buf[:n]); wErr != nil {
					closeDone()
					return
				}
			}
			if err != nil {
				closeDone()
				return
			}
		}
	}
	go pumpOut(stdout)
	go pumpOut(stderr)

	// WS -> adb shell
	go func() {
		defer closeDone()
		for {
			_, message, err := ws.ReadMessage()
			if err != nil {
				cancel()
				return
			}

			// 记录用户在 shell 中输入的命令
			if len(message) > 0 {
				// 过滤掉控制字符（如 Ctrl+C = 0x03）
				if len(message) == 1 && message[0] == 0x03 {
					// Ctrl+C，跳过记录
				} else {
					// 去除换行符，获取实际命令
					cmdStr := strings.TrimSpace(string(message))
					// 只记录非空命令
					if cmdStr != "" {
					// 异步记录命令日志，不阻塞 shell 输入
					go func(cmd string) {
						if mapping != nil && mappingID != "" {
							// 构建命令日志（shell 中的命令需要加上 "shell " 前缀）
							adbCmd := cmd
							if !strings.HasPrefix(strings.ToLower(cmd), "shell ") {
								adbCmd = "shell " + cmd
							}
							
							// 从配置获取 GatewayID
							gatewayID := ""
							if configs.AppConfig != nil {
								gatewayID = configs.AppConfig.AdbGateway.GatewayConfig.GatewayID
							}
							
							logEntry := &models.AdbCommandLog{
								Time:         time.Now(),
								From:         "127.0.0.1:0", // WebSocket 来源
								To:           adbDevice,
								AdbCommand:   adbCmd,
								ConnectionID: fmt.Sprintf("shell-%s-%d", sandboxID, time.Now().Unix()),
								MappingID:    mappingID,
								ProjectID:    mapping.ProjectID,
								FromID:       mapping.FromID,
								ToID:         sandboxID,
								GatewayID:    gatewayID,
							}
							
							if err := h.logService.SaveCommandLog(logEntry); err != nil {
								log.Printf("[sandboxes/shell] 记录命令日志失败: %v", err)
							}
						}
					}(cmdStr)
					}
				}
			}

			// 终端输入通常是一行命令；如果未带换行，默认补一个，避免 adb shell 卡住等待
			if len(message) > 0 && message[len(message)-1] != '\n' && !(len(message) == 1 && message[0] == 0x03) {
				message = append(message, '\n')
			}

			if _, err := stdin.Write(message); err != nil {
				cancel()
				return
			}
		}
	}()

	<-done
	_ = stdin.Close()
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	log.Printf("WebSocket 连接已关闭: sandbox=%s device=%s", sandboxID, adbDevice)
}

// AdbExecRequest ADB 命令执行请求
type AdbExecRequest struct {
	// command: 兼容原有单条命令写法（不包含 "adb"）
	Command string `json:"command"`
	// commands: 新增批量执行支持，每个元素为一条独立的 adb 子命令（不包含 "adb"）
	Commands []string `json:"commands"`
}

// AdbExecResponse ADB 命令执行响应
type AdbExecResponse struct {
	ExitCode int                `json:"exit_code"`         // 总体退出码（任一命令失败则为对应失败码，否则为 0）
	Output   string             `json:"output"`            // 汇总输出，附带命令标记
	Error    string             `json:"error"`             // 汇总错误信息（存在失败时返回）
	Results  []AdbCommandResult `json:"results,omitempty"` // 分条结果明细
}

// AdbCommandResult 单条命令执行结果
type AdbCommandResult struct {
	Command  string `json:"command"`         // 原始命令（不含 "adb"）
	ExitCode int    `json:"exit_code"`       // 退出码
	Output   string `json:"output"`          // 输出内容
	Error    string `json:"error,omitempty"` // 错误信息
}

// AdbExec 执行 ADB 命令（支持批量）
// @Summary 执行 ADB 命令（支持批量）
// @Description 在 Sandbox 中执行一条或多条 ADB 命令并返回结果
// @Tags sandboxes
// @Accept json
// @Produce json
// @Param id path string true "Sandbox ID"
// @Param request body AdbExecRequest true "ADB 命令"
// @Success 200 {object} AdbExecResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /sandboxes/exec/{id} [post]
func (h *SandboxAdbHandler) AdbExec(c *gin.Context) {
	sandboxID := c.Param("id")
	if sandboxID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Sandbox ID 不能为空"})
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

	// 根据命令数量动态放宽超时时间：每条 30s，至少 30s，避免长串命令过早超时
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
			log.Printf("[sandboxes/exec] 警告: adb connect 失败(忽略继续): device=%s err=%v out=%s", adbDevice, err, out)
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
