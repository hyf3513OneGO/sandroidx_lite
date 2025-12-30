package services

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"
	"sandroidx.com/sandroidx_lite/clients"
	"sandroidx.com/sandroidx_lite/configs"
	"sandroidx.com/sandroidx_lite/models"
	"sandroidx.com/sandroidx_lite/utils"
)

// SandboxService Sandbox 服务接口
type SandboxService interface {
	// 创建 Sandbox
	CreateSandbox(ctx context.Context, spec SandboxCreateSpec) (*models.Sandbox, error)
	// 获取 Sandbox
	GetSandbox(id string) (*models.Sandbox, error)
	// 获取 Sandbox 对应的宿主机 ADB 设备地址（基于映射）
	GetAdbDeviceAddress(ctx context.Context, sandboxID string) (string, error)
	// 确保 scrcpy forward 已就绪（如有需要会自动重建），返回端口
	EnsureScrcpyForward(ctx context.Context, sandboxID string) (int, error)
	// 实际启动 scrcpy-server（由 ScrcpyService 在需要时调用）
	SetupScrcpyForwardIfNeeded(ctx context.Context, sandboxID string) (int, error)
	// 获取 Sandbox 及其 volumes 信息
	GetSandboxWithVolumes(id string) (*SandboxWithVolumes, error)
	// 列出所有 Sandboxes
	ListSandboxes() ([]models.Sandbox, error)
	// 删除 Sandbox (volumesToDelete: 要删除的Volume ID列表，为空则不删除任何Volume)
	DeleteSandbox(ctx context.Context, id string, volumesToDelete []string) error
	// 启动 Sandbox
	StartSandbox(ctx context.Context, id string) error
	// 停止 Sandbox
	StopSandbox(ctx context.Context, id string) error
	// 获取 Sandbox 的挂载卷列表
	GetSandboxVolumes(sandboxID string) ([]models.SandboxVolume, error)
	// 安装 APK 到 Sandbox
	InstallApk(ctx context.Context, sandboxID string, apkID string) error
}

// ApkConfig APK 配置（用于 sandbox 和 template）
type ApkConfig struct {
	Name        string `json:"name"`         // APK 名称
	PackageName string `json:"package_name"` // 包名
	Version     string `json:"version"`      // 版本
	URL         string `json:"url"`          // 远程 URL（type=remote 时使用）
	URLStr      string `json:"url_str"`      // 远程 URL（向后兼容，优先使用 url）
	Type        string `json:"type"`         // remote 或 local
}

// SandboxCreateSpec Sandbox 创建规格
type SandboxCreateSpec struct {
	Type       string               `json:"type"`       // phone/redroid
	Image      string               `json:"image"`      // 镜像名称
	Mounts     []models.MountConfig `json:"mounts"`     // 挂载配置
	Ports      []string             `json:"ports"`      // 端口列表
	Privileged bool                 `json:"privileged"` // 是否特权模式
	Args       []string             `json:"args"`       // 启动参数
	// apks: APK 安装配置列表，会在 setup_adb_commands 之前执行安装
	Apks []ApkConfig `json:"apks"`
	// setup_adb_commands: 容器启动后要依次执行的 ADB 子命令列表（不含 "adb" 前缀）
	SetupAdbCommands []string          `json:"setup_adb_commands"`
	Envs             map[string]string `json:"envs"` // 环境变量
}

// sandboxService Sandbox 服务实现
type sandboxService struct {
	dockerService     DockerService
	adbGatewayService AdbGatewayService
	apkService        ApkService
	dataPath          string
}

// NewSandboxService 创建新的 Sandbox 服务
func NewSandboxService(dockerService DockerService, adbGatewayService AdbGatewayService, apkService ApkService) SandboxService {
	return &sandboxService{
		dockerService:     dockerService,
		adbGatewayService: adbGatewayService,
		apkService:        apkService,
		dataPath:          configs.AppConfig.Server.DataPath,
	}
}

// CreateSandbox 创建 Sandbox（异步）
func (s *sandboxService) CreateSandbox(ctx context.Context, spec SandboxCreateSpec) (*models.Sandbox, error) {
	log.Println("开始创建 Sandbox...")

	// 1. 生成 Sandbox ID
	sandboxID := utils.GenerateSandboxID()
	log.Printf("生成 Sandbox ID: %s", sandboxID)

	// 1.1 确保共享 APK 卷存在，并将其挂载到 Sandbox
	apksVolumeID, _, err := EnsureSharedApkVolume(s.dataPath)
	if err != nil {
		return nil, fmt.Errorf("准备共享 APK 卷失败: %w", err)
	}
	spec.Mounts = append(spec.Mounts, models.MountConfig{
		Volume:        apksVolumeID,
		ContainerPath: "/data/local/tmp/sandroidx/apks",
		ReadOnly:      false,
	})

	// 2. 创建 Sandbox 数据库记录
	sandbox := &models.Sandbox{
		ID:               sandboxID,
		Type:             spec.Type,
		Image:            spec.Image,
		Mounts:           spec.Mounts,
		Ports:            spec.Ports,
		Privileged:       spec.Privileged,
		Args:             spec.Args,
		SetupAdbCommands: models.StringSlice(spec.SetupAdbCommands),
		Envs:             models.StringMap(spec.Envs),
		ContainerName:    sandboxID,
		Status:           "creating",
	}

	if err := models.DB.Create(sandbox).Error; err != nil {
		return nil, fmt.Errorf("创建 Sandbox 数据库记录失败: %w", err)
	}
	log.Printf("已创建 Sandbox 数据库记录: %s", sandboxID)

	// 3. 在后台 goroutine 中异步执行容器创建
	go s.createSandboxAsync(sandboxID, spec)

	return sandbox, nil
}

