package services

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"gorm.io/gorm"
	"sandroidx.com/sandroidx_lite/configs"
	"sandroidx.com/sandroidx_lite/models"
	"sandroidx.com/sandroidx_lite/utils"
)

// VolumeService Volume 服务接口
type VolumeService interface {
	// 创建 Volume
	CreateVolume(description string) (*models.Volume, error)
	// 获取 Volume
	GetVolume(id string) (*models.Volume, error)
	// 列出所有 Volumes
	ListVolumes(volumeType string) ([]models.Volume, error)
	// 删除 Volume
	DeleteVolume(id string, force bool) error
	// 获取 Volume 的使用情况
	GetVolumeUsage(volumeID string) ([]VolumeUsageInfo, error)
	// 计算 Volume 大小
	CalculateVolumeSize(volumeID string) (int64, error)
}

// VolumeUsageInfo Volume 使用信息
type VolumeUsageInfo struct {
	AgentID       string `json:"agent_id"`
	ContainerPath string `json:"container_path"`
	ReadOnly      bool   `json:"read_only"`
	Status        string `json:"status"`
	Description   string `json:"description"`
}

// volumeService Volume 服务实现
type volumeService struct {
	dataPath string
}

// NewVolumeService 创建新的 Volume 服务
func NewVolumeService() VolumeService {
	return &volumeService{
		dataPath: configs.AppConfig.Server.DataPath,
	}
}

// CreateVolume 创建新的 Volume
func (s *volumeService) CreateVolume(description string) (*models.Volume, error) {
	// 生成 Volume ID
	volumeID := utils.GenerateVolumeID()

	// 创建宿主机目录
	volumesBasePath := filepath.Join(s.dataPath, "volumes")
	hostPath := filepath.Join(volumesBasePath, volumeID)

	if err := os.MkdirAll(hostPath, 0755); err != nil {
		return nil, fmt.Errorf("创建目录失败: %w", err)
	}

	// 创建 Volume 记录
	volume := &models.Volume{
		ID:          volumeID,
		HostPath:    hostPath,
		VolumeType:  "user",
		Description: description,
	}

	if err := models.DB.Create(volume).Error; err != nil {
		// 创建失败，清理目录
		os.RemoveAll(hostPath)
		return nil, fmt.Errorf("创建 Volume 记录失败: %w", err)
	}

	log.Printf("创建 Volume 成功: %s -> %s", volumeID, hostPath)
	return volume, nil
}

// GetVolume 获取 Volume
func (s *volumeService) GetVolume(id string) (*models.Volume, error) {
	var volume models.Volume
	if err := models.DB.First(&volume, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("Volume 不存在")
		}
		return nil, fmt.Errorf("查询 Volume 失败: %w", err)
	}
	return &volume, nil
}

// ListVolumes 列出所有 Volumes
// volumeType: 为空则返回所有类型，否则只返回指定类型（user/system）
func (s *volumeService) ListVolumes(volumeType string) ([]models.Volume, error) {
	var volumes []models.Volume
	query := models.DB.Order("created_at DESC")

	if volumeType != "" {
		query = query.Where("volume_type = ?", volumeType)
	}

	if err := query.Find(&volumes).Error; err != nil {
		return nil, fmt.Errorf("查询 Volumes 失败: %w", err)
	}

	return volumes, nil
}

// DeleteVolume 删除 Volume
// force: 是否强制删除（即使有 Agent 在使用）
func (s *volumeService) DeleteVolume(id string, force bool) error {
	volume, err := s.GetVolume(id)
	if err != nil {
		return err
	}

	// 检查是否是系统卷
	if volume.VolumeType == "system" {
		return fmt.Errorf("不能删除系统卷")
	}

	// 检查是否有 Agent 在使用
	var usageCount int64
	if err := models.DB.Model(&models.AgentVolume{}).
		Where("volume_id = ? AND status = ?", id, "active").
		Count(&usageCount).Error; err != nil {
		return fmt.Errorf("查询 Volume 使用情况失败: %w", err)
	}

	if usageCount > 0 && !force {
		return fmt.Errorf("Volume 正在被 %d 个 Agent 使用，无法删除。使用 force=true 强制删除", usageCount)
	}

	// 检查是否有只读挂载，只读挂载不应该删除本地目录
	var readOnlyCount int64
	if err := models.DB.Model(&models.AgentVolume{}).
		Where("volume_id = ? AND read_only = ?", id, true).
		Count(&readOnlyCount).Error; err != nil {
		log.Printf("警告: 查询只读挂载失败: %v", err)
	}

	// 删除宿主机目录（只有非只读挂载或没有任何挂载时才删除）
	if readOnlyCount == 0 {
		if _, err := os.Stat(volume.HostPath); err == nil {
			if err := os.RemoveAll(volume.HostPath); err != nil {
				log.Printf("警告: 删除目录失败: %v", err)
			}
		}
	} else {
		log.Printf("跳过删除目录: Volume %s 有 %d 个只读挂载，保留本地目录", id, readOnlyCount)
	}

	// 删除所有相关的 Agent-Volume 关系
	if err := models.DB.Where("volume_id = ?", id).Delete(&models.AgentVolume{}).Error; err != nil {
		log.Printf("警告: 删除 Agent-Volume 关系失败: %v", err)
	}

	// 删除 Volume 记录
	if err := models.DB.Delete(&models.Volume{}, "id = ?", id).Error; err != nil {
		return fmt.Errorf("删除 Volume 记录失败: %w", err)
	}

	log.Printf("Volume %s 已删除", id)
	return nil
}

// GetVolumeUsage 获取 Volume 的使用情况
func (s *volumeService) GetVolumeUsage(volumeID string) ([]VolumeUsageInfo, error) {
	// 检查 Volume 是否存在
	if _, err := s.GetVolume(volumeID); err != nil {
		return nil, err
	}

	// 查询所有使用该 Volume 的 Agent
	var agentVolumes []models.AgentVolume
	if err := models.DB.Where("volume_id = ?", volumeID).
		Order("created_at DESC").
		Find(&agentVolumes).Error; err != nil {
		return nil, fmt.Errorf("查询 Volume 使用情况失败: %w", err)
	}

	// 转换为使用信息
	usageInfos := make([]VolumeUsageInfo, len(agentVolumes))
	for i, av := range agentVolumes {
		usageInfos[i] = VolumeUsageInfo{
			AgentID:       av.AgentID,
			ContainerPath: av.ContainerPath,
			ReadOnly:      av.ReadOnly,
			Status:        av.Status,
			Description:   av.Description,
		}
	}

	return usageInfos, nil
}

// CalculateVolumeSize 计算 Volume 的磁盘使用大小
func (s *volumeService) CalculateVolumeSize(volumeID string) (int64, error) {
	volume, err := s.GetVolume(volumeID)
	if err != nil {
		return 0, err
	}

	// 检查目录是否存在
	if _, err := os.Stat(volume.HostPath); os.IsNotExist(err) {
		return 0, nil
	}

	// 计算目录大小
	var size int64
	err = filepath.Walk(volume.HostPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})

	if err != nil {
		return 0, fmt.Errorf("计算目录大小失败: %w", err)
	}

	// 更新数据库中的大小
	if err := models.DB.Model(&models.Volume{}).
		Where("id = ?", volumeID).
		Update("size_bytes", size).Error; err != nil {
		log.Printf("警告: 更新 Volume 大小失败: %v", err)
	}

	return size, nil
}
