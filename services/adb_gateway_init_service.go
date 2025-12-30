package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"sandroidx.com/sandroidx_lite/configs"
	"sandroidx.com/sandroidx_lite/models"
)

// parsePortRange 解析端口范围字符串
// 支持格式：
// - "8080" -> 返回单个端口
// - "15555-25555" -> 返回范围内的所有端口
func parsePortRange(portRange string) ([]int, error) {
	portRange = strings.TrimSpace(portRange)
	if portRange == "" {
		return nil, fmt.Errorf("端口范围不能为空")
	}

	// 检查是否包含 "-"（范围格式）
	if strings.Contains(portRange, "-") {
		parts := strings.Split(portRange, "-")
		if len(parts) != 2 {
			return nil, fmt.Errorf("无效的端口范围格式: %s", portRange)
		}

		start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			return nil, fmt.Errorf("无效的起始端口: %s", parts[0])
		}

		end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, fmt.Errorf("无效的结束端口: %s", parts[1])
		}

		if start <= 0 || end <= 0 {
			return nil, fmt.Errorf("端口必须大于 0")
		}

		if start > end {
			return nil, fmt.Errorf("起始端口 (%d) 必须小于等于结束端口 (%d)", start, end)
		}

		// 生成范围内的所有端口
		ports := make([]int, 0, end-start+1)
		for port := start; port <= end; port++ {
			ports = append(ports, port)
		}
		return ports, nil
	}

	// 单个端口
	port, err := strconv.Atoi(portRange)
	if err != nil {
		return nil, fmt.Errorf("无效的端口: %s", portRange)
	}

	if port <= 0 {
		return nil, fmt.Errorf("端口必须大于 0")
	}

	return []int{port}, nil
}

// parsePortRanges 解析端口范围数组，返回所有端口的列表
func parsePortRanges(portRanges []string) ([]int, error) {
	allPorts := make([]int, 0)
	portSet := make(map[int]bool) // 用于去重

	for _, portRange := range portRanges {
		ports, err := parsePortRange(portRange)
		if err != nil {
			return nil, fmt.Errorf("解析端口范围 %s 失败: %w", portRange, err)
		}

		for _, port := range ports {
			if !portSet[port] {
				portSet[port] = true
				allPorts = append(allPorts, port)
			}
		}
	}

	return allPorts, nil
}

// generatePortMappings 从端口范围数组生成 Docker 端口映射
// portRanges: 端口范围数组，如 ["8080", "1001-1006"]
// hostIP: 宿主机 IP 地址（如 "127.0.0.1"）
// 返回: Docker 端口映射，对于范围保持范围格式，如 map["127.0.0.1:1001-1006"] = "1001-1006"
func generatePortMappings(portRanges []string, hostIP string) (map[string]string, error) {
	mappings := make(map[string]string)

	for _, portRange := range portRanges {
		portRange = strings.TrimSpace(portRange)
		if portRange == "" {
			continue
		}

		// 检查是否是范围格式
		if strings.Contains(portRange, "-") {
			// 保持范围格式，不拆分成单个端口
			hostPortKey := fmt.Sprintf("%s:%s", hostIP, portRange)
			mappings[hostPortKey] = portRange
		} else {
			// 单个端口
			hostPortKey := fmt.Sprintf("%s:%s", hostIP, portRange)
			mappings[hostPortKey] = portRange
		}
	}

	return mappings, nil
}

// extractMinMaxPorts 从端口范围数组中提取最小和最大端口
// 用于生成 gateway 配置文件中的 min_port 和 max_port
func extractMinMaxPorts(portRanges []string) (minPort, maxPort int, err error) {
	allPorts, err := parsePortRanges(portRanges)
	if err != nil {
		return 0, 0, err
	}

	if len(allPorts) == 0 {
		return 0, 0, fmt.Errorf("端口范围列表为空")
	}

	minPort = allPorts[0]
	maxPort = allPorts[0]

	for _, port := range allPorts {
		if port < minPort {
			minPort = port
		}
		if port > maxPort {
			maxPort = port
		}
	}

	return minPort, maxPort, nil
}