// createSandboxAsync 异步创建 Sandbox 容器
func (s *sandboxService) createSandboxAsync(sandboxID string, spec SandboxCreateSpec) {
	ctx := context.Background()
	log.Printf("[异步] 开始为 Sandbox %s 创建容器...", sandboxID)

	// 准备挂载绑定
	binds, err := s.createMountDirectories(sandboxID, spec.Mounts)
	if err != nil {
		s.updateSandboxStatus(sandboxID, "failed", err.Error())
		log.Printf("[异步] Sandbox %s 创建失败: %v", sandboxID, err)
		return
	}

	// 准备环境变量
	envVars := []string{}
	if spec.Envs != nil {
		for key, value := range spec.Envs {
			envVars = append(envVars, fmt.Sprintf("%s=%s", key, value))
		}
	}

	// 准备端口映射
	portBindings := make(map[string]string)
	for _, port := range spec.Ports {
		// 支持多种格式:
		// - "5555" -> 自动分配宿主机端口映射到容器 5555
		// - "8080:5555" -> 宿主机 8080 映射到容器 5555
		// - "5555-5560" -> 端口范围 (宿主机 5555-5560 映射到容器 5555-5560)

		if strings.Contains(port, ":") {
			// 已指定宿主机端口，直接使用
			portBindings[port] = port
		} else if strings.Contains(port, "-") {
			// 端口范围，直接使用
			portBindings[port] = port
		} else {
			// 单个端口，自动分配宿主机端口
			// Docker API 使用空字符串表示自动分配宿主机端口
			// key 格式: containerPort, value 也是 containerPort
			// 在 createContainerWithAPI 中会处理为 HostPort=""（自动分配）
			portBindings[port] = port
			log.Printf("[异步] 端口 %s 将自动分配宿主机端口", port)
		}
	}

	// 为 scrcpy forward 分配一个端口（监听在 127.0.0.1）
	// 注意：这个端口不需要映射到容器，它是用于 adb forward 的宿主机端口
	scrcpyForwardPort, err := s.findAvailableScrcpyPort()
	if err != nil {
		log.Printf("[异步] 警告: 无法分配 scrcpy forward 端口: %v", err)
		scrcpyForwardPort = 0 // 使用 0 表示未分配
	} else {
		log.Printf("[异步] 分配 scrcpy forward 端口: %d (不映射到容器)", scrcpyForwardPort)
	}

	// 检查镜像是否存在，不存在则拉取
	exists, err := s.dockerService.ImageExists(ctx, spec.Image)
	if err != nil {
		log.Printf("[异步] 警告: 检查镜像是否存在失败: %v，将尝试拉取镜像", err)
		exists = false
	}

	if !exists {
		log.Printf("[异步] 镜像 %s 不存在，开始拉取...", spec.Image)
		if err := s.dockerService.PullImage(ctx, spec.Image); err != nil {
			s.updateSandboxStatus(sandboxID, "failed", fmt.Sprintf("拉取镜像失败: %v", err))
			log.Printf("[异步] Sandbox %s 创建失败: 拉取镜像失败: %v", sandboxID, err)
			return
		}
		log.Printf("[异步] 镜像 %s 拉取成功", spec.Image)
	} else {
		log.Printf("[异步] 镜像 %s 已存在，跳过拉取", spec.Image)
	}

	// 创建容器
	containerConfig := &ContainerCreateConfig{
		Image:         spec.Image,
		Name:          sandboxID,
		NetworkMode:   getNetworkName(),
		RestartPolicy: "no",
		Env:           envVars,
		Binds:         binds,
		Ports:         portBindings,
		Privileged:    spec.Privileged,
		Command:       spec.Args, // 将 args 作为启动命令
	}

	containerID, err := s.dockerService.CreateContainer(ctx, containerConfig)
	if err != nil {
		s.updateSandboxStatus(sandboxID, "failed", fmt.Sprintf("创建容器失败: %v", err))
		log.Printf("[异步] Sandbox %s 创建失败: %v", sandboxID, err)
		return
	}

	log.Printf("[异步] 已创建容器: %s (ID: %s)", sandboxID, containerID)

	// 更新数据库中的容器 ID
	if err := models.DB.Model(&models.Sandbox{}).
		Where("id = ?", sandboxID).
		Updates(map[string]interface{}{
			"container_id": containerID,
			"status":       "creating",
			"last_error":   "",
		}).Error; err != nil {
		log.Printf("警告: 更新 Sandbox %s 容器 ID 失败: %v", sandboxID, err)
	}

	// 启动容器
	log.Printf("[异步] 启动容器: %s", containerID)
	if err := s.dockerService.StartContainer(ctx, containerID); err != nil {
		s.updateSandboxStatus(sandboxID, "failed", fmt.Sprintf("启动容器失败: %v", err))
		log.Printf("[异步] Sandbox %s 启动失败: %v", sandboxID, err)
		return
	}

	// 创建 ADB Gateway 映射（两个映射：系统和 agent/user）
	if s.adbGatewayService != nil {
		upstream := fmt.Sprintf("%s:5555", sandboxID)

		// 1. 创建系统映射（用于 scrcpy 和系统操作）
		systemMappingName := fmt.Sprintf("sandbox-%s-system", sandboxID)
		systemMappingSpec := clients.MappingSpec{
			Name:     systemMappingName,
			Note:     fmt.Sprintf("Sandbox %s 的系统 ADB 映射（用于 scrcpy 和系统操作）", sandboxID),
			Upstream: upstream,
		}

		systemMapping, err := s.adbGatewayService.CreateMapping(systemMappingSpec)
		if err != nil {
			log.Printf("[异步] 警告: 创建系统 ADB Gateway 映射失败: %v", err)
		} else {
			log.Printf("[异步] 成功创建系统 ADB Gateway 映射: %s (listen: %s, upstream: %s)", systemMapping.ID, systemMapping.Listen, systemMapping.Upstream)
			// 保存系统映射ID到数据库
			if err := models.DB.Model(&models.Sandbox{}).
				Where("id = ?", sandboxID).
				Update("adb_mapping_id", systemMapping.ID).Error; err != nil {
				log.Printf("[异步] 警告: 保存系统 ADB 映射 ID 失败: %v", err)
			}
		}

		// 2. 创建 Agent/User 映射（用于记录 agent 和用户在 scrcpy player 上的操作）
		agentUserMappingName := fmt.Sprintf("sandbox-%s-agent-user", sandboxID)
		agentUserMappingSpec := clients.MappingSpec{
			Name:     agentUserMappingName,
			Note:     fmt.Sprintf("Sandbox %s 的 Agent/User ADB 映射（用于记录操作）", sandboxID),
			ToID:     sandboxID, // 设置 ToID 为 sandbox ID，用于标识这是 agent/user 映射
			Upstream: upstream,
		}

		agentUserMapping, err := s.adbGatewayService.CreateMapping(agentUserMappingSpec)
		if err != nil {
			log.Printf("[异步] 警告: 创建 Agent/User ADB Gateway 映射失败: %v", err)
		} else {
			log.Printf("[异步] 成功创建 Agent/User ADB Gateway 映射: %s (listen: %s, upstream: %s)", agentUserMapping.ID, agentUserMapping.Listen, agentUserMapping.Upstream)
			// 保存 Agent/User 映射ID到数据库
			if err := models.DB.Model(&models.Sandbox{}).
				Where("id = ?", sandboxID).
				Update("agent_user_mapping_id", agentUserMapping.ID).Error; err != nil {
				log.Printf("[异步] 警告: 保存 Agent/User ADB 映射 ID 失败: %v", err)
			}
		}
	}

	// 设置 scrcpy forward（如果分配了端口）
	if scrcpyForwardPort > 0 {
		// 先保存端口到数据库，这样即使 setup 失败也能记录端口
		if err := models.DB.Model(&models.Sandbox{}).
			Where("id = ?", sandboxID).
			Update("scrcpy_forward_port", scrcpyForwardPort).Error; err != nil {
			log.Printf("[异步] 警告: 保存 scrcpy forward 端口失败: %v", err)
		} else {
			log.Printf("[异步] 已保存 scrcpy forward 端口到数据库: %d", scrcpyForwardPort)
		}

		// 获取 ADB 映射地址（用于 adb connect）
		// 后端系统操作使用 AdbMappingID（系统映射）
		var sandbox models.Sandbox
		if err := models.DB.First(&sandbox, "id = ?", sandboxID).Error; err == nil && sandbox.AdbMappingID != "" {
			if mapping, err := s.adbGatewayService.GetMapping(sandbox.AdbMappingID); err == nil {
				adbDevice := mapping.Listen
				// 将 0.0.0.0 替换为 127.0.0.1
				if strings.HasPrefix(adbDevice, "0.0.0.0:") {
					adbDevice = strings.Replace(adbDevice, "0.0.0.0:", "127.0.0.1:", 1)
				}

				// 设置 scrcpy forward
				if err := s.setupScrcpyForward(ctx, sandboxID, adbDevice, scrcpyForwardPort); err != nil {
					log.Printf("[异步] 警告: 设置 scrcpy forward 失败: %v", err)
					// 不影响容器运行，但已经保存了端口，后续可以手动重试
				} else {
					log.Printf("[异步] 成功设置 scrcpy forward 端口: %d", scrcpyForwardPort)
				}
			} else {
				log.Printf("[异步] 警告: 获取 ADB 映射失败: %v，跳过 scrcpy forward 设置", err)
			}
		} else {
			log.Printf("[异步] 警告: 无法获取 sandbox 信息或 ADB 映射 ID，跳过 scrcpy forward 设置")
		}
	}

	// 安装 APK（在 setup_adb_commands 之前）
	if len(spec.Apks) > 0 {
		log.Printf("[异步] 开始安装 APK: %d 个", len(spec.Apks))
		s.updateSandboxStatus(sandboxID, "installing_apks", "")

		if err := s.installApks(ctx, sandboxID, spec.Apks); err != nil {
			s.updateSandboxStatus(sandboxID, "setup_failed", fmt.Sprintf("APK 安装失败: %v", err))
			log.Printf("[异步] APK 安装失败: %v", err)
			return
		}

		log.Printf("[异步] APK 安装完成")
	}

	// 执行 setup_adb_commands
	if len(spec.SetupAdbCommands) > 0 {
		log.Printf("[异步] 开始执行 setup_adb_commands: %d 条", len(spec.SetupAdbCommands))
		s.updateSandboxStatus(sandboxID, "setup", "")

		if err := s.runSetupAdbCommands(ctx, sandboxID, spec.SetupAdbCommands); err != nil {
			s.updateSandboxStatus(sandboxID, "setup_failed", err.Error())
			log.Printf("[异步] setup_adb_commands 执行失败: %v", err)
			return
		}

		log.Printf("[异步] setup_adb_commands 执行完成")
	}

	// 更新状态为 running
	s.updateSandboxStatus(sandboxID, "running", "")
	log.Printf("[异步] Sandbox %s 创建并启动完成，状态: running", sandboxID)
}

// installApks 安装 APK 列表
func (s *sandboxService) installApks(parentCtx context.Context, sandboxID string, apks []ApkConfig) error {
	if len(apks) == 0 {
		return nil
	}
	if s.apkService == nil {
		return fmt.Errorf("ApkService 未配置，无法安装 APK")
	}
	if s.adbGatewayService == nil {
		return fmt.Errorf("未配置 ADB Gateway，无法安装 APK")
	}

	// 为所有 APK 安装设置整体超时：每个 APK 60s，至少 120s
	timeout := time.Duration(len(apks)+1) * 60 * time.Second
	if timeout < 120*time.Second {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	// 后端系统操作使用 AdbMappingID（系统映射）
	var sandbox models.Sandbox
	if err := models.DB.WithContext(ctx).First(&sandbox, "id = ?", sandboxID).Error; err != nil {
		return fmt.Errorf("查询 sandbox 失败: %w", err)
	}
	if sandbox.AdbMappingID == "" {
		return fmt.Errorf("sandbox 未绑定系统 ADB 映射")
	}
	mapping, err := s.adbGatewayService.GetMapping(sandbox.AdbMappingID)
	if err != nil {
		return fmt.Errorf("获取 ADB 映射失败: %w", err)
	}
	adbDevice := mapping.Listen
	if adbDevice == "" {
		return fmt.Errorf("ADB 映射监听地址为空")
	}
	// 如果是 0.0.0.0:*，替换为 127.0.0.1:* 以便在宿主机使用
	if strings.HasPrefix(adbDevice, "0.0.0.0:") {
		adbDevice = strings.Replace(adbDevice, "0.0.0.0:", "127.0.0.1:", 1)
	}

	connectCmd := exec.CommandContext(ctx, "adb", "connect", adbDevice)
	if output, err := connectCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("adb connect 失败: %v, 输出: %s", err, strings.TrimSpace(string(output)))
	}

	// 等待设备与包管理器就绪
	if err := s.waitForDeviceReady(ctx, adbDevice); err != nil {
		return err
	}

	// 获取共享 APK 卷的容器内路径
	apksContainerPath := "/data/local/tmp/sandroidx/apks"

	for i, apkCfg := range apks {
		// 处理 URL 字段（优先使用 url，如果没有则使用 url_str）
		urlStr := apkCfg.URL
		if urlStr == "" {
			urlStr = apkCfg.URLStr
		}

		log.Printf("[异步] 准备安装 APK[%d/%d]: %s (包名: %s, 版本: %s)", i+1, len(apks), apkCfg.Name, apkCfg.PackageName, apkCfg.Version)

		// 查找或准备 APK（本地查找或下载）
		localPath, err := s.apkService.FindOrPrepareApk(apkCfg.PackageName, apkCfg.Version, urlStr, apkCfg.Name)
		if err != nil {
			return fmt.Errorf("准备 APK[%d] (%s) 失败: %w", i+1, apkCfg.Name, err)
		}

		// 将 APK 文件复制到容器内的共享目录
		// 获取容器 ID
		sandbox, err := s.GetSandbox(sandboxID)
		if err != nil {
			return fmt.Errorf("获取 Sandbox 信息失败: %w", err)
		}
		if sandbox.ContainerID == "" {
			return fmt.Errorf("Sandbox 容器 ID 为空")
		}

		fileName := filepath.Base(localPath)
		containerPath := filepath.Join(apksContainerPath, fileName)

		// 复制文件到容器
		copyCmd := exec.CommandContext(ctx, "docker", "cp", localPath, fmt.Sprintf("%s:%s", sandbox.ContainerID, containerPath))
		if output, err := copyCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("复制 APK 文件到容器失败: %v, 输出: %s", err, strings.TrimSpace(string(output)))
		}

		// 执行安装命令
		installCmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "shell", "pm", "install", "-r", containerPath)
		output, err := installCmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("安装 APK[%d] (%s) 失败: %v, 输出: %s", i+1, apkCfg.Name, err, strings.TrimSpace(string(output)))
		}

		log.Printf("[异步] APK[%d/%d] 安装成功: %s (包名: %s, 版本: %s)", i+1, len(apks), apkCfg.Name, apkCfg.PackageName, apkCfg.Version)
		if len(output) > 0 {
			log.Printf("[异步] 安装输出:\n%s", string(output))
		}

		// 保存 APK ID 到 Sandbox 记录中（用于重启后自动重新安装）
		// 通过包名和版本查找 APK ID
		if apkCfg.PackageName != "" && apkCfg.Version != "" {
			var apk models.Apk
			if err := models.DB.Where("package_name = ? AND version = ?", apkCfg.PackageName, apkCfg.Version).First(&apk).Error; err == nil {
				var sandbox models.Sandbox
				if err := models.DB.First(&sandbox, "id = ?", sandboxID).Error; err == nil {
					apkIDs := []string(sandbox.InstalledApkIDs)
					exists := false
					for _, id := range apkIDs {
						if id == apk.ID {
							exists = true
							break
						}
					}
					if !exists {
						apkIDs = append(apkIDs, apk.ID)
						if err := models.DB.Model(&models.Sandbox{}).
							Where("id = ?", sandboxID).
							Update("installed_apk_ids", models.StringSlice(apkIDs)).Error; err != nil {
							log.Printf("[异步] 警告: 保存已安装 APK ID 列表失败: %v", err)
						} else {
							log.Printf("[异步] 已保存 APK ID %s 到 Sandbox %s 的已安装列表", apk.ID, sandboxID)
						}
					}
				}
			}
		}
	}

	return nil
}

