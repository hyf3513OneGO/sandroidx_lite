package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// MountConfig 表示挂载配置
type MountConfig struct {
	Volume        string `json:"volume"`         // 卷ID，为空时自动创建新卷
	ContainerPath string `json:"container_path"` // 容器路径
	ReadOnly      bool   `json:"read_only"`      // 是否只读
}

// MountConfigSlice 挂载配置数组类型，用于 JSON 序列化
type MountConfigSlice []MountConfig

// Scan 实现 sql.Scanner 接口
func (mcs *MountConfigSlice) Scan(value interface{}) error {
	if value == nil {
		*mcs = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, mcs)
}

// Value 实现 driver.Valuer 接口
func (mcs MountConfigSlice) Value() (driver.Value, error) {
	if mcs == nil {
		return nil, nil
	}
	return json.Marshal(mcs)
}

// Sandbox 表示一个沙箱实例
type Sandbox struct {
	ID                 string           `gorm:"primarykey;type:varchar(50)" json:"id"`
	Type               string           `gorm:"type:varchar(100)" json:"type"`                        // phone/redroid
	Image              string           `gorm:"type:varchar(255);not null" json:"image"`              // 镜像名称
	Mounts             MountConfigSlice `gorm:"type:text" json:"mounts"`                              // 挂载配置
	Ports              StringSlice      `gorm:"type:text" json:"ports"`                               // 端口列表
	Privileged         bool             `gorm:"type:boolean;default:false" json:"privileged"`         // 是否特权模式
	Args               StringSlice      `gorm:"type:text" json:"args"`                                // 启动参数
	SetupAdbCommands   StringSlice      `gorm:"type:text" json:"setup_adb_commands"`                  // 容器启动后要执行的 ADB 命令列表（不包含 "adb" 前缀）
	Envs               StringMap        `gorm:"type:text" json:"envs"`                                // 环境变量
	ContainerID        string           `gorm:"type:varchar(100);index" json:"container_id"`          // 容器 ID
	ContainerName      string           `gorm:"type:varchar(255);uniqueIndex" json:"container_name"`  // 容器名称
	AdbMappingID       string           `gorm:"type:varchar(100);index" json:"adb_mapping_id"`        // ADB Gateway 映射 ID（系统操作，用于 scrcpy 和系统操作）
	AgentUserMappingID string           `gorm:"type:varchar(100);index" json:"agent_user_mapping_id"` // ADB Gateway 映射 ID（Agent/User 操作，用于记录）
	ScrcpyForwardPort  int              `gorm:"type:integer" json:"scrcpy_forward_port"`              // Scrcpy forward 端口（0 表示未设置）
	InstalledApkIDs    StringSlice      `gorm:"type:text" json:"installed_apk_ids"`                 // 已安装的 APK ID 列表（重启后自动重新安装）
	Status             string           `gorm:"type:varchar(50);index" json:"status"`                 // 状态: creating, running, stopped, failed
	LastError          string           `gorm:"type:text" json:"last_error"`                          // 最后错误信息
	CreatedAt          time.Time        `gorm:"index" json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
}

func (Sandbox) TableName() string {
	return "sandboxes"
}
