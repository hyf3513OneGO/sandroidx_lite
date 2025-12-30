package models

import (
	"time"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"
	RoleGuest = "guest"
)

type User struct {
	ID           uint       `gorm:"primarykey" json:"id"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	Name         string     `gorm:"size:100;not null" json:"name"`
	Email        string     `gorm:"size:100;uniqueIndex;not null" json:"email"`
	PasswordHash string     `gorm:"size:255;not null" json:"-"`        // bcrypt hash
	Role         string     `gorm:"size:20;default:guest" json:"role"` // admin/user/guest
	LastLogin    *time.Time `json:"last_login,omitempty"`              // 最近登录时间
}

func (User) TableName() string {
	return "users"
}