// InstallApk 安装单个 APK 到 Sandbox
func (s *sandboxService) InstallApk(ctx context.Context, sandboxID string, apkID string) error {
	if s.apkService == nil {
		return fmt.Errorf("ApkService 未配置，无法安装 APK")
	}
	if s.adbGatewayService == nil {
		return fmt.Errorf("未配置 ADB Gateway，无法安装 APK")
	}

	// 获取 APK 信息
	apk, err := s.apkService.GetApk(apkID)
	if err != nil {
		return fmt.Errorf("获取 APK 信息失败: %w", err)
	}

	// 检查 APK 是否有本地路径，如果没有且是 remote 类型，需要先下载
	if apk.Path == "" {
		if apk.Type == models.ApkTypeRemote {
			if apk.URL == "" {
				return fmt.Errorf("远程 APK 缺少 URL")
			}
			// 使用 FindOrPrepareApk 来下载或查找（会更新数据库中的 Path）
			_, err := s.apkService.FindOrPrepareApk(apk.PackageName, apk.Version, apk.URL, apk.Name)
			if err != nil {
				return fmt.Errorf("准备 APK 失败: %w", err)
			}
			// 重新获取 APK 信息以获取更新后的 Path
			apk, err = s.apkService.GetApk(apkID)
			if err != nil {
				return fmt.Errorf("重新获取 APK 信息失败: %w", err)
			}
		} else {
			return fmt.Errorf("本地 APK 缺少路径")
		}
	}

	// 构建 ApkConfig 并调用 installApks
	apkConfig := ApkConfig{
		Name:        apk.Name,
		PackageName: apk.PackageName,
		Version:     apk.Version,
		URL:         apk.URL,
		Type:        apk.Type,
	}

	// 使用 60 秒超时
	installCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// 调用 installApks，但只传入单个 APK
	apks := []ApkConfig{apkConfig}
	if err := s.installApks(installCtx, sandboxID, apks); err != nil {
		return fmt.Errorf("安装 APK 失败: %w", err)
	}

	// 保存 APK ID 到 Sandbox 记录中（用于重启后自动重新安装）
	var sandbox models.Sandbox
	if err := models.DB.First(&sandbox, "id = ?", sandboxID).Error; err != nil {
		log.Printf("警告: 查询 sandbox 失败，无法保存 APK ID: %v", err)
	} else {
		// 检查 APK ID 是否已存在
		apkIDs := []string(sandbox.InstalledApkIDs)
		exists := false
		for _, id := range apkIDs {
			if id == apkID {
				exists = true
				break
			}
		}
		// 如果不存在，添加到列表
		if !exists {
			apkIDs = append(apkIDs, apkID)
			if err := models.DB.Model(&models.Sandbox{}).
				Where("id = ?", sandboxID).
				Update("installed_apk_ids", models.StringSlice(apkIDs)).Error; err != nil {
				log.Printf("警告: 保存已安装 APK ID 列表失败: %v", err)
			} else {
				log.Printf("已保存 APK ID %s 到 Sandbox %s 的已安装列表", apkID, sandboxID)
			}
		}
	}

	return nil
}

// runSetupAdbCommands 依次执行 sandbox 配置的 ADB 初始化命令（不包含 "adb" 前缀）
func (s *sandboxService) runSetupAdbCommands(parentCtx context.Context, sandboxID string, commands []string) error {
	if len(commands) == 0 {
		return nil
	}
	if s.adbGatewayService == nil {
		return fmt.Errorf("未配置 ADB Gateway，无法执行 setup_adb_commands")
	}

	// 为所有命令设置整体超时：每条 30s，至少 60s
	timeout := time.Duration(len(commands)+1) * 30 * time.Second
	if timeout < 60*time.Second {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	// 后端系统操作使用 AdbMappingID（系统映射）
	var sandbox models.Sandbox
	if err := models.DB.WithContext(ctx).First(&sandbox, "id = ?", sandboxID).Error; err != nil {
		return fmt.Errorf("查询 sandbox 失败: %w", err)
	}
	if sandbox.AdbMappingID == "" {
		return fmt.Errorf("sandbox 未绑定系统 ADB 映射")
	}
	mapping, err := s.adbGatewayService.GetMapping(sandbox.AdbMappingID)
	if err != nil {
		return fmt.Errorf("获取 ADB 映射失败: %w", err)
	}
	adbDevice := mapping.Listen
	if adbDevice == "" {
		return fmt.Errorf("ADB 映射监听地址为空")
	}
	// 如果是 0.0.0.0:*，替换为 127.0.0.1:* 以便在宿主机使用
	if strings.HasPrefix(adbDevice, "0.0.0.0:") {
		adbDevice = strings.Replace(adbDevice, "0.0.0.0:", "127.0.0.1:", 1)
	}

	connectCmd := exec.CommandContext(ctx, "adb", "connect", adbDevice)
	if output, err := connectCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("adb connect 失败: %v, 输出: %s", err, strings.TrimSpace(string(output)))
	}

	// 等待设备与包管理器就绪，避免早期安装失败
	if err := s.waitForDeviceReady(ctx, adbDevice); err != nil {
		return err
	}

	for i, cmdStr := range commands {
		trimmed := strings.TrimSpace(cmdStr)
		if trimmed == "" {
			log.Printf("[异步] setup_adb_commands[%d] 为空，已跳过", i+1)
			continue
		}

		cmdParts := strings.Fields(trimmed)
		if len(cmdParts) == 0 {
			log.Printf("[异步] setup_adb_commands[%d] 为空，已跳过", i+1)
			continue
		}

		adbArgs := append([]string{"-s", adbDevice}, cmdParts...)
		cmd := exec.CommandContext(ctx, "adb", adbArgs...)

		output, err := cmd.CombinedOutput()
		if err != nil {
			exitCode := 0
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
			return fmt.Errorf("setup_adb_commands[%d] 执行失败 (exit=%d): %v, 输出: %s",
				i+1, exitCode, err, strings.TrimSpace(string(output)))
		}

		log.Printf("[异步] setup_adb_commands[%d] 执行成功: adb %s", i+1, strings.Join(cmdParts, " "))
		if len(output) > 0 {
			log.Printf("[异步] 命令输出:\n%s", string(output))
		}
	}

	return nil
}

// waitForDeviceReady 确保设备已连接、系统完成引导且包管理器可用
func (s *sandboxService) waitForDeviceReady(ctx context.Context, adbDevice string) error {
	log.Printf("[异步] 等待设备就绪: %s", adbDevice)

	// 1. wait-for-device
	waitCmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "wait-for-device")
	if output, err := waitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("adb wait-for-device 失败: %v, 输出: %s", err, strings.TrimSpace(string(output)))
	}

	// 2. 等待 sys.boot_completed = 1
	bootTicker := time.NewTicker(2 * time.Second)
	defer bootTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待 boot_completed 超时/被取消: %w", ctx.Err())
		case <-bootTicker.C:
			cmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "shell", "getprop", "sys.boot_completed")
			out, err := cmd.CombinedOutput()
			if err == nil && strings.TrimSpace(string(out)) == "1" {
				log.Printf("[异步] 设备 boot_completed=1")
				goto CHECK_PM
			}
			log.Printf("[异步] 等待 boot_completed 中... 输出: %s", strings.TrimSpace(string(out)))
		}
	}

CHECK_PM:
	// 3. 等待包管理器可用（pm path android 成功）
	pmTicker := time.NewTicker(2 * time.Second)
	defer pmTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待 PackageManager 就绪超时/被取消: %w", ctx.Err())
		case <-pmTicker.C:
			cmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "shell", "pm", "path", "android")
			out, err := cmd.CombinedOutput()
			if err == nil && len(bytes.TrimSpace(out)) > 0 {
				log.Printf("[异步] PackageManager 已就绪: %s", strings.TrimSpace(string(out)))
				// 解锁屏幕，避免卡在锁屏界面
				if err := s.unlockScreen(ctx, adbDevice); err != nil {
					log.Printf("[异步] 警告: 解锁屏幕失败（可能设备未锁屏）: %v", err)
				} else {
					log.Printf("[异步] 屏幕已解锁")
				}
				return nil
			}
			log.Printf("[异步] 等待 PackageManager 就绪中... 输出: %s", strings.TrimSpace(string(out)))
		}
	}
}

