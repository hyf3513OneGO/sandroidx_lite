package models

import (
	"time"

	"gorm.io/datatypes"
)

// Template 用于存储 Agent/Sandbox 等资源的预设配置
type Template struct {
	ID          string         `gorm:"primarykey;type:varchar(64)" json:"id"`
	Name        string         `gorm:"type:varchar(200);uniqueIndex;not null" json:"name"`
	Description string         `gorm:"type:text" json:"description"`
	Content     datatypes.JSON `gorm:"type:json" json:"content"`
	CreatedAt   time.Time      `gorm:"index" json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// TableName 自定义表名
func (Template) TableName() string {
	return "templates"
}
