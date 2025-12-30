package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"sandroidx.com/sandroidx_lite/models"
	"sandroidx.com/sandroidx_lite/services"
)

// SandboxHandler Sandbox 处理器
type SandboxHandler struct {
	sandboxService services.SandboxService
}

// NewSandboxHandler 创建新的 Sandbox 处理器
func NewSandboxHandler(sandboxService services.SandboxService) *SandboxHandler {
	return &SandboxHandler{
		sandboxService: sandboxService,
	}
}

// RegisterRoutes 注册路由
func (h *SandboxHandler) RegisterRoutes(router *gin.RouterGroup) {
	sandboxes := router.Group("/sandboxes")
	{
		sandboxes.POST("/create", h.CreateSandbox)
		sandboxes.GET("/list", h.ListSandboxes)
		sandboxes.GET("/detail/:id", h.GetSandbox)
		sandboxes.POST("/start/:id", h.StartSandbox)
		sandboxes.POST("/stop/:id", h.StopSandbox)
		sandboxes.POST("/delete/:id", h.DeleteSandbox)
		sandboxes.POST("/install-apk/:id", h.InstallApk)
	}
}

// ApkConfigRequest APK 配置请求
type ApkConfigRequest struct {
	Name        string `json:"name"`         // APK 名称
	PackageName string `json:"package_name"` // 包名
	Version     string `json:"version"`      // 版本
	URL         string `json:"url"`          // 远程 URL（type=remote 时使用）
	URLStr      string `json:"url_str"`      // 远程 URL（向后兼容，优先使用 url）
	Type        string `json:"type"`         // remote 或 local
}

// CreateSandboxRequest 创建 Sandbox 请求
type CreateSandboxRequest struct {
	Type       string               `json:"type"` // phone/redroid
	Image      string               `json:"image" binding:"required"`
	Mounts     []MountConfigRequest `json:"mounts"`
	Ports      []string             `json:"ports"`
	Privileged bool                 `json:"privileged"`
	Args       []string             `json:"args"`
	// apks: APK 安装配置列表，会在 setup_adb_commands 之前执行安装
	Apks []ApkConfigRequest `json:"apks"`
	// setup_adb_commands: 容器启动后要依次执行的 ADB 子命令列表（不含 "adb" 前缀）
	SetupAdbCommands []string          `json:"setup_adb_commands"`
	Envs             map[string]string `json:"envs"`
}

// MountConfigRequest 挂载配置请求
type MountConfigRequest struct {
	Volume        string `json:"volume"`                            // 卷ID，为空时自动创建新卷
	ContainerPath string `json:"container_path" binding:"required"` // 容器路径
	ReadOnly      bool   `json:"read_only"`                         // 是否只读
}

// CreateSandbox 创建 Sandbox
// @Summary 创建 Sandbox（异步）
// @Description 创建一个新的 Sandbox 实例。此接口是异步的，会立即返回创建中的Sandbox（状态为creating），实际容器创建在后台执行。可通过 GET /api/v1/sandboxes/detail/:id 查询创建进度。
// @Tags sandboxes
// @Accept json
// @Produce json
// @Param request body CreateSandboxRequest true "创建 Sandbox 请求"
// @Success 200 {object} models.Sandbox "立即返回，状态为creating"
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /sandboxes/create [post]
func (h *SandboxHandler) CreateSandbox(c *gin.Context) {
	var req CreateSandboxRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("绑定请求参数失败: %v", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "无效的请求参数: " + err.Error()})
		return
	}

	// 转换为 service 层的结构
	spec := services.SandboxCreateSpec{
		Type:             req.Type,
		Image:            req.Image,
		Ports:            req.Ports,
		Privileged:       req.Privileged,
		Args:             req.Args,
		SetupAdbCommands: req.SetupAdbCommands,
		Envs:             req.Envs,
	}

	// 转换 Mounts
	if req.Mounts != nil {
		for _, m := range req.Mounts {
			spec.Mounts = append(spec.Mounts, models.MountConfig{
				Volume:        m.Volume,
				ContainerPath: m.ContainerPath,
				ReadOnly:      m.ReadOnly,
			})
		}
	}

	// 转换 Apks
	if req.Apks != nil {
		for _, a := range req.Apks {
			spec.Apks = append(spec.Apks, services.ApkConfig{
				Name:        a.Name,
				PackageName: a.PackageName,
				Version:     a.Version,
				URL:         a.URL,
				URLStr:      a.URLStr,
				Type:        a.Type,
			})
		}
	}

	sandbox, err := h.sandboxService.CreateSandbox(c.Request.Context(), spec)
	if err != nil {
		log.Printf("创建 Sandbox 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "创建 Sandbox 失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, sandbox)
}