// AdbGatewayInitService ADB Gateway 初始化服务
type AdbGatewayInitService struct {
	dockerService DockerService
	dataPath      string
	containerName string
}

// NewAdbGatewayInitService 创建 ADB Gateway 初始化服务
func NewAdbGatewayInitService(dockerService DockerService) *AdbGatewayInitService {
	return &AdbGatewayInitService{
		dockerService: dockerService,
		dataPath:      configs.AppConfig.Server.DataPath,
		containerName: "adb-gateway-sandroidx_lite",
	}
}

// ensureNetwork 确保自定义网络存在
func (s *AdbGatewayInitService) ensureNetwork(ctx context.Context) error {
	// 检查网络是否存在
	networks, err := s.dockerService.ListNetworks(ctx)
	if err != nil {
		return fmt.Errorf("列出网络失败: %w", err)
	}

	networkName := getNetworkName()
	for _, net := range networks {
		if net.Name == networkName {
			log.Printf("网络 %s 已存在", networkName)
			return nil
		}
	}

	// 网络不存在，创建它
	log.Printf("创建自定义网络 %s...", networkName)
	networkConfig := &NetworkCreateConfig{
		Name:       networkName,
		Driver:     "bridge",
		Internal:   false,
		Attachable: true,
		EnableIPv6: false,
	}

	networkID, err := s.dockerService.CreateNetwork(ctx, networkConfig)
	if err != nil {
		return fmt.Errorf("创建网络失败: %w", err)
	}

	log.Printf("网络 %s 创建成功，ID: %s", networkName, networkID)
	return nil
}

// getBridgeNetworkGateway 获取容器所在网络的网关 IP（优先使用自定义网络）
func (s *AdbGatewayInitService) getBridgeNetworkGateway(ctx context.Context) (string, error) {
	// 获取网络列表
	networks, err := s.dockerService.ListNetworks(ctx)
	if err != nil {
		return "", fmt.Errorf("获取网络列表失败: %w", err)
	}

	// 优先查找自定义网络（容器主要在这个网络中）
	customNetworkName := getNetworkName()
	var customNetworkID string
	for _, net := range networks {
		if net.Name == customNetworkName {
			customNetworkID = net.ID
			break
		}
	}

	// 如果找到自定义网络，使用它的网关 IP
	if customNetworkID != "" {
		networkInfo, err := s.dockerService.GetNetwork(ctx, customNetworkID)
		if err == nil && len(networkInfo.IPAM.Config) > 0 {
			gateway := networkInfo.IPAM.Config[0].Gateway
			if gateway != "" {
				log.Printf("使用自定义网络 %s 的网关 IP: %s", customNetworkName, gateway)
				return gateway, nil
			}
		}
	}

	// 回退到 bridge 网络
	var bridgeNetworkID string
	for _, net := range networks {
		if net.Name == "bridge" && net.Driver == "bridge" {
			bridgeNetworkID = net.ID
			break
		}
	}

	if bridgeNetworkID == "" {
		return "", fmt.Errorf("未找到可用的网络")
	}

	// 获取网络详细信息以获取网关 IP
	networkInfo, err := s.dockerService.GetNetwork(ctx, bridgeNetworkID)
	if err != nil {
		return "", fmt.Errorf("获取网络信息失败: %w", err)
	}

	// 从 IPAM 配置中获取网关 IP
	if len(networkInfo.IPAM.Config) > 0 {
		gateway := networkInfo.IPAM.Config[0].Gateway
		if gateway != "" {
			log.Printf("使用 bridge 网络的网关 IP: %s", gateway)
			return gateway, nil
		}
	}

	return "", fmt.Errorf("无法从网络获取网关 IP")
}

