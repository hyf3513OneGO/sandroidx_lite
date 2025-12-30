package configs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	Server     ServerConfig     `json:"server"`
	Database   DatabaseConfig   `json:"database"`
	AdbGateway AdbGatewayConfig `json:"adb_gateway"`
	Auth       AuthConfig       `json:"auth"`
	Upload     FileUploadConfig `json:"upload"`
}

type AdbGatewayConfig struct {
	GatewayHost     string        `json:"gateway_host"`      // ADB Gateway 的主机地址，例如 "127.0.0.1"
	GatewayAPIPort  int           `json:"gateway_api_port"`  // ADB Gateway API 端口，例如 8080
	GatewayToken    string        `json:"gateway_token"`     // ADB Gateway token，用于客户端连接和容器内部认证（注入到 ADB_GATEWAY_TOKEN 环境变量）
	UploadToken     string        `json:"upload_token"`      // 接收上传数据的 token
	AutoSyncEnabled bool          `json:"auto_sync_enabled"` // 是否在服务启动时自动启动定期同步
	SyncIntervalSec int           `json:"sync_interval_sec"` // 定期同步间隔（秒），默认 300 秒（5分钟）
	Image           string        `json:"image"`             // ADB Gateway 容器镜像名称，例如 "adb-gateway:latest"
	GatewayConfig   GatewayConfig `json:"gateway_config"`    // ADB Gateway 容器配置
	// 命令日志记录配置
	LogSystemMapping  bool `json:"log_system_mapping"`  // 是否记录 system 用的 mapping 的命令历史
	LogAgentMapping   bool `json:"log_agent_mapping"`   // 是否记录 agent 的 mapping 历史
	LogSandboxMapping bool `json:"log_sandbox_mapping"` // 是否记录 sandbox 的 mapping 历史
}

type GatewayConfig struct {
	GatewayID string       `json:"gateway_id"`
	Upload    UploadConfig `json:"upload"`
	Log       LogConfig    `json:"log"`
	Database  DBConfig     `json:"database"`
	Listen    ListenConfig `json:"listen"`
}

type UploadConfig struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	Token   string `json:"token"`
}

type LogConfig struct {
	MaxDays                    int `json:"max_days"`
	PendingWarnIntervalMinutes int `json:"pending_warn_interval_minutes"`
}

type DBConfig struct {
	Path string `json:"path"`
}

type ListenConfig struct {
	MinPort int `json:"min_port"`
	MaxPort int `json:"max_port"`
}

type ServerConfig struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Mode        string `json:"mode"`
	DataPath    string `json:"data_path"`    // 容器数据目录路径
	NetworkName string `json:"network_name"` // Docker 自定义网络名称，用于容器间服务发现
}

type DatabaseConfig struct {
	Type   string       `json:"type"`
	SQLite SQLiteConfig `json:"sqlite"`
	MySQL  MySQLConfig  `json:"mysql"`
}

type SQLiteConfig struct {
	Path string `json:"path"`
}

type MySQLConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	DBName   string `json:"dbname"`
	Charset  string `json:"charset"`
}

type AuthConfig struct {
	JWTSecret     string `json:"jwt_secret"`
	TokenTTLHours int    `json:"token_ttl_hours"`
}

type FileUploadConfig struct {
	TimeoutSeconds int64 `json:"timeout_seconds"` // 上传超时时间（秒），默认 1800（30分钟）
	MaxSizeBytes   int64 `json:"max_size_bytes"`  // 最大文件大小（字节），默认 1073741824（1GB）
}

var AppConfig *Config

func LoadConfig(configPath string) error {
	file, err := os.Open(configPath)
	if err != nil {
		return fmt.Errorf("无法打开配置文件: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	AppConfig = &Config{}
	if err := decoder.Decode(AppConfig); err != nil {
		return fmt.Errorf("无法解析配置文件: %w", err)
	}

	// 获取程序可执行文件所在的目录
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法获取程序路径: %w", err)
	}
	execDir, err := filepath.Abs(filepath.Dir(execPath))
	if err != nil {
		return fmt.Errorf("无法获取程序目录: %w", err)
	}

	// 如果 data_path 是相对路径，将其转换为绝对路径（基于程序所在目录）
	if AppConfig.Server.DataPath != "" && !filepath.IsAbs(AppConfig.Server.DataPath) {
		AppConfig.Server.DataPath = filepath.Join(execDir, AppConfig.Server.DataPath)
		// 清理路径（处理 .. 和 . 等）
		AppConfig.Server.DataPath = filepath.Clean(AppConfig.Server.DataPath)
	}

	return nil
}

func GetDSN() string {
	if AppConfig.Database.Type == "mysql" {
		mysql := AppConfig.Database.MySQL
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
			mysql.User, mysql.Password, mysql.Host, mysql.Port, mysql.DBName, mysql.Charset)
	}
	return AppConfig.Database.SQLite.Path
}