// unlockScreen 解锁屏幕，避免卡在锁屏界面
func (s *sandboxService) unlockScreen(ctx context.Context, adbDevice string) error {
	// 1. 唤醒屏幕（如果屏幕关闭）
	wakeCmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "shell", "input", "keyevent", "KEYCODE_WAKEUP")
	if output, err := wakeCmd.CombinedOutput(); err != nil {
		log.Printf("[异步] 唤醒屏幕失败: %v, 输出: %s", err, strings.TrimSpace(string(output)))
		// 继续尝试解锁，即使唤醒失败
	}
	time.Sleep(500 * time.Millisecond) // 等待屏幕响应

	// 2. 解除锁屏（Android 5.0+ 推荐方法）
	dismissCmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "shell", "wm", "dismiss-keyguard")
	output, err := dismissCmd.CombinedOutput()
	if err == nil {
		log.Printf("[异步] 使用 wm dismiss-keyguard 解除锁屏")
		return nil
	}
	log.Printf("[异步] wm dismiss-keyguard 失败，尝试备用方法: %s", strings.TrimSpace(string(output)))

	// 3. 备用方法：使用 input keyevent（适用于某些设备）
	menuCmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "shell", "input", "keyevent", "KEYCODE_MENU")
	output, err = menuCmd.CombinedOutput()
	if err == nil {
		log.Printf("[异步] 使用 KEYCODE_MENU 解除锁屏")
		time.Sleep(300 * time.Millisecond)
		return nil
	}
	log.Printf("[异步] KEYCODE_MENU 也失败: %s", strings.TrimSpace(string(output)))

	// 4. 最后尝试：向上滑动解锁（适用于滑动解锁）
	swipeCmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "shell", "input", "swipe", "500", "1500", "500", "500", "300")
	output, err = swipeCmd.CombinedOutput()
	if err == nil {
		log.Printf("[异步] 使用滑动解锁")
		time.Sleep(300 * time.Millisecond)
		return nil
	}

	// 如果所有方法都失败，返回错误（但不会阻止后续操作）
	return fmt.Errorf("所有解锁方法都失败，最后输出: %s", strings.TrimSpace(string(output)))
}

// updateSandboxStatus 更新 Sandbox 状态
func (s *sandboxService) updateSandboxStatus(sandboxID, status, lastError string) {
	if err := models.DB.Model(&models.Sandbox{}).
		Where("id = ?", sandboxID).
		Updates(map[string]interface{}{
			"status":     status,
			"last_error": lastError,
		}).Error; err != nil {
		log.Printf("更新 Sandbox %s 状态失败: %v", sandboxID, err)
	}
}

// GetSandbox 获取 Sandbox
func (s *sandboxService) GetSandbox(id string) (*models.Sandbox, error) {
	var sandbox models.Sandbox
	if err := models.DB.First(&sandbox, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("sandbox 不存在")
		}
		return nil, fmt.Errorf("查询 sandbox 失败: %w", err)
	}

	// 如果有容器 ID，查询容器状态（避免覆盖失败状态）
	if sandbox.ContainerID != "" && sandbox.Status != "failed" && sandbox.Status != "setup_failed" {
		ctx := context.Background()
		containerInfo, err := s.dockerService.GetContainer(ctx, sandbox.ContainerID)
		if err == nil && containerInfo != nil {
			// 更新状态
			if containerInfo.State.Running {
				sandbox.Status = "running"
			} else {
				sandbox.Status = "stopped"
			}
			// 保存到数据库
			models.DB.Model(&sandbox).Update("status", sandbox.Status)
		}
	}

	return &sandbox, nil
}

// GetAdbDeviceAddress 获取 Sandbox 对应的宿主机 ADB 访问地址（映射 listen）
func (s *sandboxService) GetAdbDeviceAddress(ctx context.Context, sandboxID string) (string, error) {
	if sandboxID == "" {
		return "", fmt.Errorf("sandbox ID 不能为空")
	}

	if s.adbGatewayService == nil {
		return "", fmt.Errorf("未配置 ADB Gateway 服务")
	}

	var sandbox models.Sandbox
	if err := models.DB.WithContext(ctx).First(&sandbox, "id = ?", sandboxID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", fmt.Errorf("sandbox 不存在")
		}
		return "", fmt.Errorf("查询 sandbox 失败: %w", err)
	}

	// 优先使用 AgentUserMappingID（用于记录操作），如果没有则使用 AdbMappingID（向后兼容）
	mappingID := sandbox.AgentUserMappingID
	if mappingID == "" {
		mappingID = sandbox.AdbMappingID
	}
	if mappingID == "" {
		return "", fmt.Errorf("sandbox 未绑定 ADB 映射")
	}

	mapping, err := s.adbGatewayService.GetMapping(mappingID)
	if err != nil {
		return "", fmt.Errorf("获取 ADB 映射失败: %w", err)
	}

	adbDevice := mapping.Listen
	if adbDevice == "" {
		return "", fmt.Errorf("ADB 映射监听地址为空")
	}

	// 如果是 0.0.0.0:*，替换为 127.0.0.1:* 以便在宿主机使用
	if strings.HasPrefix(adbDevice, "0.0.0.0:") {
		adbDevice = strings.Replace(adbDevice, "0.0.0.0:", "127.0.0.1:", 1)
	}

	return adbDevice, nil
}

// ListSandboxes 列出所有 Sandboxes
func (s *sandboxService) ListSandboxes() ([]models.Sandbox, error) {
	var sandboxes []models.Sandbox
	if err := models.DB.Find(&sandboxes).Error; err != nil {
		return nil, fmt.Errorf("查询 sandboxes 失败: %w", err)
	}
	return sandboxes, nil
}

// DeleteSandbox 删除 Sandbox
func (s *sandboxService) DeleteSandbox(ctx context.Context, id string, volumesToDelete []string) error {
	log.Printf("开始删除 Sandbox: %s", id)

	// 1. 查询 Sandbox
	var sandbox models.Sandbox
	if err := models.DB.First(&sandbox, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("sandbox 不存在")
		}
		return fmt.Errorf("查询 sandbox 失败: %w", err)
	}

	// 2. 删除 ADB Gateway 映射（系统和 agent/user）
	if s.adbGatewayService != nil {
		if sandbox.AdbMappingID != "" {
			log.Printf("删除系统 ADB Gateway 映射: %s", sandbox.AdbMappingID)
			if err := s.adbGatewayService.RemoveMapping(sandbox.AdbMappingID); err != nil {
				log.Printf("警告: 删除系统 ADB Gateway 映射失败: %v", err)
			}
		}
		if sandbox.AgentUserMappingID != "" {
			log.Printf("删除 Agent/User ADB Gateway 映射: %s", sandbox.AgentUserMappingID)
			if err := s.adbGatewayService.RemoveMapping(sandbox.AgentUserMappingID); err != nil {
				log.Printf("警告: 删除 Agent/User ADB Gateway 映射失败: %v", err)
			}
		}
		// 不影响删除流程
	}

	// 3. 如果有容器，先停止并删除容器
	if sandbox.ContainerID != "" {
		log.Printf("正在删除容器: %s", sandbox.ContainerID)
		// 尝试停止容器（忽略错误）
		timeout := 10
		_ = s.dockerService.StopContainer(ctx, sandbox.ContainerID, &timeout)

		// 强制删除容器
		if err := s.dockerService.RemoveContainer(ctx, sandbox.ContainerID, true); err != nil {
			log.Printf("警告: 删除容器失败: %v", err)
		}
	}

	// 4. 处理挂载关系和Volume删除
	var sandboxVolumes []models.SandboxVolume
	if err := models.DB.Where("sandbox_id = ?", id).Find(&sandboxVolumes).Error; err != nil {
		log.Printf("警告: 查询挂载关系失败: %v", err)
	}

	// 构建要删除的Volume ID集合
	volumesToDeleteMap := make(map[string]bool)
	for _, vid := range volumesToDelete {
		volumesToDeleteMap[vid] = true
	}

	// 处理每个挂载的Volume
	for _, sv := range sandboxVolumes {
		var vol models.Volume
		if err := models.DB.First(&vol, "id = ?", sv.VolumeID).Error; err != nil {
			log.Printf("警告: 查询Volume %s 失败: %v", sv.VolumeID, err)
			continue
		}

		// 检查是否在删除列表中
		shouldDelete := volumesToDeleteMap[vol.ID]

		if shouldDelete && vol.VolumeType == "user" {
			// 检查是否是只读卷，只读卷不应该删除本地目录
			if sv.ReadOnly {
				log.Printf("跳过只读卷 %s: 只读卷不允许删除本地目录", vol.ID)
				// 仍然删除 Volume 记录，但不删除本地目录
				if err := models.DB.Delete(&vol).Error; err != nil {
					log.Printf("警告: 删除Volume记录失败: %v", err)
				}
				continue
			}

			// 检查是否有其他 Sandbox 或 Agent 在使用该 Volume
			var otherSandboxUsageCount int64
			if err := models.DB.Model(&models.SandboxVolume{}).
				Where("volume_id = ? AND sandbox_id != ? AND status = ?", vol.ID, id, "active").
				Count(&otherSandboxUsageCount).Error; err != nil {
				log.Printf("警告: 查询Volume在Sandbox中的使用情况失败: %v", err)
				continue
			}

			var otherAgentUsageCount int64
			if err := models.DB.Model(&models.AgentVolume{}).
				Where("volume_id = ? AND status = ?", vol.ID, "active").
				Count(&otherAgentUsageCount).Error; err != nil {
				log.Printf("警告: 查询Volume在Agent中的使用情况失败: %v", err)
				continue
			}

			totalOtherUsage := otherSandboxUsageCount + otherAgentUsageCount

			if totalOtherUsage == 0 {
				// 没有其他 Sandbox 或 Agent 使用，可以删除
				log.Printf("删除用户卷: %s -> %s (无其他使用)", vol.ID, vol.HostPath)
				if err := os.RemoveAll(vol.HostPath); err != nil {
					log.Printf("警告: 删除卷目录失败: %v", err)
				}

				// 删除Volume记录
				if err := models.DB.Delete(&vol).Error; err != nil {
					log.Printf("警告: 删除Volume记录失败: %v", err)
				}
			} else {
				log.Printf("无法删除Volume %s: 仍有 %d 个其他实例在使用", vol.ID, totalOtherUsage)
			}
		} else if shouldDelete && vol.VolumeType == "system" {
			log.Printf("跳过系统卷 %s: 系统卷不允许删除", vol.ID)
		} else {
			log.Printf("保留Volume %s", vol.ID)
		}
	}

	// 5. 删除所有该 Sandbox 的 Sandbox-Volume 关系
	if err := models.DB.Where("sandbox_id = ?", id).Delete(&models.SandboxVolume{}).Error; err != nil {
		log.Printf("警告: 删除 Sandbox-Volume 关系失败: %v", err)
	}

	// 6. 删除相关的 ADB 命令日志
	// 通过 mapping_id 删除（系统和 agent/user 映射）
	if sandbox.AdbMappingID != "" {
		log.Printf("删除系统映射相关的 ADB 命令日志: %s", sandbox.AdbMappingID)
		if err := models.DB.Where("mapping_id = ?", sandbox.AdbMappingID).Delete(&models.AdbCommandLog{}).Error; err != nil {
			log.Printf("警告: 删除系统映射相关的 ADB 命令日志失败: %v", err)
		}
	}
	if sandbox.AgentUserMappingID != "" {
		log.Printf("删除 Agent/User 映射相关的 ADB 命令日志: %s", sandbox.AgentUserMappingID)
		if err := models.DB.Where("mapping_id = ?", sandbox.AgentUserMappingID).Delete(&models.AdbCommandLog{}).Error; err != nil {
			log.Printf("警告: 删除 Agent/User 映射相关的 ADB 命令日志失败: %v", err)
		}
	}
	// 通过 to_id 删除（如果 to_id 存储的是 sandbox ID）
	log.Printf("删除 to_id 相关的 ADB 命令日志: %s", id)
	if err := models.DB.Where("to_id = ?", id).Delete(&models.AdbCommandLog{}).Error; err != nil {
		log.Printf("警告: 删除 to_id 相关的 ADB 命令日志失败: %v", err)
	}
	// 通过 to 字段删除（如果 to 字段包含容器名，如 sandbox-xxx:5555）
	// 容器名通常是 sandbox ID，所以匹配 to 字段以 sandbox ID 开头的记录
	toPattern := id + ":"
	log.Printf("删除 to 字段相关的 ADB 命令日志: %s", toPattern)
	if err := models.DB.Where("to LIKE ?", toPattern+"%").Delete(&models.AdbCommandLog{}).Error; err != nil {
		log.Printf("警告: 删除 to 字段相关的 ADB 命令日志失败: %v", err)
	}

	// 7. 删除数据库记录
	if err := models.DB.Delete(&sandbox).Error; err != nil {
		return fmt.Errorf("删除 sandbox 数据库记录失败: %w", err)
	}

	log.Printf("Sandbox %s 已删除", id)
	return nil
}