// connectToBridgeNetwork 将容器连接到默认的 bridge 网络
func (s *AdbGatewayInitService) connectToBridgeNetwork(ctx context.Context, containerID string) error {
	// 获取 bridge 网络信息
	networks, err := s.dockerService.ListNetworks(ctx)
	if err != nil {
		return fmt.Errorf("获取网络列表失败: %w", err)
	}

	// 查找默认的 bridge 网络
	var bridgeNetworkID string
	for _, net := range networks {
		if net.Name == "bridge" && net.Driver == "bridge" {
			bridgeNetworkID = net.ID
			break
		}
	}

	if bridgeNetworkID == "" {
		return fmt.Errorf("未找到默认的 bridge 网络")
	}

	// 连接容器到 bridge 网络
	connectConfig := &NetworkConnectConfig{}
	if err := s.dockerService.ConnectContainerToNetwork(ctx, bridgeNetworkID, containerID, connectConfig); err != nil {
		// 如果已经连接，忽略错误
		if strings.Contains(err.Error(), "is already a member") || strings.Contains(err.Error(), "already exists") {
			return nil
		}
		return err
	}

	return nil
}

// Initialize 初始化 ADB Gateway 容器
func (s *AdbGatewayInitService) Initialize(ctx context.Context) error {
	log.Println("开始初始化 ADB Gateway 容器...")

	// 1. 确保自定义网络存在
	if err := s.ensureNetwork(ctx); err != nil {
		return fmt.Errorf("确保网络存在失败: %w", err)
	}

	// 2. 检查并创建 data_path 目录
	if err := s.ensureDataPath(); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}

	// 2. 检查容器是否存在
	exists, running, err := s.checkContainer(ctx)
	if err != nil {
		return fmt.Errorf("检查容器失败: %w", err)
	}

	if exists {
		if running {
			log.Printf("容器 %s 已存在且正在运行", s.containerName)
			// 更新数据库中的状态
			if err := s.saveOrUpdateGatewayInfo(ctx); err != nil {
				log.Printf("警告: 保存 ADB Gateway 信息失败: %v", err)
			}
			return nil
		}
		log.Printf("容器 %s 已存在但未运行，尝试启动...", s.containerName)
		if err := s.startContainer(ctx); err != nil {
			return fmt.Errorf("启动容器失败: %w", err)
		}
		// 等待几秒钟让容器内的服务启动
		waitTime := 5 * time.Second
		log.Printf("等待 %v 让 ADB Gateway 服务启动...", waitTime)
		time.Sleep(waitTime)

		// 再次检查容器状态
		exists, running, err := s.checkContainer(ctx)
		if err != nil {
			log.Printf("警告: 检查容器状态失败: %v", err)
		} else if !exists {
			return fmt.Errorf("容器启动后不存在")
		} else if !running {
			return fmt.Errorf("容器启动后未运行")
		}
		log.Printf("ADB Gateway 容器已就绪")
		return nil
	}

	// 3. 容器不存在，创建配置和容器
	log.Printf("容器 %s 不存在，开始创建...", s.containerName)

	// 创建 adb_gateway 目录结构
	adbGatewayPath := filepath.Join(s.dataPath, "adb_gateway")
	dataDir := filepath.Join(adbGatewayPath, "data")
	configsDir := filepath.Join(dataDir, "configs")
	logsDir := filepath.Join(dataDir, "logs")
	dbDir := filepath.Join(dataDir, "database")

	for _, dir := range []string{adbGatewayPath, dataDir, configsDir, logsDir, dbDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", dir, err)
		}
	}
	log.Printf("已创建目录结构: %s", adbGatewayPath)

	// 生成配置文件
	configPath := filepath.Join(configsDir, "config.json")
	if err := s.generateGatewayConfig(configPath); err != nil {
		return fmt.Errorf("生成配置文件失败: %w", err)
	}
	log.Printf("已生成配置文件: %s", configPath)

	// 从配置中构建端口范围数组（用于初始化）
	gatewayConfig := configs.AppConfig.AdbGateway.GatewayConfig
	portRanges := []string{fmt.Sprintf("%d-%d", gatewayConfig.Listen.MinPort, gatewayConfig.Listen.MaxPort)}

	// 创建并启动容器
	if err := s.createAndStartContainer(ctx, dataDir, portRanges); err != nil {
		return fmt.Errorf("创建并启动容器失败: %w", err)
	}

	// 保存 ADB Gateway 信息到数据库
	if err := s.saveOrUpdateGatewayInfo(ctx); err != nil {
		log.Printf("警告: 保存 ADB Gateway 信息失败: %v", err)
	}

	log.Printf("ADB Gateway 容器初始化成功")
	return nil
}

