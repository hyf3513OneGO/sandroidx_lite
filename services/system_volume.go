package services

import (
	"fmt"
	"os"
	"path/filepath"

	"gorm.io/gorm"
	"sandroidx.com/sandroidx_lite/models"
)

const (
	sharedApkVolumeID          = "volume_sandroidx_apks"
	sharedApkVolumeDescription = "shared apk volume"
)

// EnsureSharedApkVolume 确保共享 APK 卷存在，并返回卷ID及宿主机路径
func EnsureSharedApkVolume(dataPath string) (string, string, error) {
	if dataPath == "" {
		return "", "", fmt.Errorf("data_path 未配置，无法创建共享 APK 卷")
	}

	hostPath := filepath.Join(dataPath, "apks")
	if err := os.MkdirAll(hostPath, 0755); err != nil {
		return "", "", fmt.Errorf("创建共享 APK 目录失败: %w", err)
	}

	var volume models.Volume
	err := models.DB.First(&volume, "id = ?", sharedApkVolumeID).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			volume = models.Volume{
				ID:          sharedApkVolumeID,
				HostPath:    hostPath,
				VolumeType:  "system",
				Description: sharedApkVolumeDescription,
			}
			if err := models.DB.Create(&volume).Error; err != nil {
				return "", "", fmt.Errorf("创建共享 APK 卷记录失败: %w", err)
			}
			return volume.ID, hostPath, nil
		}
		return "", "", fmt.Errorf("查询共享 APK 卷失败: %w", err)
	}

	// 校正已存在记录的关键字段
	needUpdate := volume.HostPath != hostPath || volume.VolumeType != "system" || volume.Description != sharedApkVolumeDescription
	if needUpdate {
		volume.HostPath = hostPath
		volume.VolumeType = "system"
		volume.Description = sharedApkVolumeDescription
		if err := models.DB.Save(&volume).Error; err != nil {
			return "", "", fmt.Errorf("更新共享 APK 卷记录失败: %w", err)
		}
	}

	return volume.ID, volume.HostPath, nil
}
