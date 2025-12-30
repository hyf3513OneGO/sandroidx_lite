package services

import (
	"fmt"
	"time"

	"sandroidx.com/sandroidx_lite/models"
)

// AdbCommandLogService ADB 命令日志服务接口
type AdbCommandLogService interface {
	// 保存 ADB 命令日志
	SaveCommandLog(log *models.AdbCommandLog) error

	// 查询日志
	GetCommandLogsByMappingID(mappingID string, start, end time.Time, limit, offset int) ([]models.AdbCommandLog, int64, error)
	GetCommandLogsByProjectID(projectID string, start, end time.Time) ([]models.AdbCommandLog, error)
	GetCommandLogsByGatewayID(gatewayID string, start, end time.Time) ([]models.AdbCommandLog, error)
	GetCommandLogs(start, end time.Time, limit, offset int) ([]models.AdbCommandLog, int64, error)
	// 通用查询方法，支持所有过滤条件和分页
	GetCommandLogsWithFilters(mappingID, projectID, gatewayID string, start, end time.Time, limit, offset int) ([]models.AdbCommandLog, int64, error)
	
	// 删除日志
	DeleteCommandLogs(ids []uint) error
	// 根据映射 ID 清空所有日志
	ClearCommandLogsByMappingID(mappingID string) error
}

// adbCommandLogService ADB 命令日志服务实现
type adbCommandLogService struct{}

// NewAdbCommandLogService 创建新的 ADB 命令日志服务
func NewAdbCommandLogService() AdbCommandLogService {
	return &adbCommandLogService{}
}

// SaveCommandLog 保存 ADB 命令日志
func (s *adbCommandLogService) SaveCommandLog(log *models.AdbCommandLog) error {
	if err := models.DB.Create(log).Error; err != nil {
		return fmt.Errorf("保存 ADB 命令日志失败: %w", err)
	}
	return nil
}

// GetCommandLogsByMappingID 根据映射 ID 查询日志（支持分页）
func (s *adbCommandLogService) GetCommandLogsByMappingID(mappingID string, start, end time.Time, limit, offset int) ([]models.AdbCommandLog, int64, error) {
	var logs []models.AdbCommandLog
	var total int64

	query := models.DB.Model(&models.AdbCommandLog{}).Where("mapping_id = ? AND time >= ? AND time <= ?", mappingID, start, end)

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("查询日志总数失败: %w", err)
	}

	// 获取数据（支持分页）
	if err := query.Order("time DESC").
		Limit(limit).
		Offset(offset).
		Find(&logs).Error; err != nil {
		return nil, 0, fmt.Errorf("查询日志失败: %w", err)
	}

	return logs, total, nil
}

// GetCommandLogsByProjectID 根据项目 ID 查询日志
func (s *adbCommandLogService) GetCommandLogsByProjectID(projectID string, start, end time.Time) ([]models.AdbCommandLog, error) {
	var logs []models.AdbCommandLog
	if err := models.DB.Where("project_id = ? AND time >= ? AND time <= ?", projectID, start, end).
		Order("time DESC").
		Find(&logs).Error; err != nil {
		return nil, fmt.Errorf("查询日志失败: %w", err)
	}
	return logs, nil
}

// GetCommandLogsByGatewayID 根据网关 ID 查询日志
func (s *adbCommandLogService) GetCommandLogsByGatewayID(gatewayID string, start, end time.Time) ([]models.AdbCommandLog, error) {
	var logs []models.AdbCommandLog
	if err := models.DB.Where("gateway_id = ? AND time >= ? AND time <= ?", gatewayID, start, end).
		Order("time DESC").
		Find(&logs).Error; err != nil {
		return nil, fmt.Errorf("查询日志失败: %w", err)
	}
	return logs, nil
}

// GetCommandLogs 查询日志（支持分页）
func (s *adbCommandLogService) GetCommandLogs(start, end time.Time, limit, offset int) ([]models.AdbCommandLog, int64, error) {
	var logs []models.AdbCommandLog
	var total int64

	query := models.DB.Model(&models.AdbCommandLog{}).Where("time >= ? AND time <= ?", start, end)

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("查询日志总数失败: %w", err)
	}

	// 获取数据
	if err := query.Order("time DESC").
		Limit(limit).
		Offset(offset).
		Find(&logs).Error; err != nil {
		return nil, 0, fmt.Errorf("查询日志失败: %w", err)
	}

	return logs, total, nil
}

// GetCommandLogsWithFilters 通用查询方法，支持所有过滤条件和分页
func (s *adbCommandLogService) GetCommandLogsWithFilters(mappingID, projectID, gatewayID string, start, end time.Time, limit, offset int) ([]models.AdbCommandLog, int64, error) {
	var logs []models.AdbCommandLog
	var total int64

	// 构建查询条件
	query := models.DB.Model(&models.AdbCommandLog{}).Where("time >= ? AND time <= ?", start, end)

	// 添加过滤条件
	if mappingID != "" {
		query = query.Where("mapping_id = ?", mappingID)
	}
	if projectID != "" {
		query = query.Where("project_id = ?", projectID)
	}
	if gatewayID != "" {
		query = query.Where("gateway_id = ?", gatewayID)
	}

	// 获取总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("查询日志总数失败: %w", err)
	}

	// 获取数据（支持分页）
	if err := query.Order("time DESC").
		Limit(limit).
		Offset(offset).
		Find(&logs).Error; err != nil {
		return nil, 0, fmt.Errorf("查询日志失败: %w", err)
	}

	return logs, total, nil
}

// DeleteCommandLogs 批量删除命令日志
func (s *adbCommandLogService) DeleteCommandLogs(ids []uint) error {
	if len(ids) == 0 {
		return fmt.Errorf("日志 ID 列表不能为空")
	}
	
	if err := models.DB.Where("id IN ?", ids).Delete(&models.AdbCommandLog{}).Error; err != nil {
		return fmt.Errorf("删除命令日志失败: %w", err)
	}
	
	return nil
}

// ClearCommandLogsByMappingID 根据映射 ID 清空所有命令日志
func (s *adbCommandLogService) ClearCommandLogsByMappingID(mappingID string) error {
	if mappingID == "" {
		return fmt.Errorf("映射 ID 不能为空")
	}
	
	if err := models.DB.Where("mapping_id = ?", mappingID).Delete(&models.AdbCommandLog{}).Error; err != nil {
		return fmt.Errorf("清空命令日志失败: %w", err)
	}
	
	return nil
}