// StartSandbox 启动 Sandbox
func (s *sandboxService) StartSandbox(ctx context.Context, id string) error {
	log.Printf("开始启动 Sandbox: %s", id)

	// 1. 查询 Sandbox
	var sandbox models.Sandbox
	if err := models.DB.First(&sandbox, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("sandbox 不存在")
		}
		return fmt.Errorf("查询 sandbox 失败: %w", err)
	}

	// 2. 检查是否有容器
	if sandbox.ContainerID == "" {
		return fmt.Errorf("sandbox 没有关联的容器")
	}

	// 3. 检查容器状态
	containerInfo, err := s.dockerService.GetContainer(ctx, sandbox.ContainerID)
	if err != nil {
		// 如果容器不存在，尝试重新创建
		if strings.Contains(err.Error(), "No such container") {
			log.Printf("容器不存在，尝试重新创建...")
			// 构建 spec 并重新创建
			spec := SandboxCreateSpec{
				Type:             sandbox.Type,
				Image:            sandbox.Image,
				Mounts:           sandbox.Mounts,
				Ports:            sandbox.Ports,
				Privileged:       sandbox.Privileged,
				Args:             sandbox.Args,
				SetupAdbCommands: []string(sandbox.SetupAdbCommands),
				Envs:             map[string]string(sandbox.Envs),
			}
			go s.createSandboxAsync(id, spec)
			return fmt.Errorf("容器不存在，正在重新创建")
		}
		return fmt.Errorf("查询容器状态失败: %w", err)
	}

	// 4. 如果已经在运行，直接返回
	if containerInfo.State.Running {
		s.updateSandboxStatus(id, "running", "")
		log.Printf("Sandbox %s 已经在运行中", id)
		return nil
	}

	// 5. 启动容器
	if err := s.dockerService.StartContainer(ctx, sandbox.ContainerID); err != nil {
		s.updateSandboxStatus(id, "failed", fmt.Sprintf("启动容器失败: %v", err))
		return fmt.Errorf("启动容器失败: %w", err)
	}

	// 6. 创建 ADB Gateway 映射（两个映射：系统和 agent/user）
	if s.adbGatewayService != nil {
		upstream := fmt.Sprintf("%s:5555", sandbox.ContainerName)

		// 如果已有映射ID，先尝试删除旧映射
		if sandbox.AdbMappingID != "" {
			log.Printf("删除旧的系统 ADB 映射: %s", sandbox.AdbMappingID)
			_ = s.adbGatewayService.RemoveMapping(sandbox.AdbMappingID)
		}
		if sandbox.AgentUserMappingID != "" {
			log.Printf("删除旧的 Agent/User ADB 映射: %s", sandbox.AgentUserMappingID)
			_ = s.adbGatewayService.RemoveMapping(sandbox.AgentUserMappingID)
		}

		// 1. 创建系统映射（用于 scrcpy 和系统操作）
		systemMappingName := fmt.Sprintf("sandbox-%s-system", sandbox.ID)
		systemMappingSpec := clients.MappingSpec{
			Name:     systemMappingName,
			Note:     fmt.Sprintf("Sandbox %s 的系统 ADB 映射（用于 scrcpy 和系统操作）", sandbox.ID),
			Upstream: upstream,
		}

		systemMapping, err := s.adbGatewayService.CreateMapping(systemMappingSpec)
		if err != nil {
			log.Printf("警告: 创建系统 ADB Gateway 映射失败: %v", err)
		} else {
			log.Printf("成功创建系统 ADB Gateway 映射: %s (listen: %s, upstream: %s)", systemMapping.ID, systemMapping.Listen, systemMapping.Upstream)
			// 保存系统映射ID到数据库
			if err := models.DB.Model(&models.Sandbox{}).
				Where("id = ?", id).
				Update("adb_mapping_id", systemMapping.ID).Error; err != nil {
				log.Printf("警告: 保存系统 ADB 映射 ID 失败: %v", err)
			}

			// 6.1 启动阶段预初始化 scrcpy，减少首次连接等待
			if port, err := s.EnsureScrcpyForward(ctx, id); err != nil {
				log.Printf("警告: 启动阶段初始化 scrcpy 失败: %v", err)
			} else {
				log.Printf("Sandbox %s 启动时已初始化 scrcpy (端口: %d)", id, port)
			}
		}

		// 2. 创建 Agent/User 映射（用于记录 agent 和用户在 scrcpy player 上的操作）
		agentUserMappingName := fmt.Sprintf("sandbox-%s-agent-user", sandbox.ID)
		agentUserMappingSpec := clients.MappingSpec{
			Name:     agentUserMappingName,
			Note:     fmt.Sprintf("Sandbox %s 的 Agent/User ADB 映射（用于记录操作）", sandbox.ID),
			ToID:     sandbox.ID, // 设置 ToID 为 sandbox ID，用于标识这是 agent/user 映射
			Upstream: upstream,
		}

		agentUserMapping, err := s.adbGatewayService.CreateMapping(agentUserMappingSpec)
		if err != nil {
			log.Printf("警告: 创建 Agent/User ADB Gateway 映射失败: %v", err)
		} else {
			log.Printf("成功创建 Agent/User ADB Gateway 映射: %s (listen: %s, upstream: %s)", agentUserMapping.ID, agentUserMapping.Listen, agentUserMapping.Upstream)
			// 保存 Agent/User 映射ID到数据库
			if err := models.DB.Model(&models.Sandbox{}).
				Where("id = ?", id).
				Update("agent_user_mapping_id", agentUserMapping.ID).Error; err != nil {
				log.Printf("警告: 保存 Agent/User ADB 映射 ID 失败: %v", err)
			}
		}
	}

	// 7. 等待 ADB 就绪（等待容器完全启动）
	log.Printf("等待 Sandbox %s 的 ADB 服务就绪...", id)
	waitCtx, waitCancel := context.WithTimeout(ctx, 60*time.Second)
	defer waitCancel()
	if sandbox.AdbMappingID != "" {
		mapping, err := s.adbGatewayService.GetMapping(sandbox.AdbMappingID)
		if err == nil && mapping != nil {
			adbDevice := mapping.Listen
			if strings.HasPrefix(adbDevice, "0.0.0.0:") {
				adbDevice = strings.Replace(adbDevice, "0.0.0.0:", "127.0.0.1:", 1)
			}
			if err := s.waitForDeviceReady(waitCtx, adbDevice); err != nil {
				log.Printf("警告: 等待 ADB 就绪超时或失败: %v", err)
				// 不返回错误，继续执行，因为可能 ADB 已经就绪
			} else {
				log.Printf("Sandbox %s 的 ADB 服务已就绪", id)
			}
		}
	}

	// 8. 自动重新安装保存的 APK（如果有）
	if len(sandbox.InstalledApkIDs) > 0 {
		log.Printf("开始自动重新安装 %d 个已保存的 APK...", len(sandbox.InstalledApkIDs))
		// 在后台异步安装，不阻塞启动流程
		go func() {
			installCtx, installCancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer installCancel()

			for _, apkID := range sandbox.InstalledApkIDs {
				if apkID == "" {
					continue
				}
				log.Printf("正在重新安装 APK: %s", apkID)
				if err := s.InstallApk(installCtx, id, apkID); err != nil {
					log.Printf("警告: 重新安装 APK %s 失败: %v", apkID, err)
					// 继续安装其他 APK，不中断
				} else {
					log.Printf("成功重新安装 APK: %s", apkID)
				}
				// 每个 APK 之间稍微延迟，避免并发过多
				time.Sleep(2 * time.Second)
			}
			log.Printf("完成自动重新安装 APK")

			// 8.1 重新执行 setup_adb_commands（在 APK 安装完成后）
			if len(sandbox.SetupAdbCommands) > 0 {
				log.Printf("开始重新执行 %d 条 setup_adb_commands...", len(sandbox.SetupAdbCommands))
				setupCtx, setupCancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer setupCancel()
				if err := s.runSetupAdbCommands(setupCtx, id, []string(sandbox.SetupAdbCommands)); err != nil {
					log.Printf("警告: 重新执行 setup_adb_commands 失败: %v", err)
				} else {
					log.Printf("成功重新执行 setup_adb_commands")
				}
			}
		}()
	} else {
		// 如果没有需要重新安装的 APK，直接执行 setup_adb_commands
		if len(sandbox.SetupAdbCommands) > 0 {
			log.Printf("开始重新执行 %d 条 setup_adb_commands...", len(sandbox.SetupAdbCommands))
			// 在后台异步执行，不阻塞启动流程
			go func() {
				setupCtx, setupCancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer setupCancel()
				if err := s.runSetupAdbCommands(setupCtx, id, []string(sandbox.SetupAdbCommands)); err != nil {
					log.Printf("警告: 重新执行 setup_adb_commands 失败: %v", err)
				} else {
					log.Printf("成功重新执行 setup_adb_commands")
				}
			}()
		}
	}

	// 9. 更新状态
	s.updateSandboxStatus(id, "running", "")
	log.Printf("Sandbox %s 已启动", id)
	return nil
}

