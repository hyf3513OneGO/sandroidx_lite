package models

import (
	"encoding/json"
	"time"
)

// AdbGateway ADB Gateway 容器信息模型
type AdbGateway struct {
	ID             string    `gorm:"primarykey;type:varchar(50)" json:"id"`
	ContainerName  string    `gorm:"type:varchar(255);not null;uniqueIndex" json:"container_name"`
	ContainerID    string    `gorm:"type:varchar(100);index" json:"container_id"`
	Image          string    `gorm:"type:varchar(255);not null" json:"image"`
	GatewayHost    string    `gorm:"type:varchar(100)" json:"gateway_host"`
	GatewayAPIPort int       `gorm:"type:int" json:"gateway_api_port"`
	PortRanges     string    `gorm:"type:text" json:"port_ranges"` // JSON 字符串，存储端口范围数组，如 ["8080", "15555-25555"]
	Status         string    `gorm:"type:varchar(50);index" json:"status"`
	CreatedAt      time.Time `gorm:"index" json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// GetPortRanges 获取端口范围数组
func (g *AdbGateway) GetPortRanges() ([]string, error) {
	if g.PortRanges == "" {
		return []string{}, nil
	}
	var ranges []string
	err := json.Unmarshal([]byte(g.PortRanges), &ranges)
	return ranges, err
}

// SetPortRanges 设置端口范围数组
func (g *AdbGateway) SetPortRanges(ranges []string) error {
	data, err := json.Marshal(ranges)
	if err != nil {
		return err
	}
	g.PortRanges = string(data)
	return nil
}

func (AdbGateway) TableName() string {
	return "adb_gateways"
}