// ensureDataPath 确保数据目录存在
func (s *AdbGatewayInitService) ensureDataPath() error {
	if s.dataPath == "" {
		return fmt.Errorf("data_path 未配置")
	}

	// 将相对路径转换为绝对路径
	absPath, err := filepath.Abs(s.dataPath)
	if err != nil {
		return fmt.Errorf("获取绝对路径失败: %w", err)
	}
	s.dataPath = absPath

	if _, err := os.Stat(s.dataPath); os.IsNotExist(err) {
		if err := os.MkdirAll(s.dataPath, 0755); err != nil {
			return fmt.Errorf("创建目录失败: %w", err)
		}
		log.Printf("已创建数据目录: %s", s.dataPath)
	} else {
		log.Printf("数据目录已存在: %s", s.dataPath)
	}

	return nil
}

// checkContainer 检查容器是否存在及运行状态
func (s *AdbGatewayInitService) checkContainer(ctx context.Context) (exists bool, running bool, err error) {
	containers, err := s.dockerService.ListContainers(ctx, true)
	if err != nil {
		return false, false, err
	}

	for _, c := range containers {
		for _, name := range c.Names {
			// Docker 容器名称前面会有 "/"
			cleanName := strings.TrimPrefix(name, "/")
			if cleanName == s.containerName {
				return true, c.State == "running", nil
			}
		}
	}

	return false, false, nil
}

// startContainer 启动已存在的容器
func (s *AdbGatewayInitService) startContainer(ctx context.Context) error {
	containers, err := s.dockerService.ListContainers(ctx, true)
	if err != nil {
		return err
	}

	for _, c := range containers {
		for _, name := range c.Names {
			cleanName := strings.TrimPrefix(name, "/")
			if cleanName == s.containerName {
				return s.dockerService.StartContainer(ctx, c.ID)
			}
		}
	}

	return fmt.Errorf("容器不存在")
}

// generateGatewayConfig 生成 ADB Gateway 配置文件
func (s *AdbGatewayInitService) generateGatewayConfig(configPath string) error {
	gatewayConfig := configs.AppConfig.AdbGateway.GatewayConfig
	// 从配置中构建端口范围数组
	portRanges := []string{fmt.Sprintf("%d-%d", gatewayConfig.Listen.MinPort, gatewayConfig.Listen.MaxPort)}
	ctx := context.Background()
	return s.generateGatewayConfigWithPortRanges(ctx, configPath, portRanges)
}