// StopSandbox 停止 Sandbox
func (s *sandboxService) StopSandbox(ctx context.Context, id string) error {
	log.Printf("开始停止 Sandbox: %s", id)

	// 1. 查询 Sandbox
	var sandbox models.Sandbox
	if err := models.DB.First(&sandbox, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("sandbox 不存在")
		}
		return fmt.Errorf("查询 sandbox 失败: %w", err)
	}

	// 2. 删除 ADB Gateway 映射（系统和 agent/user）
	if s.adbGatewayService != nil {
		if sandbox.AdbMappingID != "" {
			log.Printf("删除系统 ADB Gateway 映射: %s", sandbox.AdbMappingID)
			if err := s.adbGatewayService.RemoveMapping(sandbox.AdbMappingID); err != nil {
				log.Printf("警告: 删除系统 ADB Gateway 映射失败: %v", err)
			} else {
				// 清除系统映射ID
				if err := models.DB.Model(&models.Sandbox{}).
					Where("id = ?", id).
					Update("adb_mapping_id", "").Error; err != nil {
					log.Printf("警告: 清除系统 ADB 映射 ID 失败: %v", err)
				}
			}
		}
		if sandbox.AgentUserMappingID != "" {
			log.Printf("删除 Agent/User ADB Gateway 映射: %s", sandbox.AgentUserMappingID)
			if err := s.adbGatewayService.RemoveMapping(sandbox.AgentUserMappingID); err != nil {
				log.Printf("警告: 删除 Agent/User ADB Gateway 映射失败: %v", err)
			} else {
				// 清除 Agent/User 映射ID
				if err := models.DB.Model(&models.Sandbox{}).
					Where("id = ?", id).
					Update("agent_user_mapping_id", "").Error; err != nil {
					log.Printf("警告: 清除 Agent/User ADB 映射 ID 失败: %v", err)
				}
			}
		}
		// 不影响容器停止
	}

	// 3. 检查是否有容器
	if sandbox.ContainerID == "" {
		return fmt.Errorf("sandbox 没有关联的容器")
	}

	// 4. 检查容器状态
	containerInfo, err := s.dockerService.GetContainer(ctx, sandbox.ContainerID)
	if err != nil {
		if strings.Contains(err.Error(), "No such container") {
			// 容器已不存在，更新状态为 stopped
			s.updateSandboxStatus(id, "stopped", "")
			return nil
		}
		return fmt.Errorf("查询容器状态失败: %w", err)
	}

	// 5. 如果已经停止，直接返回
	if !containerInfo.State.Running {
		s.updateSandboxStatus(id, "stopped", "")
		log.Printf("Sandbox %s 已经停止", id)
		return nil
	}

	// 6. 停止容器
	timeout := 10
	if err := s.dockerService.StopContainer(ctx, sandbox.ContainerID, &timeout); err != nil {
		s.updateSandboxStatus(id, "failed", fmt.Sprintf("停止容器失败: %v", err))
		return fmt.Errorf("停止容器失败: %w", err)
	}

	// 7. 更新状态
	s.updateSandboxStatus(id, "stopped", "")
	log.Printf("Sandbox %s 已停止", id)
	return nil
}

// SandboxVolumeWithType SandboxVolume 及其关联的 Volume 信息
type SandboxVolumeWithType struct {
	models.SandboxVolume
	VolumeType string `json:"volume_type"` // 从关联的 Volume 表中获取
}

// SandboxWithVolumes Sandbox 详细信息（包含 volumes）
type SandboxWithVolumes struct {
	models.Sandbox
	Volumes []SandboxVolumeWithType `json:"volumes"`
}

// GetSandboxWithVolumes 获取 Sandbox 及其 volumes 信息
func (s *sandboxService) GetSandboxWithVolumes(id string) (*SandboxWithVolumes, error) {
	sandbox, err := s.GetSandbox(id)
	if err != nil {
		return nil, err
	}

	volumes, err := s.GetSandboxVolumes(id)
	if err != nil {
		return nil, fmt.Errorf("查询 sandbox volumes 失败: %w", err)
	}

	// 为每个 SandboxVolume 添加 Volume 信息（包括 volume_type）
	volumesWithType := make([]SandboxVolumeWithType, 0, len(volumes))
	for _, sv := range volumes {
		var vol models.Volume
		if err := models.DB.First(&vol, "id = ?", sv.VolumeID).Error; err == nil {
			volumesWithType = append(volumesWithType, SandboxVolumeWithType{
				SandboxVolume: sv,
				VolumeType:    vol.VolumeType,
			})
		} else {
			// 如果查询失败，使用默认值 "user"
			volumesWithType = append(volumesWithType, SandboxVolumeWithType{
				SandboxVolume: sv,
				VolumeType:    "user",
			})
		}
	}

	return &SandboxWithVolumes{
		Sandbox: *sandbox,
		Volumes: volumesWithType,
	}, nil
}

// GetSandboxVolumes 获取 Sandbox 的挂载卷列表
func (s *sandboxService) GetSandboxVolumes(sandboxID string) ([]models.SandboxVolume, error) {
	var volumes []models.SandboxVolume
	if err := models.DB.Where("sandbox_id = ?", sandboxID).Find(&volumes).Error; err != nil {
		return nil, fmt.Errorf("查询 sandbox volumes 失败: %w", err)
	}
	return volumes, nil
}

// createMountDirectories 创建或复用挂载卷，返回挂载绑定列表（包含ro标记）
func (s *sandboxService) createMountDirectories(sandboxID string, mounts []models.MountConfig) ([]string, error) {
	mountBinds := make([]string, 0)
	volumesBasePath := filepath.Join(s.dataPath, "volumes") // Volume独立存储目录

	for i, mountSpec := range mounts {
		var volume *models.Volume
		var volumeID string
		var hostPath string

		if mountSpec.Volume == "" {
			// 创建新卷
			volumeID = utils.GenerateVolumeID()
			hostPath = filepath.Join(volumesBasePath, volumeID)

			// 创建目录
			if err := os.MkdirAll(hostPath, 0755); err != nil {
				return nil, fmt.Errorf("创建挂载目录 %s 失败: %w", hostPath, err)
			}

			// 创建 Volume 记录
			volume = &models.Volume{
				ID:          volumeID,
				HostPath:    hostPath,
				VolumeType:  "user",
				Description: fmt.Sprintf("Sandbox %s 的用户挂载卷", sandboxID),
			}
			if err := models.DB.Create(volume).Error; err != nil {
				return nil, fmt.Errorf("创建 Volume 记录失败: %w", err)
			}
			log.Printf("创建新卷: %s -> %s", volumeID, hostPath)
		} else {
			// 复用已存在的卷
			volumeID = mountSpec.Volume
			volume = &models.Volume{}
			if err := models.DB.First(volume, "id = ?", volumeID).Error; err != nil {
				return nil, fmt.Errorf("卷 %s 不存在: %w", volumeID, err)
			}
			hostPath = volume.HostPath
			log.Printf("复用卷: %s -> %s", volumeID, hostPath)
		}

		// 构建挂载绑定字符串
		bindStr := hostPath + ":" + mountSpec.ContainerPath
		if mountSpec.ReadOnly {
			bindStr += ":ro"
		}
		mountBinds = append(mountBinds, bindStr)

		log.Printf("挂载 [%d]: %s -> %s (readonly=%v)", i, hostPath, mountSpec.ContainerPath, mountSpec.ReadOnly)

		// 创建 Sandbox-Volume 关系记录
		sandboxVolume := models.SandboxVolume{
			SandboxID:     sandboxID,
			VolumeID:      volumeID,
			ContainerPath: mountSpec.ContainerPath,
			ReadOnly:      mountSpec.ReadOnly,
			Status:        "active",
			Description:   fmt.Sprintf("挂载点 %d", i),
		}
		if err := models.DB.Create(&sandboxVolume).Error; err != nil {
			log.Printf("警告: 记录 Sandbox-Volume 关系失败: %v", err)
		}
	}

	return mountBinds, nil
}

// findAvailableScrcpyPort 查找一个可用的 scrcpy forward 端口
// 端口范围：16000-17000
func (s *sandboxService) findAvailableScrcpyPort() (int, error) {
	minPort := 16000
	maxPort := 17000

	// 查询所有已使用的端口
	var usedPorts []int
	if err := models.DB.Model(&models.Sandbox{}).
		Where("scrcpy_forward_port > 0").
		Pluck("scrcpy_forward_port", &usedPorts).Error; err != nil {
		log.Printf("查询已使用的 scrcpy 端口失败: %v", err)
		// 继续执行，只是不能排除已使用的端口
	}

	usedPortsMap := make(map[int]bool)
	for _, port := range usedPorts {
		usedPortsMap[port] = true
	}

	// 查找可用端口
	for port := minPort; port <= maxPort; port++ {
		if !usedPortsMap[port] {
			return port, nil
		}
	}

	return 0, fmt.Errorf("无法找到可用的 scrcpy forward 端口（范围: %d-%d）", minPort, maxPort)
}

