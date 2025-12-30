package models

import "time"

const (
	ApkTypeRemote = "remote"
	ApkTypeLocal  = "local"
)

// Apk 用于存储可安装 APK 的元信息
// - type=local: path 为服务端本地路径（例如共享 APK 目录中的文件路径）
// - type=remote: path 为远程下载地址（例如 http(s) URL）
type Apk struct {
	ID          string    `gorm:"primarykey;type:varchar(64)" json:"id"`
	Name        string    `gorm:"type:varchar(200);index;not null" json:"name"`
	PackageName string    `gorm:"type:varchar(255);not null;uniqueIndex:idx_apk_pkg_ver" json:"package_name"`
	Version     string    `gorm:"type:varchar(64);not null;uniqueIndex:idx_apk_pkg_ver" json:"version"`
	// Path: 文件已下载/已上传后在服务端的本地路径（空字符串表示尚未落盘）
	Path string `gorm:"type:text;not null" json:"path"`
	// URL: 远程下载地址（type=remote 时必填；type=local 时可为空）
	URL  string `gorm:"type:text" json:"url"`
	// Icon: 应用图标文件路径（从 APK 中提取的图标保存路径，可为空）
	Icon        string    `gorm:"type:text" json:"icon"`
	Type        string    `gorm:"type:varchar(20);index;not null" json:"type"`
	Description string    `gorm:"type:text" json:"description"`
	CreatedAt   time.Time `gorm:"index" json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (Apk) TableName() string {
	return "apks"
}