// generateGatewayConfigWithPortRanges 生成 ADB Gateway 配置文件（带端口范围数组参数）
func (s *AdbGatewayInitService) generateGatewayConfigWithPortRanges(ctx context.Context, configPath string, portRanges []string) error {
	gatewayConfig := configs.AppConfig.AdbGateway.GatewayConfig

	// 从端口范围中提取最小和最大端口
	minPort, maxPort, err := extractMinMaxPorts(portRanges)
	if err != nil {
		// 如果解析失败，使用配置中的默认值
		minPort = gatewayConfig.Listen.MinPort
		maxPort = gatewayConfig.Listen.MaxPort
		log.Printf("警告: 解析端口范围失败，使用默认值: %d-%d", minPort, maxPort)
	}

	// 处理上传 URL：如果包含 host.docker.internal，则替换为 bridge 网络的网关 IP
	uploadURL := gatewayConfig.Upload.URL
	if strings.Contains(uploadURL, "host.docker.internal") {
		bridgeGatewayIP, err := s.getBridgeNetworkGateway(ctx)
		if err != nil {
			log.Printf("警告: 获取 bridge 网络网关 IP 失败: %v，保持原 URL", err)
		} else {
			uploadURL = strings.ReplaceAll(uploadURL, "host.docker.internal", bridgeGatewayIP)
			log.Printf("已将上传 URL 中的 host.docker.internal 替换为 %s", bridgeGatewayIP)
		}
	}

	// 构建配置对象
	config := map[string]interface{}{
		"gateway_id": gatewayConfig.GatewayID,
		"upload": map[string]interface{}{
			"enabled": gatewayConfig.Upload.Enabled,
			"url":     uploadURL,
			"token":   gatewayConfig.Upload.Token,
		},
		"log": map[string]interface{}{
			"max_days":                      gatewayConfig.Log.MaxDays,
			"pending_warn_interval_minutes": gatewayConfig.Log.PendingWarnIntervalMinutes,
		},
		"database": map[string]interface{}{
			"path": gatewayConfig.Database.Path,
		},
		"listen": map[string]interface{}{
			"min_port": minPort,
			"max_port": maxPort,
		},
	}

	// 写入文件
	file, err := os.Create(configPath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(config)
}

// createAndStartContainer 创建并启动容器
// portRanges: 端口范围数组，如 ["8080", "15555-25555"]
func (s *AdbGatewayInitService) createAndStartContainer(ctx context.Context, dataDir string, portRanges []string) error {
	// 获取数据目录的绝对路径
	absDataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return fmt.Errorf("获取绝对路径失败: %w", err)
	}

	// 获取镜像名称，如果未配置则使用默认值
	gatewayConfig := configs.AppConfig.AdbGateway
	imageName := gatewayConfig.Image
	if imageName == "" {
		imageName = "adb-gateway:latest"
		log.Printf("未配置镜像名称，使用默认值: %s", imageName)
	}

	// 获取端口配置，如果未配置则使用默认值
	apiPort := gatewayConfig.GatewayAPIPort
	if apiPort <= 0 {
		apiPort = 8080
		log.Printf("未配置 API 端口，使用默认值: %d", apiPort)
	}

	// 获取主机地址，如果未配置则使用默认值
	gatewayHost := gatewayConfig.GatewayHost
	if gatewayHost == "" {
		gatewayHost = "127.0.0.1"
		log.Printf("未配置主机地址，使用默认值: %s", gatewayHost)
	}

	// 检查镜像是否存在，不存在则拉取
	exists, err := s.dockerService.ImageExists(ctx, imageName)
	if err != nil {
		log.Printf("警告: 检查镜像是否存在失败: %v，将尝试拉取镜像", err)
		exists = false
	}

	if !exists {
		log.Printf("镜像 %s 不存在，开始拉取 ADB Gateway 镜像...", imageName)
		if err := s.dockerService.PullImage(ctx, imageName); err != nil {
			return fmt.Errorf("拉取镜像失败: %w", err)
		}
		log.Printf("镜像 %s 拉取成功", imageName)
	} else {
		log.Printf("镜像 %s 已存在，跳过拉取", imageName)
	}

	// 从端口范围生成端口映射
	portMappings, err := generatePortMappings(portRanges, gatewayHost)
	if err != nil {
		return fmt.Errorf("生成端口映射失败: %w", err)
	}

	// 添加 API 端口映射（如果不在 portRanges 中）
	apiPortStr := fmt.Sprintf("%d", apiPort)
	apiPortKey := fmt.Sprintf("%s:%s", gatewayHost, apiPortStr)
	if _, exists := portMappings[apiPortKey]; !exists {
		portMappings[apiPortKey] = apiPortStr
	}

	log.Printf("生成的端口映射: %v", portMappings)

	// 构建环境变量列表
	envVars := []string{
		"CONFIG_PATH=/data/configs/config.json",
		"LOG_PATH=/data/logs/adb-gateway.log",
		"LOG_LEVEL=info",
		fmt.Sprintf("API_LISTEN=0.0.0.0:%d", apiPort),
	}

	// 如果配置了 gateway_token，注入到 ADB_GATEWAY_TOKEN 环境变量
	if gatewayConfig.GatewayToken != "" {
		envVars = append(envVars, fmt.Sprintf("ADB_GATEWAY_TOKEN=%s", gatewayConfig.GatewayToken))
	}

	containerConfig := &ContainerCreateConfig{
		Image:         imageName,
		Name:          s.containerName,
		NetworkMode:   getNetworkName(),
		RestartPolicy: "unless-stopped",
		Env:           envVars,
		Ports:         portMappings,
		Volumes: map[string]string{
			absDataDir: "/data",
		},
		// 添加 host.docker.internal 映射，支持 Linux 系统
		ExtraHosts: []string{
			"host.docker.internal:host-gateway",
		},
	}

	// 创建容器
	log.Println("创建容器...")
	containerID, err := s.dockerService.CreateContainer(ctx, containerConfig)
	if err != nil {
		return fmt.Errorf("创建容器失败: %w", err)
	}

	log.Printf("容器已创建，ID: %s", containerID)

	// 启动容器
	log.Println("启动容器...")
	if err := s.dockerService.StartContainer(ctx, containerID); err != nil {
		return fmt.Errorf("启动容器失败: %w", err)
	}

	log.Printf("容器 %s 已成功启动，等待服务就绪...", s.containerName)

	// 将容器连接到默认的 bridge 网络，以便访问宿主机
	log.Println("将容器连接到默认 bridge 网络...")
	if err := s.connectToBridgeNetwork(ctx, containerID); err != nil {
		log.Printf("警告: 连接容器到 bridge 网络失败: %v（可能已连接）", err)
	} else {
		log.Println("容器已成功连接到 bridge 网络")
	}

	// 等待几秒钟让容器内的服务启动
	waitTime := 5 * time.Second
	log.Printf("等待 %v 让 ADB Gateway 服务启动...", waitTime)
	time.Sleep(waitTime)

	// 检查容器是否仍在运行
	exists, running, err := s.checkContainer(ctx)
	if err != nil {
		log.Printf("警告: 检查容器状态失败: %v", err)
	} else if !exists {
		return fmt.Errorf("容器启动后不存在")
	} else if !running {
		return fmt.Errorf("容器启动后未运行")
	}

	log.Printf("ADB Gateway 容器已就绪")
	return nil
}

