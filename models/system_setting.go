package models

import (
	"time"

	"gorm.io/datatypes"
)

// SystemSetting 用于存储可运行时修改的系统配置，保存在 JSON 字段中。
type SystemSetting struct {
	ID        uint              `gorm:"primarykey" json:"id"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	Settings  datatypes.JSONMap `gorm:"type:json" json:"settings"`
}

func (SystemSetting) TableName() string {
	return "system_settings"
}
