package handlers

import (
	"net/http"

	"sandroidx.com/sandroidx_lite/configs"
	"sandroidx.com/sandroidx_lite/services"

	"github.com/gin-gonic/gin"
)

type SystemSettingHandler struct {
	settingService *services.SystemSettingService
}

func NewSystemSettingHandler(settingService *services.SystemSettingService) *SystemSettingHandler {
	return &SystemSettingHandler{
		settingService: settingService,
	}
}

type UpdateSystemSettingRequest struct {
	AllowRegistration *bool `json:"allow_registration"`
	AllowSandboxStart *bool `json:"allow_sandbox_start"`
	MaintenanceMode   *bool `json:"maintenance_mode"`
}

func (h *SystemSettingHandler) GetSettings(c *gin.Context) {
	settings, err := h.settingService.GetSettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	// 添加上传配置信息
	uploadConfig := map[string]interface{}{
		"timeout_seconds": configs.AppConfig.Upload.TimeoutSeconds,
		"max_size_bytes":  configs.AppConfig.Upload.MaxSizeBytes,
	}
	
	response := map[string]interface{}{
		"settings":      settings,
		"upload_config": uploadConfig,
	}
	
	c.JSON(http.StatusOK, gin.H{"data": response})
}

func (h *SystemSettingHandler) UpdateSettings(c *gin.Context) {
	var req UpdateSystemSettingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updated, err := h.settingService.UpdateSettings(services.RuntimeSettingsUpdate{
		AllowRegistration: req.AllowRegistration,
		AllowSandboxStart: req.AllowSandboxStart,
		MaintenanceMode:   req.MaintenanceMode,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": updated})
}
