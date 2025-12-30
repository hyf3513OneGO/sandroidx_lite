package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"sandroidx.com/sandroidx_lite/configs"
	"sandroidx.com/sandroidx_lite/models"
	"sandroidx.com/sandroidx_lite/services"

	"github.com/gin-gonic/gin"
)

// AdbCommandLogHandler ADB 命令日志处理器
type AdbCommandLogHandler struct {
	logService services.AdbCommandLogService
}

// NewAdbCommandLogHandler 创建新的 ADB 命令日志处理器
func NewAdbCommandLogHandler() *AdbCommandLogHandler {
	return &AdbCommandLogHandler{
		logService: services.NewAdbCommandLogService(),
	}
}

// UploadCommandLogRequest ADB Gateway 上传的请求体
type UploadCommandLogRequest struct {
	Time         string `json:"time" binding:"required"`
	From         string `json:"from" binding:"required"`
	To           string `json:"to" binding:"required"`
	AdbCommand   string `json:"adb_command" binding:"required"`
	ConnectionID string `json:"connection_id" binding:"required"`
	MappingID    string `json:"mapping_id" binding:"required"`
	ProjectID    string `json:"project_id"`
	FromID       string `json:"from_id"`
	ToID         string `json:"to_id"`
	GatewayID    string `json:"gateway_id"`
}

// AuthMiddleware 鉴权中间件（用于 ADB Gateway 上传接口）
func (h *AdbCommandLogHandler) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取 Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少 Authorization header"})
			c.Abort()
			return
		}

		// 检查 Bearer token 格式
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 Authorization 格式，需要 Bearer token"})
			c.Abort()
			return
		}

		token := parts[1]

		// 验证 token
		expectedToken := configs.AppConfig.AdbGateway.UploadToken
		if expectedToken == "" {
			// 如果配置中没有设置 token，允许所有请求（向后兼容）
			c.Next()
			return
		}

		if token != expectedToken {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "无效的 token"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// getMappingType 获取映射类型：system, agent, 或 sandbox
// 通过查询数据库中的映射名称来判断
func getMappingType(mappingID string) string {
	var mapping models.Mapping
	if err := models.DB.First(&mapping, "id = ?", mappingID).Error; err != nil {
		// 如果查询失败，尝试通过名称模式判断
		return "unknown"
	}
	
	name := strings.ToLower(mapping.Name)
	
	// System mapping: sandbox-{id}-system
	if strings.Contains(name, "-system") {
		return "system"
	}
	
	// Agent mapping: 通过 FromID 判断（Agent 的映射 FromID 是 agent ID）
	if mapping.FromID != "" {
		// 检查是否是 agent 映射（FromID 是 agent ID，且不在 sandbox 映射中）
		if !strings.Contains(name, "sandbox-") {
			return "agent"
		}
	}
	
	// Sandbox mapping: sandbox-{id}-agent-user 或其他 sandbox 相关
	if strings.Contains(name, "sandbox-") {
		return "sandbox"
	}
	
	return "unknown"
}

// shouldLogMapping 根据配置决定是否应该记录该映射的日志
func shouldLogMapping(mappingType string) bool {
	if configs.AppConfig == nil {
		return true // 默认记录
	}
	
	switch mappingType {
	case "system":
		return configs.AppConfig.AdbGateway.LogSystemMapping
	case "agent":
		return configs.AppConfig.AdbGateway.LogAgentMapping
	case "sandbox":
		return configs.AppConfig.AdbGateway.LogSandboxMapping
	default:
		return true // 未知类型默认记录
	}
}

// UploadCommandLog 接收 ADB Gateway 上传的命令日志
func (h *AdbCommandLogHandler) UploadCommandLog(c *gin.Context) {
	var req UploadCommandLogRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("请求参数错误: %v", err)})
		return
	}

	// 解析时间
	timeValue, err := time.Parse(time.RFC3339, req.Time)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("时间格式错误: %v", err)})
		return
	}

	// 获取映射类型并检查是否应该记录
	mappingType := getMappingType(req.MappingID)
	if !shouldLogMapping(mappingType) {
		// 根据配置不记录此类型的映射，直接返回成功（不保存）
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"message": "日志已跳过（根据配置）",
		})
		return
	}

	// 转换为数据库模型
	logEntry := &models.AdbCommandLog{
		Time:         timeValue,
		From:         req.From,
		To:           req.To,
		AdbCommand:   req.AdbCommand,
		ConnectionID: req.ConnectionID,
		MappingID:    req.MappingID,
		ProjectID:    req.ProjectID,
		FromID:       req.FromID,
		ToID:         req.ToID,
		GatewayID:    req.GatewayID,
	}

	// 保存到数据库
	if err := h.logService.SaveCommandLog(logEntry); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("保存日志失败: %v", err)})
		return
	}

	// 返回成功响应
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": "日志保存成功",
	})
}