// EnsureScrcpyForward 确保指定 Sandbox 的 scrcpy forward 可用，如有需要会重新设置
func (s *sandboxService) EnsureScrcpyForward(ctx context.Context, sandboxID string) (int, error) {
	log.Printf("[scrcpy] 确保 Sandbox %s 的 scrcpy forward 可用", sandboxID)

	var sandbox models.Sandbox
	if err := models.DB.First(&sandbox, "id = ?", sandboxID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return 0, fmt.Errorf("sandbox 不存在")
		}
		return 0, fmt.Errorf("查询 sandbox 失败: %w", err)
	}

	scrcpyPort := sandbox.ScrcpyForwardPort

	// 如果端口未分配，分配一个
	if scrcpyPort == 0 {
		var err error
		if scrcpyPort, err = s.findAvailableScrcpyPort(); err != nil {
			return 0, fmt.Errorf("分配 scrcpy 端口失败: %w", err)
		}
		if err := models.DB.Model(&models.Sandbox{}).
			Where("id = ?", sandboxID).
			Update("scrcpy_forward_port", scrcpyPort).Error; err != nil {
			return 0, fmt.Errorf("保存 scrcpy forward 端口失败: %w", err)
		}
		log.Printf("[scrcpy] 为 Sandbox %s 分配 scrcpy 端口 %d", sandboxID, scrcpyPort)
	}

	// 后端系统操作使用 AdbMappingID（系统映射）
	if sandbox.AdbMappingID == "" {
		return 0, fmt.Errorf("sandbox %s 未绑定 ADB 映射，无法初始化 scrcpy", sandboxID)
	}

	mapping, err := s.adbGatewayService.GetMapping(sandbox.AdbMappingID)
	if err != nil {
		return 0, fmt.Errorf("获取 ADB 映射失败: %w", err)
	}

	adbDevice := mapping.Listen
	if strings.HasPrefix(adbDevice, "0.0.0.0:") {
		adbDevice = strings.Replace(adbDevice, "0.0.0.0:", "127.0.0.1:", 1)
	}

	// 后端重启后 scrcpy-server 会消失，不需要在这里检查进程
	// 让 scrcpy_service 的 StartScrcpySession 来决定是否需要重启
	// 因为它知道是否有活跃的 Session（广播模式）

	log.Printf("[scrcpy] EnsureScrcpyForward 完成，端口: %d", scrcpyPort)
	log.Printf("[scrcpy] 注意: 实际的进程管理由 ScrcpyService 处理（支持多订阅者广播）")
	return scrcpyPort, nil
}

// setupScrcpyForwardIfNeeded 仅在需要时设置 scrcpy forward（由 ScrcpyService 调用）
func (s *sandboxService) SetupScrcpyForwardIfNeeded(ctx context.Context, sandboxID string) (int, error) {
	var sandbox models.Sandbox
	if err := models.DB.First(&sandbox, "id = ?", sandboxID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return 0, fmt.Errorf("sandbox 不存在")
		}
		return 0, fmt.Errorf("查询 sandbox 失败: %w", err)
	}

	scrcpyPort := sandbox.ScrcpyForwardPort
	if scrcpyPort == 0 {
		return 0, fmt.Errorf("scrcpy 端口未分配")
	}

	// 后端系统操作使用 AdbMappingID（系统映射）
	if sandbox.AdbMappingID == "" {
		return 0, fmt.Errorf("sandbox 未绑定 ADB 映射")
	}

	mapping, err := s.adbGatewayService.GetMapping(sandbox.AdbMappingID)
	if err != nil {
		return 0, fmt.Errorf("获取 ADB 映射失败: %w", err)
	}

	adbDevice := mapping.Listen
	if strings.HasPrefix(adbDevice, "0.0.0.0:") {
		adbDevice = strings.Replace(adbDevice, "0.0.0.0:", "127.0.0.1:", 1)
	}

	// ⚠️ 关键：后端重启后可能存在孤儿进程和孤儿 forward
	// 必须先清理，确保干净的初始状态
	log.Printf("[scrcpy] 启动前清理旧状态（进程+forward）...")
	s.cleanupOldScrcpyState(ctx, adbDevice, scrcpyPort)

	// 完全重新初始化 scrcpy
	log.Printf("[scrcpy] 完全重新初始化 scrcpy-server...")
	if err := s.setupScrcpyForward(ctx, sandboxID, adbDevice, scrcpyPort); err != nil {
		return 0, err
	}

	log.Printf("[scrcpy] Sandbox %s 的 scrcpy forward 就绪 (端口: %d)", sandboxID, scrcpyPort)
	return scrcpyPort, nil
}

// cleanupOldScrcpyState 清理旧的 scrcpy 状态（进程 + forward）
func (s *sandboxService) cleanupOldScrcpyState(ctx context.Context, adbDevice string, forwardPort int) {
	log.Printf("[scrcpy] 步骤1: 清理宿主机上的 adb forward...")
	// 先清理 forward（避免孤儿 forward）
	removeForwardCmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "forward", "--remove", fmt.Sprintf("tcp:%d", forwardPort))
	if _, err := removeForwardCmd.CombinedOutput(); err != nil {
		log.Printf("[scrcpy] 清理 forward 失败（可能不存在）: %v", err)
	} else {
		log.Printf("[scrcpy] 已清理旧 forward: tcp:%d", forwardPort)
	}

	log.Printf("[scrcpy] 步骤2: 清理设备上的 scrcpy-server 进程...")
	// 方法1: pkill
	killCmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "shell", "pkill", "-9", "-f", "app_process.*scrcpy")
	if _, err := killCmd.CombinedOutput(); err == nil {
		log.Printf("[scrcpy] 已清理旧进程 (pkill)")
	}

	// 方法2: 通过 ps 查找并 kill（备用方案）
	killCmd2 := exec.CommandContext(ctx, "adb", "-s", adbDevice, "shell",
		"ps -ef | grep 'app_process.*scrcpy' | grep -v grep | awk '{print $2}' | xargs kill -9 2>/dev/null || true")
	killCmd2.CombinedOutput()

	// 给一点时间让进程和 forward 完全清理
	log.Printf("[scrcpy] 等待清理完成...")
	time.Sleep(1 * time.Second)

	log.Printf("[scrcpy] 清理完成，准备启动新的 scrcpy-server")
}

// isScrcpyServerRunning 检测目标设备上是否已有 scrcpy-server 进程
func (s *sandboxService) isScrcpyServerRunning(ctx context.Context, adbDevice string) bool {
	// 验证 scrcpy-server 是否在运行（使用 [s]crcpy 避免匹配到 grep 自身）
	checkCmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "shell", "ps -ef | grep '[s]crcpy'")
	checkOutput, err := checkCmd.CombinedOutput()
	if err == nil && len(checkOutput) > 0 {
		log.Printf("[异步] ✓ scrcpy-server 已在运行: %s", strings.TrimSpace(string(checkOutput)))
		return true
	}

	log.Printf("[异步] 警告: 未检测到 scrcpy-server 进程 (ps)")
	// 尝试使用 ps -A（某些 Android 版本需要）
	checkCmd2 := exec.CommandContext(ctx, "adb", "-s", adbDevice, "shell", "ps -A | grep '[s]crcpy'")
	checkOutput2, err2 := checkCmd2.CombinedOutput()
	if err2 == nil && len(checkOutput2) > 0 {
		log.Printf("[异步] ✓ scrcpy-server 已在运行 (ps -ef): %s", strings.TrimSpace(string(checkOutput2)))
		return true
	}

	log.Printf("[异步] ✗ 警告: 无法确认 scrcpy-server 是否运行")
	log.Printf("[异步] 尝试查看设备日志...")
	logCmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "shell", "logcat -d -s scrcpy:* | tail -20")
	logOutput, _ := logCmd.CombinedOutput()
	if len(logOutput) > 0 {
		log.Printf("[异步] 设备日志:\n%s", string(logOutput))
	}

	return false
}

