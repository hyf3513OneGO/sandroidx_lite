package handlers

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"sandroidx.com/sandroidx_lite/services"
)

var scrcpyUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许所有来源（生产环境应该限制）
	},
}

// SandboxScrcpyHandler Sandbox Scrcpy 处理器
type SandboxScrcpyHandler struct {
	scrcpyService services.ScrcpyService
}

// NewSandboxScrcpyHandler 创建新的 Sandbox Scrcpy 处理器
func NewSandboxScrcpyHandler(scrcpyService services.ScrcpyService) *SandboxScrcpyHandler {
	return &SandboxScrcpyHandler{
		scrcpyService: scrcpyService,
	}
}

// RegisterRoutes 注册路由
func (h *SandboxScrcpyHandler) RegisterRoutes(router *gin.RouterGroup) {
	router.GET("/sandboxes/scrcpy/:id", h.ScrcpyWebSocket)
	router.POST("/sandboxes/scrcpy/:id/start", h.StartScrcpy)
	router.POST("/sandboxes/scrcpy/:id/stop", h.StopScrcpy)
	router.GET("/sandboxes/scrcpy/:id/status", h.GetScrcpyStatus)
	router.GET("/sandboxes/scrcpy/:id/resolution", h.GetScrcpyResolution)
}

// StartScrcpy 启动 Scrcpy 会话
// @Summary 启动 Scrcpy 会话
// @Description 为指定的 Sandbox 启动 scrcpy 屏幕镜像会话
// @Tags sandboxes
// @Produce json
// @Param id path string true "Sandbox ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /sandboxes/scrcpy/{id}/start [post]
func (h *SandboxScrcpyHandler) StartScrcpy(c *gin.Context) {
	sandboxID := c.Param("id")
	if sandboxID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Sandbox ID 不能为空"})
		return
	}

	session, err := h.scrcpyService.StartScrcpySession(c.Request.Context(), sandboxID)
	if err != nil {
		log.Printf("启动 scrcpy 会话失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: fmt.Sprintf("启动 scrcpy 失败: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":        "Scrcpy 会话已启动",
		"session_id":     session.ID,
		"sandbox_id":     session.SandboxID,
		"listen_address": session.ListenAddress,
		"created_at":     session.CreatedAt,
	})
}

// StopScrcpy 停止 Scrcpy 会话
// @Summary 停止 Scrcpy 会话
// @Description 停止指定 Sandbox 的 scrcpy 屏幕镜像会话
// @Tags sandboxes
// @Produce json
// @Param id path string true "Sandbox ID"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /sandboxes/scrcpy/{id}/stop [post]
func (h *SandboxScrcpyHandler) StopScrcpy(c *gin.Context) {
	sandboxID := c.Param("id")
	if sandboxID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Sandbox ID 不能为空"})
		return
	}

	if err := h.scrcpyService.StopScrcpySession(sandboxID); err != nil {
		log.Printf("停止 scrcpy 会话失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: fmt.Sprintf("停止 scrcpy 失败: %v", err)})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{Message: "Scrcpy 会话已停止"})
}

// GetScrcpyStatus 获取 Scrcpy 会话状态
// @Summary 获取 Scrcpy 会话状态
// @Description 获取指定 Sandbox 的 scrcpy 会话状态
// @Tags sandboxes
// @Produce json
// @Param id path string true "Sandbox ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Router /sandboxes/scrcpy/{id}/status [get]
func (h *SandboxScrcpyHandler) GetScrcpyStatus(c *gin.Context) {
	sandboxID := c.Param("id")
	if sandboxID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Sandbox ID 不能为空"})
		return
	}

	session, exists := h.scrcpyService.GetSession(sandboxID)
	if !exists {
		c.JSON(http.StatusOK, gin.H{
			"active":      false,
			"sandbox_id":  sandboxID,
			"subscribers": 0,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"active":         true,
		"session_id":     session.ID,
		"sandbox_id":     session.SandboxID,
		"listen_address": session.ListenAddress,
		"created_at":     session.CreatedAt,
		"subscribers":    session.SubscriberCount(),
	})
}

// GetScrcpyResolution 获取设备分辨率（通过截图解析）
// @Summary 获取设备分辨率
// @Description 基于 ADB 截图解析宽高，供前端按真实分辨率渲染画面
// @Tags sandboxes
// @Produce json
// @Param id path string true "Sandbox ID"
// @Success 200 {object} map[string]int
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /sandboxes/scrcpy/{id}/resolution [get]
func (h *SandboxScrcpyHandler) GetScrcpyResolution(c *gin.Context) {
	sandboxID := c.Param("id")
	if sandboxID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Sandbox ID 不能为空"})
		return
	}

	width, height, err := h.scrcpyService.GetDeviceResolution(c.Request.Context(), sandboxID)
	if err != nil {
		log.Printf("获取设备分辨率失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: fmt.Sprintf("获取设备分辨率失败: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"width":  width,
		"height": height,
	})
}

