package models

import (
	"time"
)

// Mapping 映射数据库模型
type Mapping struct {
	ID        string    `gorm:"primarykey;type:varchar(36)" json:"id"`
	ProjectID string    `gorm:"type:varchar(100);index" json:"project_id"`
	FromID    string    `gorm:"type:varchar(100);index" json:"from_id"`
	ToID      string    `gorm:"type:varchar(100);index" json:"to_id"`
	Name      string    `gorm:"type:varchar(200);not null;index" json:"name"`
	Note      string    `gorm:"type:text" json:"note"`
	Listen    string    `gorm:"type:varchar(100);uniqueIndex" json:"listen"`
	Upstream  string    `gorm:"type:varchar(100)" json:"upstream"`
	Status    string    `gorm:"type:varchar(50);index" json:"status"`
	LastError string    `gorm:"type:text" json:"last_error"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	SyncedAt  time.Time `gorm:"index" json:"synced_at"` // 最后同步时间
}

func (Mapping) TableName() string {
	return "mappings"
}
