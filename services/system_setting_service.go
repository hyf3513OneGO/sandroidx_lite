package services

import (
	"errors"

	"sandroidx.com/sandroidx_lite/models"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// RuntimeSettings 定义系统运行时可修改的配置。
type RuntimeSettings struct {
	AllowRegistration bool `json:"allow_registration"`
	AdminInitialized  bool `json:"admin_initialized"`
	AllowSandboxStart bool `json:"allow_sandbox_start"`
	MaintenanceMode   bool `json:"maintenance_mode"`
}

type RuntimeSettingsUpdate struct {
	AllowRegistration *bool `json:"allow_registration"`
	AllowSandboxStart *bool `json:"allow_sandbox_start"`
	MaintenanceMode   *bool `json:"maintenance_mode"`
	AdminInitialized  *bool `json:"admin_initialized"`
}

type SystemSettingService struct{}

func NewSystemSettingService() *SystemSettingService {
	return &SystemSettingService{}
}

func defaultRuntimeSettings() RuntimeSettings {
	return RuntimeSettings{
		AllowRegistration: true,
		AdminInitialized:  false,
		AllowSandboxStart: true,
		MaintenanceMode:   false,
	}
}

// EnsureDefaults 确保至少有一条设置记录，并返回当前设置。
func (s *SystemSettingService) EnsureDefaults() (*RuntimeSettings, error) {
	var setting models.SystemSetting
	err := models.DB.First(&setting).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			defaults := defaultRuntimeSettings()
			setting = models.SystemSetting{
				Settings: runtimeSettingsToMap(defaults),
			}
			if createErr := models.DB.Create(&setting).Error; createErr != nil {
				return nil, createErr
			}
			return &defaults, nil
		}
		return nil, err
	}
	settings := mapToRuntimeSettings(setting.Settings)
	return &settings, nil
}

// GetSettings 获取当前设置（会自动初始化默认值）。
func (s *SystemSettingService) GetSettings() (*RuntimeSettings, error) {
	return s.EnsureDefaults()
}

// UpdateSettings 更新指定字段。
func (s *SystemSettingService) UpdateSettings(update RuntimeSettingsUpdate) (*RuntimeSettings, error) {
	setting, current, err := s.loadForUpdate()
	if err != nil {
		return nil, err
	}

	if update.AllowRegistration != nil {
		current.AllowRegistration = *update.AllowRegistration
	}
	if update.AllowSandboxStart != nil {
		current.AllowSandboxStart = *update.AllowSandboxStart
	}
	if update.MaintenanceMode != nil {
		current.MaintenanceMode = *update.MaintenanceMode
	}
	if update.AdminInitialized != nil {
		current.AdminInitialized = *update.AdminInitialized
	}

	setting.Settings = runtimeSettingsToMap(current)
	if err := models.DB.Save(setting).Error; err != nil {
		return nil, err
	}
	return &current, nil
}

// MarkAdminInitialized 标记管理员初始化状态。
func (s *SystemSettingService) MarkAdminInitialized(done bool) error {
	_, _, err := s.ensureAdminFlag(done)
	return err
}

func (s *SystemSettingService) ensureAdminFlag(done bool) (*models.SystemSetting, RuntimeSettings, error) {
	setting, current, err := s.loadForUpdate()
	if err != nil {
		return nil, RuntimeSettings{}, err
	}
	current.AdminInitialized = done
	setting.Settings = runtimeSettingsToMap(current)
	return setting, current, models.DB.Save(setting).Error
}

func (s *SystemSettingService) loadForUpdate() (*models.SystemSetting, RuntimeSettings, error) {
	var setting models.SystemSetting
	err := models.DB.First(&setting).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			defaults := defaultRuntimeSettings()
			setting = models.SystemSetting{
				Settings: runtimeSettingsToMap(defaults),
			}
			if createErr := models.DB.Create(&setting).Error; createErr != nil {
				return nil, RuntimeSettings{}, createErr
			}
			return &setting, defaults, nil
		}
		return nil, RuntimeSettings{}, err
	}
	current := mapToRuntimeSettings(setting.Settings)
	return &setting, current, nil
}

func runtimeSettingsToMap(settings RuntimeSettings) datatypes.JSONMap {
	return datatypes.JSONMap{
		"allow_registration":  settings.AllowRegistration,
		"admin_initialized":   settings.AdminInitialized,
		"allow_sandbox_start": settings.AllowSandboxStart,
		"maintenance_mode":    settings.MaintenanceMode,
	}
}

func mapToRuntimeSettings(data datatypes.JSONMap) RuntimeSettings {
	settings := defaultRuntimeSettings()

	if v, ok := data["allow_registration"]; ok {
		if val, okb := v.(bool); okb {
			settings.AllowRegistration = val
		}
	}
	if v, ok := data["admin_initialized"]; ok {
		if val, okb := v.(bool); okb {
			settings.AdminInitialized = val
		}
	}
	if v, ok := data["allow_sandbox_start"]; ok {
		if val, okb := v.(bool); okb {
			settings.AllowSandboxStart = val
		}
	}
	if v, ok := data["maintenance_mode"]; ok {
		if val, okb := v.(bool); okb {
			settings.MaintenanceMode = val
		}
	}

	return settings
}