// saveOrUpdateGatewayInfo 保存或更新 ADB Gateway 信息到数据库
func (s *AdbGatewayInitService) saveOrUpdateGatewayInfo(ctx context.Context) error {
	// 查找容器信息
	containers, err := s.dockerService.ListContainers(ctx, true)
	if err != nil {
		return fmt.Errorf("查询容器列表失败: %w", err)
	}

	var containerID string
	for _, c := range containers {
		for _, name := range c.Names {
			cleanName := strings.TrimPrefix(name, "/")
			if cleanName == s.containerName {
				containerID = c.ID
				break
			}
		}
		if containerID != "" {
			break
		}
	}

	if containerID == "" {
		return fmt.Errorf("未找到容器: %s", s.containerName)
	}

	// 获取配置信息
	gatewayConfig := configs.AppConfig.AdbGateway
	apiPort := gatewayConfig.GatewayAPIPort
	if apiPort <= 0 {
		apiPort = 8080
	}
	gatewayHost := gatewayConfig.GatewayHost
	if gatewayHost == "" {
		gatewayHost = "127.0.0.1"
	}
	imageName := gatewayConfig.Image
	if imageName == "" {
		imageName = "adb-gateway:latest"
	}

	// 从配置中构建端口范围数组
	gatewayConfigConfig := gatewayConfig.GatewayConfig
	portRanges := []string{fmt.Sprintf("%d-%d", gatewayConfigConfig.Listen.MinPort, gatewayConfigConfig.Listen.MaxPort)}

	// 创建或更新记录
	adbGateway := &models.AdbGateway{
		ID:             "default", // 只存储一个默认的 ADB Gateway
		ContainerName:  s.containerName,
		ContainerID:    containerID,
		Image:          imageName,
		GatewayHost:    gatewayHost,
		GatewayAPIPort: apiPort,
		Status:         "running",
		UpdatedAt:      time.Now(),
	}

	// 设置端口范围
	if err := adbGateway.SetPortRanges(portRanges); err != nil {
		return fmt.Errorf("设置端口范围失败: %w", err)
	}

	// 使用 FirstOrCreate，如果不存在则创建，存在则更新
	var existing models.AdbGateway
	if err := models.DB.Where("id = ?", "default").First(&existing).Error; err != nil {
		// 不存在，创建新记录
		adbGateway.CreatedAt = time.Now()
		if err := models.DB.Create(adbGateway).Error; err != nil {
			return fmt.Errorf("创建 ADB Gateway 记录失败: %w", err)
		}
		log.Printf("已创建 ADB Gateway 记录: %s", s.containerName)
	} else {
		// 存在，更新记录
		adbGateway.CreatedAt = existing.CreatedAt
		if err := models.DB.Model(&existing).Updates(adbGateway).Error; err != nil {
			return fmt.Errorf("更新 ADB Gateway 记录失败: %w", err)
		}
		log.Printf("已更新 ADB Gateway 记录: %s", s.containerName)
	}

	return nil
}

