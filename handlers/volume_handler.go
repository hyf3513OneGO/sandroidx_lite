package handlers

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"sandroidx.com/sandroidx_lite/models"
	"sandroidx.com/sandroidx_lite/services"
)

// VolumeHandler Volume 处理器
type VolumeHandler struct {
	volumeService services.VolumeService
}

// NewVolumeHandler 创建新的 Volume 处理器
func NewVolumeHandler(volumeService services.VolumeService) *VolumeHandler {
	return &VolumeHandler{
		volumeService: volumeService,
	}
}

// RegisterRoutes 注册路由
func (h *VolumeHandler) RegisterRoutes(router *gin.RouterGroup) {
	volumes := router.Group("/volumes")
	{
		volumes.POST("/create", h.CreateVolume)
		volumes.GET("/list", h.ListVolumes)
		volumes.GET("/detail/:id", h.GetVolume)
		volumes.POST("/delete/:id", h.DeleteVolume)
	}
}

// CreateVolumeRequest 创建 Volume 请求
type CreateVolumeRequest struct {
	Description string `json:"description"`
}

// CreateVolume 创建 Volume
// @Summary 创建 Volume
// @Description 创建一个新的用户 Volume
// @Tags volumes
// @Accept json
// @Produce json
// @Param request body CreateVolumeRequest true "创建 Volume 请求"
// @Success 200 {object} models.Volume
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /volumes/create [post]
func (h *VolumeHandler) CreateVolume(c *gin.Context) {
	var req CreateVolumeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "无效的请求参数: " + err.Error()})
		return
	}

	volume, err := h.volumeService.CreateVolume(req.Description)
	if err != nil {
		log.Printf("创建 Volume 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "创建 Volume 失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, volume)
}

// VolumeDetailResponse Volume 详情响应
type VolumeDetailResponse struct {
	*models.Volume
	Usage     []services.VolumeUsageInfo `json:"usage"`      // 使用情况
	SizeBytes int64                      `json:"size_bytes"` // 大小（字节）
	SizeMB    float64                    `json:"size_mb"`    // 大小（MB）
	SizeGB    float64                    `json:"size_gb"`    // 大小（GB）
}

// GetVolume 获取 Volume
// @Summary 获取 Volume
// @Description 根据 ID 获取 Volume 信息，包括使用情况和大小
// @Tags volumes
// @Produce json
// @Param id path string true "Volume ID"
// @Success 200 {object} VolumeDetailResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /volumes/detail/{id} [get]
func (h *VolumeHandler) GetVolume(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Volume ID 不能为空"})
		return
	}

	// 获取 Volume 基本信息
	volume, err := h.volumeService.GetVolume(id)
	if err != nil {
		if err.Error() == "Volume 不存在" {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("查询 Volume 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "查询 Volume 失败: " + err.Error()})
		return
	}

	// 获取使用情况
	usage, err := h.volumeService.GetVolumeUsage(id)
	if err != nil {
		log.Printf("查询 Volume 使用情况失败: %v", err)
		// 使用情况获取失败不影响主流程，返回空数组
		usage = []services.VolumeUsageInfo{}
	}

	// 计算大小
	size, err := h.volumeService.CalculateVolumeSize(id)
	if err != nil {
		log.Printf("计算 Volume 大小失败: %v", err)
		// 大小计算失败不影响主流程，使用数据库中的值
		size = volume.SizeBytes
	}

	// 构建响应
	response := VolumeDetailResponse{
		Volume:    volume,
		Usage:     usage,
		SizeBytes: size,
		SizeMB:    float64(size) / 1024 / 1024,
		SizeGB:    float64(size) / 1024 / 1024 / 1024,
	}

	c.JSON(http.StatusOK, response)
}

// ListVolumes 列出所有 Volumes
// @Summary 列出所有 Volumes
// @Description 获取所有 Volume 列表，可按类型过滤
// @Tags volumes
// @Produce json
// @Param type query string false "Volume 类型 (user/system)，为空则返回所有"
// @Success 200 {array} models.Volume
// @Failure 500 {object} ErrorResponse
// @Router /volumes/list [get]
func (h *VolumeHandler) ListVolumes(c *gin.Context) {
	volumeType := c.Query("type")

	volumes, err := h.volumeService.ListVolumes(volumeType)
	if err != nil {
		log.Printf("查询 Volumes 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "查询 Volumes 失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, volumes)
}

// DeleteVolume 删除 Volume
// @Summary 删除 Volume
// @Description 根据 ID 删除 Volume
// @Tags volumes
// @Produce json
// @Param id path string true "Volume ID"
// @Param force query boolean false "是否强制删除（即使有 Agent 在使用）"
// @Success 200 {object} SuccessResponse
// @Failure 404 {object} ErrorResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /volumes/delete/{id} [post]
func (h *VolumeHandler) DeleteVolume(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Volume ID 不能为空"})
		return
	}

	// 获取 force 参数
	forceStr := c.Query("force")
	force := false
	if forceStr != "" {
		var err error
		force, err = strconv.ParseBool(forceStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: "无效的 force 参数"})
			return
		}
	}

	if err := h.volumeService.DeleteVolume(id, force); err != nil {
		if err.Error() == "Volume 不存在" {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		if err.Error() == "不能删除系统卷" {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("删除 Volume 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "删除 Volume 失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{Message: "Volume 已删除"})
}
