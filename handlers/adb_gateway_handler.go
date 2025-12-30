package handlers

import (
	"context"
	"net/http"
	"time"

	"sandroidx.com/sandroidx_lite/clients"
	"sandroidx.com/sandroidx_lite/services"

	"github.com/gin-gonic/gin"
)

// AdbGatewayHandler ADB Gateway 处理器
type AdbGatewayHandler struct {
	service services.AdbGatewayService
}

// NewAdbGatewayHandler 创建新的 ADB Gateway 处理器
func NewAdbGatewayHandler(service services.AdbGatewayService) *AdbGatewayHandler {
	return &AdbGatewayHandler{
		service: service,
	}
}

// ListMappings 查询所有映射
// @Summary 查询所有映射
// @Description 从 ADB Gateway API 查询所有映射
// @Tags adb-gateway
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /adb-gateway/mappings [get]
func (h *AdbGatewayHandler) ListMappings(c *gin.Context) {
	mappings, err := h.service.ListMappings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": mappings})
}

// GetMapping 查询单个映射
// @Summary 查询单个映射
// @Description 从 ADB Gateway API 查询单个映射
// @Tags adb-gateway
// @Produce json
// @Param id path string true "映射 ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /adb-gateway/mappings/{id} [get]
func (h *AdbGatewayHandler) GetMapping(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "映射 ID 不能为空"})
		return
	}

	mapping, err := h.service.GetMapping(id)
	if err != nil {
		if err.Error() == "not found" {
			c.JSON(http.StatusNotFound, gin.H{"error": "映射不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": mapping})
}

// CreateMapping 创建映射
// @Summary 创建映射
// @Description 在 ADB Gateway 中创建新的映射
// @Tags adb-gateway
// @Accept json
// @Produce json
// @Param request body clients.MappingSpec true "映射规格"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Router /adb-gateway/mappings/create [post]
func (h *AdbGatewayHandler) CreateMapping(c *gin.Context) {
	var spec clients.MappingSpec
	if err := c.ShouldBindJSON(&spec); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	mapping, err := h.service.CreateMapping(spec)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": mapping})
}

// UpdateMapping 更新映射
// @Summary 更新映射
// @Description 更新 ADB Gateway 中的映射
// @Tags adb-gateway
// @Accept json
// @Produce json
// @Param request body clients.MappingSpec true "映射规格"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Router /adb-gateway/mappings/update [post]
func (h *AdbGatewayHandler) UpdateMapping(c *gin.Context) {
	var spec clients.MappingSpec
	if err := c.ShouldBindJSON(&spec); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	mapping, err := h.service.UpdateMapping(spec)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": mapping})
}

// RemoveMapping 删除映射
// @Summary 删除映射
// @Description 从 ADB Gateway 中删除映射
// @Tags adb-gateway
// @Accept json
// @Produce json
// @Param request body object true "删除映射请求" example({"id":"mapping_id"})
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Router /adb-gateway/mappings/remove [post]
func (h *AdbGatewayHandler) RemoveMapping(c *gin.Context) {
	var req struct {
		ID string `json:"id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.service.RemoveMapping(req.ID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// GetAdbCommandLogs 查询 ADB 命令日志（从 API）
// @Summary 查询 ADB 命令日志
// @Description 从 ADB Gateway API 查询 ADB 命令日志
// @Tags adb-gateway
// @Produce json
// @Param mapping_id query string true "映射 ID"
// @Param start query string true "开始时间 (RFC3339格式)"
// @Param end query string true "结束时间 (RFC3339格式)"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /adb-gateway/logs/adb-commands [get]
func (h *AdbGatewayHandler) GetAdbCommandLogs(c *gin.Context) {
	mappingID := c.Query("mapping_id")
	if mappingID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "mapping_id 参数不能为空"})
		return
	}

	startStr := c.Query("start")
	endStr := c.Query("end")
	if startStr == "" || endStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start 和 end 参数不能为空"})
		return
	}

	start, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start 时间格式错误，需要 RFC3339 格式"})
		return
	}

	end, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "end 时间格式错误，需要 RFC3339 格式"})
		return
	}

	logs, err := h.service.GetAdbCommandLogs(mappingID, start, end)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": logs})
}

// ListMappingsFromDB 从数据库查询所有映射
// @Summary 从数据库查询所有映射
// @Description 从本地数据库查询所有映射
// @Tags adb-gateway
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /adb-gateway/mappings/db [get]
func (h *AdbGatewayHandler) ListMappingsFromDB(c *gin.Context) {
	mappings, err := h.service.ListMappingsFromDB()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": mappings})
}

// GetMappingFromDB 从数据库查询单个映射
// @Summary 从数据库查询单个映射
// @Description 从本地数据库查询单个映射
// @Tags adb-gateway
// @Produce json
// @Param id path string true "映射 ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /adb-gateway/mappings/db/{id} [get]
func (h *AdbGatewayHandler) GetMappingFromDB(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "映射 ID 不能为空"})
		return
	}

	mapping, err := h.service.GetMappingFromDB(id)
	if err != nil {
		if err.Error() == "映射不存在" {
			c.JSON(http.StatusNotFound, gin.H{"error": "映射不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": mapping})
}

// SyncMappings 手动同步映射
// @Summary 手动同步映射
// @Description 手动同步 ADB Gateway 映射到本地数据库
// @Tags adb-gateway
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /adb-gateway/sync [post]
func (h *AdbGatewayHandler) SyncMappings(c *gin.Context) {
	if err := h.service.SyncMappings(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "同步成功"})
}

// StartPeriodicSync 启动定期同步
// @Summary 启动定期同步
// @Description 启动定期同步 ADB Gateway 映射
// @Tags adb-gateway
// @Accept json
// @Produce json
// @Param request body object true "启动同步请求" example({"interval_seconds":300})
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Router /adb-gateway/sync/start [post]
func (h *AdbGatewayHandler) StartPeriodicSync(c *gin.Context) {
	var req struct {
		IntervalSeconds int `json:"interval_seconds" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	interval := time.Duration(req.IntervalSeconds) * time.Second
	cancel, err := h.service.StartPeriodicSync(interval)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 注意：这里 cancel 函数无法通过 HTTP 返回，实际应用中可能需要存储到某个地方
	_ = cancel

	c.JSON(http.StatusOK, gin.H{
		"status":           "ok",
		"message":          "定期同步已启动",
		"interval_seconds": req.IntervalSeconds,
	})
}

// StopPeriodicSync 停止定期同步
// @Summary 停止定期同步
// @Description 停止定期同步 ADB Gateway 映射
// @Tags adb-gateway
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /adb-gateway/sync/stop [post]
func (h *AdbGatewayHandler) StopPeriodicSync(c *gin.Context) {
	h.service.StopPeriodicSync()
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "定期同步已停止"})
}

// UpdateContainerConfig 更新容器配置
// @Summary 更新容器配置
// @Description 更新 ADB Gateway 容器配置，例如端口映射范围。端口范围格式支持单个端口（如 "8080"）或端口范围（如 "15555-25555"）
// @Tags adb-gateway
// @Accept json
// @Produce json
// @Param request body object true "更新容器配置请求" example({"port_ranges":["8080","15555-25555"]})
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /adb-gateway/container/config [post]
func (h *AdbGatewayHandler) UpdateContainerConfig(c *gin.Context) {
	var req struct {
		PortRanges []string `json:"port_ranges" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if len(req.PortRanges) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "端口范围数组不能为空"})
		return
	}

	ctx := context.Background()
	if err := h.service.UpdateContainerConfig(ctx, req.PortRanges); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":      "ok",
		"message":     "容器配置更新成功",
		"port_ranges": req.PortRanges,
	})
}
