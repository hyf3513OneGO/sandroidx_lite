package utils

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

const (
	scrcpyServerURL  = "https://github.com/Genymobile/scrcpy/releases/download/v3.3.3/scrcpy-server-v3.3.3"
	scrcpyServerName = "scrcpy-server.jar"
)

// EnsureScrcpyServer 确保 scrcpy-server 文件存在，如果不存在则下载
func EnsureScrcpyServer(dataPath string) error {
	if dataPath == "" {
		return fmt.Errorf("data_path 未配置，无法下载 scrcpy-server")
	}

	// 构建文件路径
	serverPath := filepath.Join(dataPath, scrcpyServerName)

	// 检查文件是否已存在
	if _, err := os.Stat(serverPath); err == nil {
		log.Printf("scrcpy-server 已存在: %s", serverPath)
		return nil
	}

	// 确保目录存在
	if err := os.MkdirAll(dataPath, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	// 下载文件
	log.Printf("开始下载 scrcpy-server 从 %s 到 %s", scrcpyServerURL, serverPath)
	if err := downloadFile(scrcpyServerURL, serverPath); err != nil {
		return fmt.Errorf("下载 scrcpy-server 失败: %w", err)
	}

	log.Printf("scrcpy-server 下载成功: %s", serverPath)
	return nil
}

// GetScrcpyServerPath 获取 scrcpy-server 文件路径
func GetScrcpyServerPath(dataPath string) string {
	if dataPath == "" {
		return ""
	}
	return filepath.Join(dataPath, scrcpyServerName)
}

// downloadFile 下载文件
func downloadFile(url string, filepath string) error {
	// 创建 HTTP 请求
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP 请求失败，状态码: %d", resp.StatusCode)
	}

	// 创建文件
	out, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer out.Close()

	// 复制内容
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}

	return nil
}