// ScrcpyWebSocket Scrcpy 视频流 WebSocket 连接
// @Summary Scrcpy 视频流 WebSocket
// @Description 通过 WebSocket 接收 Sandbox 的 scrcpy 视频流（H.264）
// @Tags sandboxes
// @Param id path string true "Sandbox ID"
// @Router /sandboxes/scrcpy/{id} [get]
func (h *SandboxScrcpyHandler) ScrcpyWebSocket(c *gin.Context) {
	sandboxID := c.Param("id")
	if sandboxID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Sandbox ID 不能为空"})
		return
	}

	// 升级为 WebSocket 连接
	ws, err := scrcpyUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[scrcpy-ws] WebSocket 升级失败: %v", err)
		return
	}
	defer ws.Close()

	log.Printf("[scrcpy-ws] WebSocket 连接已建立: sandbox=%s", sandboxID)

	// 检查或启动 scrcpy 会话
	session, exists := h.scrcpyService.GetSession(sandboxID)
	if !exists {
		log.Printf("[scrcpy-ws] Scrcpy 会话不存在，自动启动: %s", sandboxID)

		// 自动启动会话
		session, err = h.scrcpyService.StartScrcpySession(c.Request.Context(), sandboxID)
		if err != nil {
			errMsg := fmt.Sprintf(`{"type":"error","message":"启动 scrcpy 失败: %v"}`, err)
			log.Printf("[scrcpy-ws] 启动 scrcpy 失败: %v", err)
			ws.WriteMessage(websocket.TextMessage, []byte(errMsg))
			return
		}

		log.Printf("[scrcpy-ws] Scrcpy 会话已创建: %s", session.ID)
	} else {
		log.Printf("[scrcpy-ws] 复用现有 scrcpy 会话: %s (订阅者数: %d)", session.ID, session.SubscriberCount())
	}

	// 生成订阅者 ID
	subscriberID := uuid.New().String()

	// 添加订阅者（会自动取消空闲计时器）
	videoChan := session.AddSubscriber(subscriberID)
	defer func() {
		session.RemoveSubscriber(subscriberID)
		log.Printf("[scrcpy-ws] 订阅者已移除: %s", subscriberID)
	}()

	log.Printf("[scrcpy-ws] 订阅者已添加: %s (当前订阅者数: %d)", subscriberID, session.SubscriberCount())

	// 发送初始消息
	initMsg := fmt.Sprintf(`{"type":"ready","session_id":"%s","sandbox_id":"%s","reconnected":%t}`,
		session.ID, session.SandboxID, exists)
	if err := ws.WriteMessage(websocket.TextMessage, []byte(initMsg)); err != nil {
		log.Printf("[scrcpy-ws] 发送初始消息失败: %v", err)
		return
	}

	// 创建通道用于处理 WebSocket 关闭
	done := make(chan struct{})

	// Goroutine: 处理客户端消息（主要用于心跳和关闭检测）
	go func() {
		defer close(done)
		for {
			_, _, err := ws.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("[scrcpy-ws] WebSocket 意外关闭: %v (订阅者: %s)", err, subscriberID)
				} else {
					log.Printf("[scrcpy-ws] WebSocket 正常关闭 (订阅者: %s)", subscriberID)
				}
				return
			}
		}
	}()

	// 主循环: 从视频通道读取数据并发送到 WebSocket
	frameCount := 0
	for {
		select {
		case data, ok := <-videoChan:
			if !ok {
				// 通道已关闭，会话结束或出错
				log.Printf("[scrcpy-ws] 视频通道已关闭: %s", subscriberID)
				// 发送错误消息给客户端
				errMsg := `{"type":"error","message":"视频流已断开，scrcpy 会话已结束"}`
				ws.WriteMessage(websocket.TextMessage, []byte(errMsg))
				// 等待一下再关闭，确保消息能发送
				time.Sleep(100 * time.Millisecond)
				return
			}

			// 发送二进制数据（H.264 视频流）
			if err := ws.WriteMessage(websocket.BinaryMessage, data); err != nil {
				log.Printf("[scrcpy-ws] 发送视频数据失败: %v (订阅者: %s)", err, subscriberID)
				return
			}

			frameCount++
			if frameCount%100 == 0 {
				log.Printf("[scrcpy-ws] 已发送 %d 帧给订阅者 %s", frameCount, subscriberID)
			}

		case <-done:
			// 客户端断开连接
			log.Printf("[scrcpy-ws] 客户端主动断开连接: %s (已发送 %d 帧)", subscriberID, frameCount)
			return
		}
	}
}