// GetCommandLogsByMappingID 根据映射 ID 查询日志（支持分页）
func (h *AdbCommandLogHandler) GetCommandLogsByMappingID(c *gin.Context) {
	mappingID := c.Param("id")
	if mappingID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "映射 ID 不能为空"})
		return
	}

	// 解析时间参数
	startStr := c.Query("start")
	endStr := c.Query("end")

	if startStr == "" || endStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 start 或 end 参数"})
		return
	}

	start, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("start 时间格式错误: %v", err)})
		return
	}

	end, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("end 时间格式错误: %v", err)})
		return
	}

	// 解析分页参数
	limit := 100 // 默认每页 100 条
	offset := 0

	if limitStr := c.Query("limit"); limitStr != "" {
		if parsedLimit, err := parseInt(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		if parsedOffset, err := parseInt(offsetStr); err == nil && parsedOffset >= 0 {
			offset = parsedOffset
		}
	}

	logs, total, err := h.logService.GetCommandLogsByMappingID(mappingID, start, end, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":   logs,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// GetCommandLogs 查询日志（支持分页和过滤）
func (h *AdbCommandLogHandler) GetCommandLogs(c *gin.Context) {
	// 解析时间参数
	startStr := c.Query("start")
	endStr := c.Query("end")

	var start, end time.Time
	var err error

	if startStr != "" {
		start, err = time.Parse(time.RFC3339, startStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("start 时间格式错误: %v", err)})
			return
		}
	} else {
		// 默认查询最近 24 小时
		start = time.Now().AddDate(0, 0, -1)
	}

	if endStr != "" {
		end, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("end 时间格式错误: %v", err)})
			return
		}
	} else {
		end = time.Now()
	}

	// 解析分页参数
	limit := 100 // 默认每页 100 条
	offset := 0

	if limitStr := c.Query("limit"); limitStr != "" {
		if parsedLimit, err := parseInt(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		if parsedOffset, err := parseInt(offsetStr); err == nil && parsedOffset >= 0 {
			offset = parsedOffset
		}
	}

	// 可选过滤参数
	mappingID := c.Query("mapping_id")
	projectID := c.Query("project_id")
	gatewayID := c.Query("gateway_id")

	// 使用通用查询方法，支持所有过滤条件和分页
	logs, total, err := h.logService.GetCommandLogsWithFilters(mappingID, projectID, gatewayID, start, end, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":   logs,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// DeleteCommandLogs 批量删除命令日志
func (h *AdbCommandLogHandler) DeleteCommandLogs(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("请求参数错误: %v", err)})
		return
	}
	
	if len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "日志 ID 列表不能为空"})
		return
	}
	
	if err := h.logService.DeleteCommandLogs(req.IDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": fmt.Sprintf("成功删除 %d 条日志", len(req.IDs)),
	})
}

// ClearCommandLogsByMappingID 清空指定映射的所有命令日志
// @Summary 清空映射的命令日志
// @Description 根据映射 ID 清空该映射的所有命令日志
// @Tags adb-command-logs
// @Accept json
// @Produce json
// @Param id path string true "映射 ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /adb-gateway/command-logs/mapping/{id}/clear [post]
func (h *AdbCommandLogHandler) ClearCommandLogsByMappingID(c *gin.Context) {
	mappingID := c.Param("id")
	if mappingID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "映射 ID 不能为空"})
		return
	}
	
	if err := h.logService.ClearCommandLogsByMappingID(mappingID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": "成功清空该映射的所有命令日志",
	})
}

// parseInt 辅助函数：解析整数
func parseInt(s string) (int, error) {
	return strconv.Atoi(s)
}