// UpdateContainerConfig 更新 ADB Gateway 容器配置（如端口范围）
// portRanges: 端口范围数组，如 ["8080", "15555-25555"]
func (s *AdbGatewayInitService) UpdateContainerConfig(ctx context.Context, portRanges []string) error {
	if len(portRanges) == 0 {
		return fmt.Errorf("端口范围数组不能为空")
	}

	// 验证端口范围格式
	if _, err := parsePortRanges(portRanges); err != nil {
		return fmt.Errorf("端口范围格式错误: %w", err)
	}

	log.Printf("开始更新 ADB Gateway 容器配置，端口范围: %v", portRanges)

	// 1. 检查容器是否存在
	exists, running, err := s.checkContainer(ctx)
	if err != nil {
		return fmt.Errorf("检查容器失败: %w", err)
	}

	if !exists {
		return fmt.Errorf("容器不存在，无法更新配置")
	}

	// 2. 停止并删除旧容器
	if running {
		log.Printf("停止容器 %s...", s.containerName)
		containers, err := s.dockerService.ListContainers(ctx, true)
		if err != nil {
			return fmt.Errorf("查询容器列表失败: %w", err)
		}

		var containerID string
		for _, c := range containers {
			for _, name := range c.Names {
				cleanName := strings.TrimPrefix(name, "/")
				if cleanName == s.containerName {
					containerID = c.ID
					break
				}
			}
			if containerID != "" {
				break
			}
		}

		if containerID == "" {
			return fmt.Errorf("未找到容器: %s", s.containerName)
		}

		// 停止容器
		timeout := 10
		if err := s.dockerService.StopContainer(ctx, containerID, &timeout); err != nil {
			log.Printf("警告: 停止容器失败: %v", err)
		}

		// 删除容器
		log.Printf("删除容器 %s...", s.containerName)
		if err := s.dockerService.RemoveContainer(ctx, containerID, false); err != nil {
			return fmt.Errorf("删除容器失败: %w", err)
		}
	}

	// 3. 更新配置文件
	adbGatewayPath := filepath.Join(s.dataPath, "adb_gateway")
	dataDir := filepath.Join(adbGatewayPath, "data")
	configsDir := filepath.Join(dataDir, "configs")
	configPath := filepath.Join(configsDir, "config.json")

	log.Printf("更新配置文件: %s", configPath)
	if err := s.generateGatewayConfigWithPortRanges(ctx, configPath, portRanges); err != nil {
		return fmt.Errorf("更新配置文件失败: %w", err)
	}

	// 4. 使用新配置重新创建并启动容器
	if err := s.createAndStartContainer(ctx, dataDir, portRanges); err != nil {
		return fmt.Errorf("重新创建容器失败: %w", err)
	}

	// 5. 更新数据库记录
	if err := s.saveOrUpdateGatewayInfoWithPortRanges(ctx, portRanges); err != nil {
		log.Printf("警告: 更新数据库记录失败: %v", err)
	}

	log.Printf("ADB Gateway 容器配置更新成功")
	return nil
}