// setupScrcpyForward 设置 scrcpy 端口转发
func (s *sandboxService) setupScrcpyForward(ctx context.Context, sandboxID string, adbDevice string, forwardPort int) error {
	// 1. adb connect 到设备
	log.Printf("[异步] 执行 adb connect: %s", adbDevice)
	connectCmd := exec.CommandContext(ctx, "adb", "connect", adbDevice)
	connectOutput, err := connectCmd.CombinedOutput()
	if err != nil {
		log.Printf("[异步] adb connect 失败: %v, 输出: %s", err, string(connectOutput))
		// 继续尝试，可能是已经连接了
	} else {
		log.Printf("[异步] adb connect 成功: %s", string(connectOutput))
	}

	// 1.1 等待并检查设备是否在线，找到实际的设备标识符
	maxRetries := 10
	actualDevice := adbDevice
	for i := 0; i < maxRetries; i++ {
		time.Sleep(500 * time.Millisecond)
		// 检查设备状态
		devicesCmd := exec.CommandContext(ctx, "adb", "devices")
		devicesOutput, err := devicesCmd.CombinedOutput()
		if err == nil {
			outputStr := string(devicesOutput)
			// 查找设备标识符（可能是 127.0.0.1:port 或 0.0.0.0:port）
			lines := strings.Split(outputStr, "\n")
			for _, line := range lines {
				// 查找包含端口号且状态为 "device" 的行
				if strings.Contains(line, ":") && strings.Contains(line, "device") {
					parts := strings.Fields(line)
					if len(parts) >= 2 && parts[1] == "device" {
						deviceID := parts[0]
						// 检查是否匹配我们的设备（可能是不同的 IP 格式）
						if strings.Contains(deviceID, ":") {
							// 提取端口号
							portPart := deviceID[strings.LastIndex(deviceID, ":"):]
							if strings.Contains(adbDevice, portPart) {
								actualDevice = deviceID
								log.Printf("[异步] 找到在线设备: %s (原地址: %s)", actualDevice, adbDevice)
								break
							}
						}
					}
				}
			}
			// 如果找到了设备，使用它
			if actualDevice != adbDevice || strings.Contains(outputStr, adbDevice) && strings.Contains(outputStr, "device") {
				log.Printf("[异步] 设备已在线: %s", actualDevice)
				break
			}
			log.Printf("[异步] 等待设备上线，重试 %d/%d...", i+1, maxRetries)
		}
		if i == maxRetries-1 {
			log.Printf("[异步] 警告: 设备可能未完全上线，使用原始地址继续尝试: %s", adbDevice)
		}
	}

	// 使用实际找到的设备标识符
	adbDevice = actualDevice
	log.Printf("[异步] 使用设备标识符: %s", adbDevice)

	// 1.2 验证设备真正可用（执行一个简单的命令）
	log.Printf("[异步] 验证设备是否真正可用...")
	maxTestRetries := 5
	deviceReady := false
	for i := 0; i < maxTestRetries; i++ {
		testCmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "shell", "echo", "test")
		testOutput, err := testCmd.CombinedOutput()
		if err == nil && strings.Contains(string(testOutput), "test") {
			log.Printf("[异步] 设备验证成功: %s", adbDevice)
			deviceReady = true
			break
		}
		log.Printf("[异步] 设备验证失败，重试 %d/%d: %v", i+1, maxTestRetries, err)
		time.Sleep(1 * time.Second)
	}

	if !deviceReady {
		return fmt.Errorf("设备 %s 无法正常响应命令", adbDevice)
	}
	// 2. 获取 scrcpy-server 文件路径
	serverPath := utils.GetScrcpyServerPath(s.dataPath)
	if serverPath == "" {
		return fmt.Errorf("无法获取 scrcpy-server 路径，data_path 未配置")
	}

	// 检查文件是否存在
	if _, err := os.Stat(serverPath); os.IsNotExist(err) {
		return fmt.Errorf("scrcpy-server 文件不存在: %s，请先下载", serverPath)
	}

	// 3. 推送 scrcpy-server 到设备
	deviceServerPath := "/data/local/tmp/scrcpy-server"
	log.Printf("[异步] 推送 scrcpy-server 到设备: %s -> %s", serverPath, deviceServerPath)
	pushCmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "push", serverPath, deviceServerPath)
	pushOutput, err := pushCmd.CombinedOutput()
	if err != nil {
		log.Printf("[异步] 推送 scrcpy-server 失败: %v, 输出: %s", err, string(pushOutput))
		// 继续尝试，可能是文件已存在
	} else {
		log.Printf("[异步] 推送 scrcpy-server 成功: %s", string(pushOutput))
	}

	// 4. 设置文件权限
	chmodCmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "shell", "chmod", "755", deviceServerPath)
	chmodOutput, err := chmodCmd.CombinedOutput()
	if err != nil {
		log.Printf("[异步] 设置 scrcpy-server 权限失败: %v, 输出: %s", err, string(chmodOutput))
	}

	// 5. 先清理可能存在的旧 forward
	log.Printf("[异步] 清理可能存在的 adb forward...")
	removeForwardCmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "forward", "--remove", fmt.Sprintf("tcp:%d", forwardPort))
	removeForwardCmd.CombinedOutput() // 忽略错误，可能不存在

	// 6. 杀掉可能存在的旧 scrcpy-server 进程
	log.Printf("[异步] 清理可能存在的旧 scrcpy-server 进程...")
	// 通过ps查找并kill
	killCmd2 := exec.CommandContext(ctx, "adb", "-s", adbDevice, "shell",
		"ps -ef | grep 'app_process.*scrcpy' | grep -v grep | awk '{print $2}' | xargs kill -9")
	killCmd2.CombinedOutput() // 忽略错误

	time.Sleep(1 * time.Second)

	// 7. ⭐ 关键：先设置 adb forward，再启动 server！
	// scrcpy-server 启动后会立即尝试监听 localabstract:scrcpy
	// 如果 forward 还没设置，server 可能会失败或超时
	log.Printf("[异步] 设置 adb forward (在启动 server 之前): localabstract:scrcpy -> tcp:%d", forwardPort)

	maxForwardRetries := 3
	var forwardErr error
	for i := 0; i < maxForwardRetries; i++ {
		forwardCmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "forward",
			fmt.Sprintf("tcp:%d", forwardPort), "localabstract:scrcpy")
		forwardOutput, err := forwardCmd.CombinedOutput()

		if err != nil {
			forwardErr = fmt.Errorf("adb forward 失败: %w, 输出: %s", err, string(forwardOutput))

			if i < maxForwardRetries-1 {
				log.Printf("[异步] adb forward 失败，重试 %d/%d: %v", i+1, maxForwardRetries, err)
				time.Sleep(500 * time.Millisecond)
				continue
			}
		} else {
			log.Printf("[异步] ✓ adb forward 成功: %s", strings.TrimSpace(string(forwardOutput)))
			forwardErr = nil
			break
		}
	}

	if forwardErr != nil {
		return forwardErr
	}

	// 8. 启动 scrcpy-server（保持前台运行）
	log.Printf("[异步] 启动 scrcpy-server...")
	// 重要：scrcpy-server 必须保持在前台运行（不使用 & 后台）
	// 参数说明：
	// - tunnel_forward=true: 使用 adb forward 方式（而不是 adb reverse）
	// - audio=false: 禁用音频流
	// - control=false: 禁用控制通道
	// - cleanup=false: 不自动清理
	// - max_size=1280: 限制视频分辨率
	// - video_bit_rate=1000000: 视频比特率 1Mbps
	// - max_fps=20: 限制帧率到20fps
	// - video_codec_options=i-frame-interval=1: 每1秒一个关键帧（用于快速重连）
	scrcpyArgs := []string{
		"max_size=1280",
		"video_bit_rate=1000000",
		"max_fps=20",
		"tunnel_forward=true",
		"audio=false",
		"control=false",
		"cleanup=true",
		"send_frame_meta=false",
		"raw_stream=true",
		"video_codec_options=i-frame-interval=1",
	}

	serverArgs := append([]string{
		"CLASSPATH=/data/local/tmp/scrcpy-server",
		"app_process",
		"/",
		"com.genymobile.scrcpy.Server",
		"3.3.3",
	}, scrcpyArgs...)

	adbArgs := append([]string{"-s", adbDevice, "shell"}, serverArgs...)
	// scrcpy-server 需要长期存活，不应绑定到请求级 ctx；使用无取消的命令并手动回收。
	startCmd := exec.Command("adb", adbArgs...)
	log.Printf("scrcpy-server 启动命令: %s", utils.CmdString(startCmd))
	// ✅ 捕获 stdout 和 stderr 以便调试
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	startCmd.Stdout = stdout
	startCmd.Stderr = stderr

	// 启动命令并捕获输出（但不等待完成，让它在后台持续运行）
	if err := startCmd.Start(); err != nil {
		log.Printf("[异步] 启动 scrcpy-server 失败: %v", err)
		return fmt.Errorf("启动 scrcpy-server 失败: %w", err)
	}
	log.Printf("[异步] scrcpy-server 进程已启动 (PID: %d)", startCmd.Process.Pid)
	// 后台回收子进程，避免成为僵尸进程
	go func() {
		if err := startCmd.Wait(); err != nil {
			log.Printf("[异步] scrcpy-server 退出: %v", err)
		} else {
			log.Printf("[异步] scrcpy-server 已正常退出")
		}
	}()

	// ⭐ 关键：给 scrcpy-server 充足的时间启动和初始化编码器
	// MediaCodec 初始化通常需要 2-4 秒
	log.Printf("[异步] 等待 scrcpy-server 初始化（等待 3 秒让编码器就绪）...")
	time.Sleep(3 * time.Second)

	// 打印 scrcpy-server 的输出（用于调试）
	log.Printf("[异步] scrcpy-server 初始化等待完成")
	if stdout.Len() > 0 {
		log.Printf("[异步] scrcpy-server stdout: %s", strings.TrimSpace(stdout.String()))
	}
	if stderr.Len() > 0 {
		log.Printf("[异步] scrcpy-server stderr: %s", strings.TrimSpace(stderr.String()))
	}

	// 验证 scrcpy-server 是否在运行（使用 [s]crcpy 避免匹配 grep 进程自身）
	checkCmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "shell", "ps -ef | grep '[s]crcpy'")
	checkOutput, err := checkCmd.CombinedOutput()
	if err == nil && len(checkOutput) > 0 {
		log.Printf("[异步] ✓ scrcpy-server 已在运行: %s", strings.TrimSpace(string(checkOutput)))
	} else {
		log.Printf("[异步] 警告: 未检测到 scrcpy-server 进程 (ps)")
		// 尝试使用 ps -A（某些 Android 版本需要）
		checkCmd2 := exec.CommandContext(ctx, "adb", "-s", adbDevice, "shell", "ps -A | grep '[s]crcpy'")
		checkOutput2, err2 := checkCmd2.CombinedOutput()
		if err2 == nil && len(checkOutput2) > 0 {
			log.Printf("[异步] ✓ scrcpy-server 已在运行 (ps -ef): %s", strings.TrimSpace(string(checkOutput2)))
		} else {
			log.Printf("[异步] ✗ 警告: 无法确认 scrcpy-server 是否运行")
			log.Printf("[异步] 尝试查看设备日志...")
			logCmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "shell", "logcat -d -s scrcpy:* | tail -20")
			logOutput, _ := logCmd.CombinedOutput()
			if len(logOutput) > 0 {
				log.Printf("[异步] 设备日志:\n%s", string(logOutput))
			}
		}
	}

	// 验证 forward 是否真的设置成功
	log.Printf("[异步] 验证 adb forward 配置...")
	listForwardCmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "forward", "--list")
	listOutput, err := listForwardCmd.CombinedOutput()
	if err == nil {
		log.Printf("[异步] 当前 forward 列表:\n%s", string(listOutput))
		if !strings.Contains(string(listOutput), fmt.Sprintf("tcp:%d", forwardPort)) {
			log.Printf("[异步] ⚠️ 警告: forward 列表中未找到端口 %d", forwardPort)
		}
	}

	// ⚠️ 重要：不要在这里测试 TCP 连接！
	// scrcpy-server 只接受一个连接，测试连接会导致它立即退出
	// 让真正的客户端（createSession）去建立连接
	log.Printf("[异步] 跳过 TCP 连接测试（避免消耗唯一的连接机会）")
	log.Printf("[异步] scrcpy-server 应该已经就绪，等待客户端连接...")

	log.Printf("[异步] 成功设置 scrcpy forward 端口: %d", forwardPort)
	return nil
}
