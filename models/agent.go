package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// Command 表示一个命令结构，包含工作目录和运行命令
type Command struct {
	Workdir string `json:"workdir"`
	Run     string `json:"run"`
}

// RunningVariable 表示运行时变量
type RunningVariable struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
}

// CommandSlice 命令数组类型，用于 JSON 序列化
type CommandSlice []Command

// Scan 实现 sql.Scanner 接口
func (cs *CommandSlice) Scan(value interface{}) error {
	if value == nil {
		*cs = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, cs)
}

// Value 实现 driver.Valuer 接口
func (cs CommandSlice) Value() (driver.Value, error) {
	if cs == nil {
		return nil, nil
	}
	return json.Marshal(cs)
}

// StringSlice 字符串数组类型，用于 JSON 序列化
type StringSlice []string

// Scan 实现 sql.Scanner 接口
func (ss *StringSlice) Scan(value interface{}) error {
	if value == nil {
		*ss = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, ss)
}

// Value 实现 driver.Valuer 接口
func (ss StringSlice) Value() (driver.Value, error) {
	if ss == nil {
		return nil, nil
	}
	return json.Marshal(ss)
}

// RunningVariableSlice 运行变量数组类型，用于 JSON 序列化
type RunningVariableSlice []RunningVariable

// Scan 实现 sql.Scanner 接口
func (rvs *RunningVariableSlice) Scan(value interface{}) error {
	if value == nil {
		*rvs = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, rvs)
}

// Value 实现 driver.Valuer 接口
func (rvs RunningVariableSlice) Value() (driver.Value, error) {
	if rvs == nil {
		return nil, nil
	}
	return json.Marshal(rvs)
}

// StringMap 字符串映射类型，用于 JSON 序列化（用于存储环境变量）
type StringMap map[string]string

// Scan 实现 sql.Scanner 接口
func (sm *StringMap) Scan(value interface{}) error {
	if value == nil {
		*sm = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, sm)
}

// Value 实现 driver.Valuer 接口
func (sm StringMap) Value() (driver.Value, error) {
	if sm == nil {
		return nil, nil
	}
	return json.Marshal(sm)
}

// Agent 表示一个智能体实例
type Agent struct {
	ID                   string               `gorm:"primarykey;type:varchar(50)" json:"id"`
	Image                string               `gorm:"type:varchar(255);not null" json:"image"`
	RequiredEnvVariables StringSlice          `gorm:"type:text" json:"required_env_variables"`
	SetupCommands        CommandSlice         `gorm:"type:text" json:"setup_commands"`
	RunningVariables     RunningVariableSlice `gorm:"type:text" json:"running_variables"`
	RunningCommands      CommandSlice         `gorm:"type:text" json:"running_commands"`
	ContainerID          string               `gorm:"type:varchar(100);index" json:"container_id"`
	MappingID            string               `gorm:"type:varchar(100);index" json:"mapping_id"`
	Envs                 StringMap            `gorm:"type:text" json:"envs"` // 存储创建时的所有环境变量
	Status               string               `gorm:"type:varchar(50);index" json:"status"`
	LastError            string               `gorm:"type:text" json:"last_error"`
	CreatedAt            time.Time            `gorm:"index" json:"created_at"`
	UpdatedAt            time.Time            `json:"updated_at"`
	SetupCompletedAt     *time.Time           `json:"setup_completed_at"`
}

func (Agent) TableName() string {
	return "agents"
}
