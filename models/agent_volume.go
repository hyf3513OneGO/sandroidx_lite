package models

import (
	"time"
)

// AgentVolume Agent 与 Volume 的关系表
type AgentVolume struct {
	ID            uint      `gorm:"primarykey" json:"id"`
	AgentID       string    `gorm:"type:varchar(50);index;not null" json:"agent_id"`  // Agent ID
	VolumeID      string    `gorm:"type:varchar(50);index;not null" json:"volume_id"` // Volume ID
	ContainerPath string    `gorm:"type:varchar(255);not null" json:"container_path"` // 容器内路径
	ReadOnly      bool      `gorm:"type:boolean;default:false" json:"read_only"`      // 是否只读
	Status        string    `gorm:"type:varchar(50);default:'active'" json:"status"`  // active, unmounted
	Description   string    `gorm:"type:text" json:"description"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (AgentVolume) TableName() string {
	return "agent_volumes"
}
