package models

import "time"

// AgentShare 表示一个 Agent 的对外分享令牌（只读）。
// 通过 token 可访问 share 接口（无需登录），但能力受限（只读 Shell / 仅视频）。
type AgentShare struct {
	// Token 作为主键，避免再引入自增 ID；使用 URL-safe token
	Token string `gorm:"primaryKey;type:varchar(128)" json:"token"`

	AgentID string `gorm:"type:varchar(50);index;not null" json:"agent_id"`

	// 可选过期时间；nil 表示不过期（不建议生产环境使用）
	ExpiresAt *time.Time `gorm:"index" json:"expires_at"`

	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

func (AgentShare) TableName() string {
	return "agent_shares"
}