// saveOrUpdateGatewayInfoWithPortRanges 保存或更新 ADB Gateway 信息到数据库（带端口范围数组）
func (s *AdbGatewayInitService) saveOrUpdateGatewayInfoWithPortRanges(ctx context.Context, portRanges []string) error {
	// 查找容器信息
	containers, err := s.dockerService.ListContainers(ctx, true)
	if err != nil {
		return fmt.Errorf("查询容器列表失败: %w", err)
	}

	var containerID string
	for _, c := range containers {
		for _, name := range c.Names {
			cleanName := strings.TrimPrefix(name, "/")
			if cleanName == s.containerName {
				containerID = c.ID
				break
			}
		}
		if containerID != "" {
			break
		}
	}

	if containerID == "" {
		return fmt.Errorf("未找到容器: %s", s.containerName)
	}

	// 获取配置信息
	gatewayConfig := configs.AppConfig.AdbGateway
	apiPort := gatewayConfig.GatewayAPIPort
	if apiPort <= 0 {
		apiPort = 8080
	}
	gatewayHost := gatewayConfig.GatewayHost
	if gatewayHost == "" {
		gatewayHost = "127.0.0.1"
	}
	imageName := gatewayConfig.Image
	if imageName == "" {
		imageName = "adb-gateway:latest"
	}

	// 创建或更新记录
	adbGateway := &models.AdbGateway{
		ID:             "default", // 只存储一个默认的 ADB Gateway
		ContainerName:  s.containerName,
		ContainerID:    containerID,
		Image:          imageName,
		GatewayHost:    gatewayHost,
		GatewayAPIPort: apiPort,
		Status:         "running",
		UpdatedAt:      time.Now(),
	}

	// 设置端口范围
	if err := adbGateway.SetPortRanges(portRanges); err != nil {
		return fmt.Errorf("设置端口范围失败: %w", err)
	}

	// 使用 FirstOrCreate，如果不存在则创建，存在则更新
	var existing models.AdbGateway
	if err := models.DB.Where("id = ?", "default").First(&existing).Error; err != nil {
		// 不存在，创建新记录
		adbGateway.CreatedAt = time.Now()
		if err := models.DB.Create(adbGateway).Error; err != nil {
			return fmt.Errorf("创建 ADB Gateway 记录失败: %w", err)
		}
		log.Printf("已创建 ADB Gateway 记录: %s", s.containerName)
	} else {
		// 存在，更新记录
		adbGateway.CreatedAt = existing.CreatedAt
		if err := models.DB.Model(&existing).Updates(adbGateway).Error; err != nil {
			return fmt.Errorf("更新 ADB Gateway 记录失败: %w", err)
		}
		log.Printf("已更新 ADB Gateway 记录: %s", s.containerName)
	}

	return nil
}
