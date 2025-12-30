package models

import (
	"time"
)

// SandboxVolume Sandbox 与 Volume 的关系表
type SandboxVolume struct {
	ID            uint      `gorm:"primarykey" json:"id"`
	SandboxID     string    `gorm:"type:varchar(50);index;not null" json:"sandbox_id"` // Sandbox ID
	VolumeID      string    `gorm:"type:varchar(50);index;not null" json:"volume_id"`  // Volume ID
	ContainerPath string    `gorm:"type:varchar(255);not null" json:"container_path"`  // 容器内路径
	ReadOnly      bool      `gorm:"type:boolean;default:false" json:"read_only"`       // 是否只读
	Status        string    `gorm:"type:varchar(50);default:'active'" json:"status"`   // active, unmounted
	Description   string    `gorm:"type:text" json:"description"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (SandboxVolume) TableName() string {
	return "sandbox_volumes"
}
