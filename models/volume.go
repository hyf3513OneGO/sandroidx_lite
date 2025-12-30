package models

import (
	"time"
)

// Volume 挂载卷（独立的卷，可被多个Agent复用）
type Volume struct {
	ID          string    `gorm:"primarykey;type:varchar(50)" json:"id"`                   // 卷ID，如 volume_xxxx
	HostPath    string    `gorm:"type:varchar(500);not null;uniqueIndex" json:"host_path"` // 宿主机路径
	VolumeType  string    `gorm:"type:varchar(50);default:'user'" json:"volume_type"`      // user, system
	Description string    `gorm:"type:text" json:"description"`
	SizeBytes   int64     `gorm:"type:bigint;default:0" json:"size_bytes"` // 卷大小（字节）
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (Volume) TableName() string {
	return "volumes"
}