// GetSandbox 获取 Sandbox
// @Summary 获取 Sandbox
// @Description 根据 ID 获取 Sandbox 信息
// @Tags sandboxes
// @Produce json
// @Param id path string true "Sandbox ID"
// @Success 200 {object} models.Sandbox
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /sandboxes/detail/{id} [get]
func (h *SandboxHandler) GetSandbox(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Sandbox ID 不能为空"})
		return
	}

	sandbox, err := h.sandboxService.GetSandboxWithVolumes(id)
	if err != nil {
		if err.Error() == "sandbox 不存在" {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("查询 Sandbox 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "查询 Sandbox 失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, sandbox)
}

// ListSandboxes 列出所有 Sandboxes
// @Summary 列出所有 Sandboxes
// @Description 获取所有 Sandbox 列表
// @Tags sandboxes
// @Produce json
// @Success 200 {array} models.Sandbox
// @Failure 500 {object} ErrorResponse
// @Router /sandboxes/list [get]
func (h *SandboxHandler) ListSandboxes(c *gin.Context) {
	sandboxes, err := h.sandboxService.ListSandboxes()
	if err != nil {
		log.Printf("查询 Sandboxes 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "查询 Sandboxes 失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, sandboxes)
}

// DeleteSandbox 删除 Sandbox
// @Summary 删除 Sandbox
// @Description 根据 ID 删除 Sandbox，可选择删除哪些Volume
// @Tags sandboxes
// @Produce json
// @Param id path string true "Sandbox ID"
// @Param volume_ids query string false "要删除的Volume ID列表，逗号分隔，如: volume_xxx,volume_yyy"
// @Success 200 {object} SuccessResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /sandboxes/delete/{id} [post]
func (h *SandboxHandler) DeleteSandbox(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Sandbox ID 不能为空"})
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

	if err := h.sandboxService.DeleteSandbox(c.Request.Context(), id, volumesToDelete); err != nil {
		if err.Error() == "sandbox 不存在" {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("删除 Sandbox 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "删除 Sandbox 失败: " + err.Error()})
		return
	}

	message := "Sandbox 已删除"
	if len(volumesToDelete) > 0 {
		message = fmt.Sprintf("Sandbox 已删除，尝试删除 %d 个Volume", len(volumesToDelete))
	}
	c.JSON(http.StatusOK, SuccessResponse{Message: message})
}

// StartSandbox 启动 Sandbox
// @Summary 启动 Sandbox
// @Description 根据 ID 启动 Sandbox
// @Tags sandboxes
// @Produce json
// @Param id path string true "Sandbox ID"
// @Success 200 {object} SuccessResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /sandboxes/start/{id} [post]
func (h *SandboxHandler) StartSandbox(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Sandbox ID 不能为空"})
		return
	}

	if err := h.sandboxService.StartSandbox(c.Request.Context(), id); err != nil {
		if err.Error() == "Sandbox 不存在" {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("启动 Sandbox 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "启动 Sandbox 失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{Message: "Sandbox 已启动"})
}

// StopSandbox 停止 Sandbox
// @Summary 停止 Sandbox
// @Description 根据 ID 停止 Sandbox
// @Tags sandboxes
// @Produce json
// @Param id path string true "Sandbox ID"
// @Success 200 {object} SuccessResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /sandboxes/stop/{id} [post]
func (h *SandboxHandler) StopSandbox(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Sandbox ID 不能为空"})
		return
	}

	if err := h.sandboxService.StopSandbox(c.Request.Context(), id); err != nil {
		if err.Error() == "Sandbox 不存在" {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("停止 Sandbox 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "停止 Sandbox 失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{Message: "Sandbox 已停止"})
}

// InstallApkRequest 安装 APK 请求
type InstallApkRequest struct {
	ApkID string `json:"apk_id" binding:"required"` // APK ID
}

// InstallApk 安装 APK 到 Sandbox
// @Summary 安装 APK 到 Sandbox
// @Description 将指定的 APK 安装到 Sandbox 中
// @Tags sandboxes
// @Accept json
// @Produce json
// @Param id path string true "Sandbox ID"
// @Param request body InstallApkRequest true "安装 APK 请求"
// @Success 200 {object} SuccessResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /sandboxes/install-apk/{id} [post]
func (h *SandboxHandler) InstallApk(c *gin.Context) {
	sandboxID := c.Param("id")
	if sandboxID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Sandbox ID 不能为空"})
		return
	}

	var req InstallApkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("绑定请求参数失败: %v", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "无效的请求参数: " + err.Error()})
		return
	}

	if req.ApkID == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "APK ID 不能为空"})
		return
	}

	if err := h.sandboxService.InstallApk(c.Request.Context(), sandboxID, req.ApkID); err != nil {
		log.Printf("安装 APK 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "安装 APK 失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{Message: "APK 安装成功"})
}
