package models

import (
	"time"
)

// AdbCommandLog ADB 命令日志数据库模型
type AdbCommandLog struct {
	ID           uint      `gorm:"primarykey" json:"id"`
	Time         time.Time `gorm:"index" json:"time"`
	From         string    `gorm:"type:varchar(100)" json:"from"`
	To           string    `gorm:"type:varchar(100)" json:"to"`
	AdbCommand   string    `gorm:"type:text" json:"adb_command"`
	ConnectionID string    `gorm:"type:varchar(200);index" json:"connection_id"`
	MappingID    string    `gorm:"type:varchar(36);index" json:"mapping_id"`
	ProjectID    string    `gorm:"type:varchar(100);index" json:"project_id"`
	FromID       string    `gorm:"type:varchar(100);index" json:"from_id"`
	ToID         string    `gorm:"type:varchar(100);index" json:"to_id"`
	GatewayID    string    `gorm:"type:varchar(100);index" json:"gateway_id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (AdbCommandLog) TableName() string {
	return "adb_command_logs"
}
