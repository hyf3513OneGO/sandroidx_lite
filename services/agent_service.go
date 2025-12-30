package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"
	"sandroidx.com/sandroidx_lite/clients"
	"sandroidx.com/sandroidx_lite/configs"
	"sandroidx.com/sandroidx_lite/models"
	"sandroidx.com/sandroidx_lite/utils"
)

// AgentService Agent 服务接口
type AgentService interface {
	// 创建 Agent
	CreateAgent(ctx context.Context, spec AgentCreateSpec) (*models.Agent, error)
	// 获取 Agent
	GetAgent(id string) (*models.Agent, error)
	// 获取 Agent 性能指标
	GetAgentMetrics(ctx context.Context, id string) (*AgentMetrics, error)
	// 获取 Agent 及其 volumes 信息
	GetAgentWithVolumes(id string) (*AgentWithVolumes, error)
	// 列出所有 Agents
	ListAgents() ([]models.Agent, error)
	// 删除 Agent (volumesToDelete: 要删除的Volume ID列表，为空则不删除任何Volume)
	DeleteAgent(ctx context.Context, id string, volumesToDelete []string) error
	// 启动 Agent
	StartAgent(ctx context.Context, id string) error
	// 停止 Agent
	StopAgent(ctx context.Context, id string) error
	// 获取 Agent 的挂载卷列表
	GetAgentVolumes(agentID string) ([]models.AgentVolume, error)
	// 在 Agent 中执行命令
	ExecCommand(ctx context.Context, agentID string, config *ExecConfig) (*ExecResult, error)
	// 创建一个 exec（用于流式输出 + 退出码检查）
	CreateExec(ctx context.Context, agentID string, config *ExecConfig) (string, error)
	// 启动一个 exec 并返回输出流（调用方负责 Close）
	StartExec(ctx context.Context, execID string, config *ExecConfig) (io.ReadCloser, error)
	// 检查 exec 状态（退出码/是否运行中）
	InspectExec(ctx context.Context, execID string) (*ExecInspectResponse, error)
	// 创建 Agent 交互式 shell 会话
	CreateShellSession(ctx context.Context, agentID string, shell string) (*ExecSession, error)
	// 切换 Agent 的 Sandbox（更新 mapping 的 upstream）
	SwitchSandbox(ctx context.Context, agentID string, sandboxName string) error
}

// AgentCreateSpec Agent 创建规格
type AgentCreateSpec struct {
	Image                string                   `json:"image"`
	Mounts               []MountSpec              `json:"mounts"`
	RequiredEnvVariables []string                 `json:"required_env_variables"`
	SetupCommands        []models.Command         `json:"setup_commands"`
	RunningVariables     []models.RunningVariable `json:"running_variables"`
	RunningCommands      []models.Command         `json:"running_commands"`
	Envs                 map[string]string        `json:"envs"` // 用户提供的环境变量
}

// MountSpec 挂载规格
type MountSpec struct {
	Volume        string `json:"volume"`         // 卷ID，为空则创建新卷
	ContainerPath string `json:"container_path"` // 容器内路径
	ReadOnly      bool   `json:"read_only"`      // 是否只读，默认false
}

// AgentMetrics Agent 性能指标
type AgentMetrics struct {
	CPU        float64 `json:"cpu"`         // CPU 使用率（百分比）
	Memory     float64 `json:"memory"`      // 内存使用率（百分比）
	NetworkIn  float64 `json:"network_in"`  // 下行速率 KB/s
	NetworkOut float64 `json:"network_out"` // 上行速率 KB/s
}

// containerStats 定义用于解析 Docker Stats 的最小字段集合
type containerStats struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage  uint64   `json:"total_usage"`
			PercpuUsage []uint64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs  uint32 `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage  uint64   `json:"total_usage"`
			PercpuUsage []uint64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs  uint32 `json:"online_cpus"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
}

// agentService Agent 服务实现
type agentService struct {
	dockerService     DockerService
	adbGatewayService AdbGatewayService
	dataPath          string
}

// NewAgentService 创建新的 Agent 服务
func NewAgentService(dockerService DockerService, adbGatewayService AdbGatewayService) AgentService {
	return &agentService{
		dockerService:     dockerService,
		adbGatewayService: adbGatewayService,
		dataPath:          configs.AppConfig.Server.DataPath,
	}
}

// generateAgentID 生成 Agent ID (已废弃，使用 utils.GenerateAgentID)
// 保留此函数以保持向后兼容
func generateAgentID() string {
	return utils.GenerateAgentID()
}

// CreateAgent 创建 Agent（异步）
// 此方法会立即创建 Agent 数据库记录并返回，实际容器创建在后台异步执行
func (s *agentService) CreateAgent(ctx context.Context, spec AgentCreateSpec) (*models.Agent, error) {
	log.Println("开始创建 Agent...")

	// 1. 生成 Agent ID
	agentID := generateAgentID()
	log.Printf("生成 Agent ID: %s", agentID)

	// 2. 创建 Agent 数据库记录
	agent := &models.Agent{
		ID:                   agentID,
		Image:                spec.Image,
		RequiredEnvVariables: spec.RequiredEnvVariables,
		SetupCommands:        spec.SetupCommands,
		RunningVariables:     spec.RunningVariables,
		RunningCommands:      spec.RunningCommands,
		Status:               "creating",
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
	}

	if err := models.DB.Create(agent).Error; err != nil {
		return nil, fmt.Errorf("创建 Agent 数据库记录失败: %w", err)
	}
	log.Printf("已创建 Agent 数据库记录: %s", agentID)

	// 3. 在后台 goroutine 中异步执行容器创建
	go s.createAgentAsync(agentID, spec)

	return agent, nil
}

// createAgentAsync 异步创建 Agent 容器
func (s *agentService) createAgentAsync(agentID string, spec AgentCreateSpec) {
	ctx := context.Background()
	log.Printf("[异步] 开始为 Agent %s 创建容器...", agentID)

	// 4. 创建挂载目录
	mountBinds, err := s.createMountDirectories(agentID, spec.Mounts)
	if err != nil {
		s.updateAgentStatus(agentID, "failed", err.Error())
		log.Printf("[异步] Agent %s 创建失败: %v", agentID, err)
		return
	}

	// 5. 添加共享 APK 卷挂载（只读，使用系统级 Volume）
	apksVolumeID, apksPath, err := EnsureSharedApkVolume(s.dataPath)
	if err != nil {
		s.updateAgentStatus(agentID, "failed", fmt.Sprintf("准备共享 APK 卷失败: %v", err))
		log.Printf("[异步] Agent %s 创建失败: %v", agentID, err)
		return
	}
	mountBinds = append(mountBinds, fmt.Sprintf("%s:/sandroidx/apks:ro", apksPath))
	log.Printf("添加共享 APK 卷挂载: %s -> /sandroidx/apks (只读)", apksPath)

	// 记录 apks 挂载关系（只读）
	apksAgentVolume := models.AgentVolume{
		AgentID:       agentID,
		VolumeID:      apksVolumeID,
		ContainerPath: "/sandroidx/apks",
		ReadOnly:      true, // APKs 目录只读
		Status:        "active",
		Description:   "共享 APKs 目录（只读）",
	}
	if err := models.DB.Create(&apksAgentVolume).Error; err != nil {
		log.Printf("警告: 记录 APKs 挂载关系失败: %v", err)
	}

	// 6. 在 ADB Gateway 中创建映射
	mapping, err := s.createAdbMapping(agentID)
	if err != nil {
		s.updateAgentStatus(agentID, "failed", err.Error())
		log.Printf("[异步] Agent %s 创建失败: %v", agentID, err)
		return
	}
	log.Printf("[异步] 已创建 ADB 映射: %s, listen: %s", mapping.ID, mapping.Listen)

	// 从 mapping.Listen 中提取端口号（格式: 127.0.0.1:15555）
	listenPort := ""
	if mapping.Listen != "" {
		parts := strings.Split(mapping.Listen, ":")
		if len(parts) == 2 {
			listenPort = parts[1]
		} else {
			// 如果没有冒号，可能是只有端口号
			listenPort = mapping.Listen
		}
	}

	// 从数据库获取 ADB Gateway 容器名
	var adbGateway models.AdbGateway
	if err := models.DB.Where("id = ?", "default").First(&adbGateway).Error; err != nil {
		s.updateAgentStatus(agentID, "failed", fmt.Sprintf("获取 ADB Gateway 信息失败: %v", err))
		log.Printf("[异步] Agent %s 创建失败: %v", agentID, err)
		return
	}

	// 使用服务发现方式构建 ANDROID_SERIAL（容器名:端口）
	androidSerial := ""
	if listenPort != "" {
		androidSerial = fmt.Sprintf("%s:%s", adbGateway.ContainerName, listenPort)
	}
	log.Printf("[异步] 构建 ANDROID_SERIAL: %s", androidSerial)

	// 更新 Agent 的 MappingID
	var agent models.Agent
	if err := models.DB.First(&agent, "id = ?", agentID).Error; err != nil {
		s.updateAgentStatus(agentID, "failed", fmt.Sprintf("查询 Agent 失败: %v", err))
		log.Printf("[异步] Agent %s 创建失败: %v", agentID, err)
		return
	}
	agent.MappingID = mapping.ID

	// 存储所有环境变量到 Agent 模型
	if spec.Envs == nil {
		agent.Envs = make(models.StringMap)
	} else {
		agent.Envs = models.StringMap(spec.Envs)
	}
	// 添加 ANDROID_SERIAL 到 envs（使用服务发现方式）
	if androidSerial != "" {
		agent.Envs["ANDROID_SERIAL"] = androidSerial
	}
	if err := models.DB.Save(&agent).Error; err != nil {
		log.Printf("更新 Agent 映射信息和环境变量失败: %v", err)
	}

	// 7. 准备环境变量
	envVars := []string{}
	if androidSerial != "" {
		envVars = append(envVars, fmt.Sprintf("ANDROID_SERIAL=%s", androidSerial))
	}

	// 添加用户提供的环境变量
	if spec.Envs != nil {
		for key, value := range spec.Envs {
			envVars = append(envVars, fmt.Sprintf("%s=%s", key, value))
		}
	}
	log.Printf("[异步] 准备环境变量: %d 个", len(envVars))

	// 8. 创建容器
	containerConfig := &ContainerCreateConfig{
		Image:         spec.Image,
		Name:          agentID,
		NetworkMode:   getNetworkName(),
		RestartPolicy: "no",
		Env:           envVars,
		Binds:         mountBinds, // 使用Binds而不是Volumes
		ExtraHosts: []string{
			"host.docker.internal:host-gateway",
		},
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
			s.updateAgentStatus(agentID, "failed", fmt.Sprintf("拉取镜像失败: %v", err))
			log.Printf("[异步] Agent %s 创建失败: 拉取镜像失败: %v", agentID, err)
			return
		}
		log.Printf("[异步] 镜像 %s 拉取成功", spec.Image)
	} else {
		log.Printf("[异步] 镜像 %s 已存在，跳过拉取", spec.Image)
	}

	log.Printf("[异步] 创建容器: %s", agentID)
	containerID, err := s.dockerService.CreateContainer(ctx, containerConfig)
	if err != nil {
		s.updateAgentStatus(agentID, "failed", err.Error())
		log.Printf("[异步] Agent %s 创建失败: %v", agentID, err)
		return
	}

	if err := models.DB.Model(&models.Agent{}).Where("id = ?", agentID).Update("container_id", containerID).Error; err != nil {
		log.Printf("更新 Agent 容器 ID 失败: %v", err)
	}
	log.Printf("[异步] 容器已创建，ID: %s", containerID)

	// 9. 启动容器
	log.Printf("[异步] 启动容器: %s", containerID)
	if err := s.dockerService.StartContainer(ctx, containerID); err != nil {
		s.updateAgentStatus(agentID, "failed", err.Error())
		log.Printf("[异步] Agent %s 创建失败: %v", agentID, err)
		return
	}

	s.updateAgentStatus(agentID, "running", "")
	log.Printf("[异步] 容器已启动: %s", containerID)

	// 10. 等待容器启动
	time.Sleep(2 * time.Second)

	// 11. 执行 setup_commands
	if len(spec.SetupCommands) > 0 {
		log.Printf("[异步] 开始执行 setup 命令: %d 个", len(spec.SetupCommands))
		s.updateAgentStatus(agentID, "setup", "")

		for i, cmd := range spec.SetupCommands {
			log.Printf("[异步] 执行 setup 命令 [%d/%d]: workdir=%s, cmd=%s", i+1, len(spec.SetupCommands), cmd.Workdir, cmd.Run)

			// 构建完整的命令
			fullCmd := []string{"sh", "-c"}
			if cmd.Workdir != "" {
				fullCmd = append(fullCmd, fmt.Sprintf("cd %s && %s", cmd.Workdir, cmd.Run))
			} else {
				fullCmd = append(fullCmd, cmd.Run)
			}

			execConfig := &ExecConfig{
				Command:      fullCmd,
				AttachStdout: true,
				AttachStderr: true,
			}

			result, err := s.dockerService.ExecCommand(ctx, containerID, execConfig)
			if err != nil {
				errMsg := fmt.Sprintf("执行 setup 命令失败: %v, output: %s", err, result.Output)
				s.updateAgentStatus(agentID, "setup_failed", errMsg)
				log.Printf("[异步] Setup 命令执行失败: %s", errMsg)
				return
			}

			if result.ExitCode != 0 {
				errMsg := fmt.Sprintf("Setup 命令执行失败，退出码: %d, output: %s", result.ExitCode, result.Output)
				s.updateAgentStatus(agentID, "setup_failed", errMsg)
				log.Printf("[异步] Setup 命令执行失败: %s", errMsg)
				return
			}

			log.Printf("[异步] Setup 命令 [%d/%d] 执行成功", i+1, len(spec.SetupCommands))
			if result.Output != "" {
				log.Printf("[异步] 命令输出: %s", result.Output)
			}
		}

		now := time.Now()
		if err := models.DB.Model(&models.Agent{}).Where("id = ?", agentID).Update("setup_completed_at", &now).Error; err != nil {
			log.Printf("更新 SetupCompletedAt 失败: %v", err)
		}
		s.updateAgentStatus(agentID, "running", "")
		log.Printf("[异步] 所有 setup 命令执行完成")
	} else {
		log.Printf("[异步] 无需执行 setup 命令，保持 running 状态")
	}

	log.Printf("[异步] Agent %s 创建成功", agentID)
}

// createMountDirectories 创建或复用挂载卷，返回挂载绑定列表（包含ro标记）
func (s *agentService) createMountDirectories(agentID string, mounts []MountSpec) ([]string, error) {
	mountBinds := make([]string, 0)
	volumesBasePath := filepath.Join(s.dataPath, "volumes") // Volume独立存储目录

	for i, mountSpec := range mounts {
		var volume *models.Volume
		var volumeID string

		if mountSpec.Volume == "" {
			// 创建新卷
			volumeID = utils.GenerateVolumeID()
			hostPath := filepath.Join(volumesBasePath, volumeID) // 独立路径，不包含agent_id

			// 创建目录
			if err := os.MkdirAll(hostPath, 0755); err != nil {
				return nil, fmt.Errorf("创建挂载目录 %s 失败: %w", hostPath, err)
			}

			// 创建 Volume 记录
			volume = &models.Volume{
				ID:          volumeID,
				HostPath:    hostPath,
				VolumeType:  "user",
				Description: utils.GenerateVolumeDescription(agentID),
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
			log.Printf("复用卷: %s -> %s", volumeID, volume.HostPath)
		}

		// 构建挂载绑定字符串
		bindStr := volume.HostPath + ":" + mountSpec.ContainerPath
		if mountSpec.ReadOnly {
			bindStr += ":ro"
		}
		mountBinds = append(mountBinds, bindStr)

		log.Printf("挂载 [%d]: %s -> %s (readonly=%v)", i, volume.HostPath, mountSpec.ContainerPath, mountSpec.ReadOnly)

		// 创建 Agent-Volume 关系记录
		agentVolume := models.AgentVolume{
			AgentID:       agentID,
			VolumeID:      volumeID,
			ContainerPath: mountSpec.ContainerPath,
			ReadOnly:      mountSpec.ReadOnly,
			Status:        "active",
			Description:   fmt.Sprintf("挂载点 %d", i),
		}
		if err := models.DB.Create(&agentVolume).Error; err != nil {
			log.Printf("警告: 记录 Agent-Volume 关系失败: %v", err)
		}
	}

	return mountBinds, nil
}

// createAdbMapping 在 ADB Gateway 中创建映射
func (s *agentService) createAdbMapping(agentID string) (*clients.Mapping, error) {
	mappingSpec := clients.MappingSpec{
		Name:     utils.GenerateAdbMappingName(agentID),
		Note:     utils.GenerateAdbMappingNote(agentID),
		FromID:   agentID,
		ToID:     "",
		Listen:   "", // 让 Gateway 自动分配
		Upstream: "",
	}

	mapping, err := s.adbGatewayService.CreateMapping(mappingSpec)
	if err != nil {
		return nil, fmt.Errorf("创建 ADB 映射失败: %w", err)
	}

	return mapping, nil
}

// updateAgentStatus 更新 Agent 状态
func (s *agentService) updateAgentStatus(agentID string, status string, lastError string) {
	updates := map[string]interface{}{
		"status":     status,
		"last_error": lastError,
		"updated_at": time.Now(),
	}

	if err := models.DB.Model(&models.Agent{}).Where("id = ?", agentID).Updates(updates).Error; err != nil {
		log.Printf("更新 Agent 状态失败: %v", err)
	}
}

// AgentWithVolumes Agent 详细信息（包含 volumes）
type AgentWithVolumes struct {
	models.Agent
	Volumes []models.AgentVolume `json:"volumes"`
}

// GetAgent 获取 Agent
func (s *agentService) GetAgent(id string) (*models.Agent, error) {
	var agent models.Agent
	if err := models.DB.First(&agent, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("agent 不存在")
		}
		return nil, fmt.Errorf("查询 agent 失败: %w", err)
	}
	return &agent, nil
}

// GetAgentWithVolumes 获取 Agent 及其 volumes 信息
func (s *agentService) GetAgentWithVolumes(id string) (*AgentWithVolumes, error) {
	agent, err := s.GetAgent(id)
	if err != nil {
		return nil, err
	}

	volumes, err := s.GetAgentVolumes(id)
	if err != nil {
		return nil, fmt.Errorf("查询 agent volumes 失败: %w", err)
	}

	return &AgentWithVolumes{
		Agent:   *agent,
		Volumes: volumes,
	}, nil
}

// ListAgents 列出所有 Agents
func (s *agentService) ListAgents() ([]models.Agent, error) {
	var agents []models.Agent
	if err := models.DB.Order("created_at DESC").Find(&agents).Error; err != nil {
		return nil, fmt.Errorf("查询 agents 失败: %w", err)
	}
	return agents, nil
}

// DeleteAgent 删除 Agent
func (s *agentService) DeleteAgent(ctx context.Context, id string, volumesToDelete []string) error {
	agent, err := s.GetAgent(id)
	if err != nil {
		return err
	}

	// 1. 停止并删除容器
	if agent.ContainerID != "" {
		log.Printf("停止容器: %s", agent.ContainerID)
		timeout := 10
		if err := s.dockerService.StopContainer(ctx, agent.ContainerID, &timeout); err != nil {
			log.Printf("警告: 停止容器失败: %v", err)
		}

		log.Printf("删除容器: %s", agent.ContainerID)
		if err := s.dockerService.RemoveContainer(ctx, agent.ContainerID, true); err != nil {
			log.Printf("警告: 删除容器失败: %v", err)
		}
	}

	// 2. 删除 ADB 映射
	if agent.MappingID != "" {
		log.Printf("删除 ADB 映射: %s", agent.MappingID)
		if err := s.adbGatewayService.RemoveMapping(agent.MappingID); err != nil {
			log.Printf("警告: 删除 ADB 映射失败: %v", err)
		}
	}

	// 3. 处理挂载关系和Volume删除
	var agentVolumes []models.AgentVolume
	if err := models.DB.Where("agent_id = ?", id).Find(&agentVolumes).Error; err != nil {
		log.Printf("警告: 查询挂载关系失败: %v", err)
	}

	// 构建要删除的Volume ID集合
	volumesToDeleteMap := make(map[string]bool)
	for _, vid := range volumesToDelete {
		volumesToDeleteMap[vid] = true
	}

	// 处理每个挂载的Volume
	for _, av := range agentVolumes {
		var vol models.Volume
		if err := models.DB.First(&vol, "id = ?", av.VolumeID).Error; err != nil {
			log.Printf("警告: 查询Volume %s 失败: %v", av.VolumeID, err)
			continue
		}

		// 检查是否在删除列表中
		shouldDelete := volumesToDeleteMap[vol.ID]

		if shouldDelete && vol.VolumeType == "user" {
			// 检查是否是只读卷，只读卷不应该删除本地目录
			if av.ReadOnly {
				log.Printf("跳过只读卷 %s: 只读卷不允许删除本地目录", vol.ID)
				// 仍然删除 Volume 记录，但不删除本地目录
				if err := models.DB.Delete(&vol).Error; err != nil {
					log.Printf("警告: 删除Volume记录失败: %v", err)
				}
				continue
			}

			// 检查是否有其他Agent在使用该Volume
			var otherUsageCount int64
			if err := models.DB.Model(&models.AgentVolume{}).
				Where("volume_id = ? AND agent_id != ? AND status = ?", vol.ID, id, "active").
				Count(&otherUsageCount).Error; err != nil {
				log.Printf("警告: 查询Volume使用情况失败: %v", err)
				continue
			}

			if otherUsageCount == 0 {
				// 没有其他Agent使用，可以删除
				log.Printf("删除用户卷: %s -> %s (无其他Agent使用)", vol.ID, vol.HostPath)
				if err := os.RemoveAll(vol.HostPath); err != nil {
					log.Printf("警告: 删除卷目录失败: %v", err)
				}

				// 删除Volume记录
				if err := models.DB.Delete(&vol).Error; err != nil {
					log.Printf("警告: 删除Volume记录失败: %v", err)
				}
			} else {
				log.Printf("无法删除Volume %s: 仍有 %d 个其他Agent在使用", vol.ID, otherUsageCount)
			}
		} else if shouldDelete && vol.VolumeType == "system" {
			log.Printf("跳过系统卷 %s: 系统卷不允许删除", vol.ID)
		} else {
			log.Printf("保留Volume %s", vol.ID)
		}
	}

	// 4. 删除所有该Agent的Agent-Volume关系
	if err := models.DB.Where("agent_id = ?", id).Delete(&models.AgentVolume{}).Error; err != nil {
		log.Printf("警告: 删除Agent-Volume关系失败: %v", err)
	}

	// 4.5 删除该 Agent 的所有分享链接（避免遗留可访问的 share token）
	if err := models.DB.WithContext(ctx).Where("agent_id = ?", id).Delete(&models.AgentShare{}).Error; err != nil {
		return fmt.Errorf("删除 agent 分享链接失败: %w", err)
	}

	// 5. 删除数据库记录
	if err := models.DB.Delete(&models.Agent{}, "id = ?", id).Error; err != nil {
		return fmt.Errorf("删除 agent 数据库记录失败: %w", err)
	}

	log.Printf("Agent %s 已删除 (删除了 %d 个指定的Volume)", id, len(volumesToDelete))
	return nil
}

// StartAgent 启动 Agent
func (s *agentService) StartAgent(ctx context.Context, id string) error {
	agent, err := s.GetAgent(id)
	if err != nil {
		return err
	}

	if agent.ContainerID == "" {
		return fmt.Errorf("agent 没有关联的容器")
	}

	if err := s.dockerService.StartContainer(ctx, agent.ContainerID); err != nil {
		return fmt.Errorf("启动容器失败: %w", err)
	}

	s.updateAgentStatus(id, "running", "")
	log.Printf("Agent %s 已启动", id)
	return nil
}

// StopAgent 停止 Agent
func (s *agentService) StopAgent(ctx context.Context, id string) error {
	agent, err := s.GetAgent(id)
	if err != nil {
		return err
	}

	if agent.ContainerID == "" {
		return fmt.Errorf("agent 没有关联的容器")
	}

	timeout := 10
	if err := s.dockerService.StopContainer(ctx, agent.ContainerID, &timeout); err != nil {
		return fmt.Errorf("停止容器失败: %w", err)
	}

	s.updateAgentStatus(id, "stopped", "")
	log.Printf("Agent %s 已停止", id)
	return nil
}

// GetAgentVolumes 获取 Agent 的挂载卷列表（包含Volume详情）
func (s *agentService) GetAgentVolumes(agentID string) ([]models.AgentVolume, error) {
	var volumes []models.AgentVolume
	// 按创建时间排序
	if err := models.DB.Where("agent_id = ?", agentID).Order("created_at ASC").Find(&volumes).Error; err != nil {
		return nil, fmt.Errorf("查询 Agent 挂载卷失败: %w", err)
	}
	return volumes, nil
}

// ExecCommand 在 Agent 容器中执行命令
func (s *agentService) ExecCommand(ctx context.Context, agentID string, config *ExecConfig) (*ExecResult, error) {
	agent, err := s.GetAgent(agentID)
	if err != nil {
		return nil, err
	}

	if agent.ContainerID == "" {
		return nil, fmt.Errorf("agent 没有关联的容器")
	}

	if agent.Status != "running" && agent.Status != "ready" {
		return nil, fmt.Errorf("agent 状态不是运行中: %s", agent.Status)
	}

	// 使用 Docker 服务执行命令
	result, err := s.dockerService.ExecCommand(ctx, agent.ContainerID, config)
	if err != nil {
		return nil, fmt.Errorf("执行命令失败: %w", err)
	}

	return result, nil
}

// CreateExec 创建 exec 实例（用于后续 StartExec/InspectExec）
func (s *agentService) CreateExec(ctx context.Context, agentID string, config *ExecConfig) (string, error) {
	agent, err := s.GetAgent(agentID)
	if err != nil {
		return "", err
	}
	if agent.ContainerID == "" {
		return "", fmt.Errorf("agent 没有关联的容器")
	}
	if agent.Status != "running" && agent.Status != "ready" {
		return "", fmt.Errorf("agent 状态不是运行中: %s", agent.Status)
	}
	return s.dockerService.CreateExec(ctx, agent.ContainerID, config)
}

// StartExec 启动 exec 并返回输出流
func (s *agentService) StartExec(ctx context.Context, execID string, config *ExecConfig) (io.ReadCloser, error) {
	resp, err := s.dockerService.StartExec(ctx, execID, config)
	if err != nil {
		return nil, err
	}
	// types.HijackedResponse 实现 Close，且 resp.Reader 负责读取输出
	return &execStreamWrapper{resp: resp}, nil
}

// InspectExec 检查 exec 状态
func (s *agentService) InspectExec(ctx context.Context, execID string) (*ExecInspectResponse, error) {
	return s.dockerService.InspectExec(ctx, execID)
}

// GetAgentMetrics 获取 Agent 容器的实时性能指标
func (s *agentService) GetAgentMetrics(ctx context.Context, id string) (*AgentMetrics, error) {
	agent, err := s.GetAgent(id)
	if err != nil {
		return nil, err
	}

	if agent.ContainerID == "" {
		return nil, fmt.Errorf("agent 没有关联的容器")
	}

	// 首次采样
	sampleStart := time.Now()
	firstStats, err := s.fetchContainerStats(ctx, agent.ContainerID)
	if err != nil {
		return nil, err
	}

	cpuPercent := roundTwoDecimals(calculateCPUPercent(firstStats))
	memPercent := roundTwoDecimals(calculateMemoryPercent(firstStats))
	rx1, tx1 := aggregateNetworkBytes(firstStats)

	// 等待一小段时间进行第二次采样，以计算网络速率
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(1 * time.Second):
	}

	secondStats, err := s.fetchContainerStats(ctx, agent.ContainerID)
	if err != nil {
		return nil, err
	}
	rx2, tx2 := aggregateNetworkBytes(secondStats)

	elapsed := time.Since(sampleStart).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}

	networkInKBs := roundTwoDecimals(float64(rx2-rx1) / 1024 / elapsed)
	networkOutKBs := roundTwoDecimals(float64(tx2-tx1) / 1024 / elapsed)

	return &AgentMetrics{
		CPU:        cpuPercent,
		Memory:     memPercent,
		NetworkIn:  networkInKBs,
		NetworkOut: networkOutKBs,
	}, nil
}

// fetchContainerStats 拉取一次容器统计信息并解析
func (s *agentService) fetchContainerStats(ctx context.Context, containerID string) (*containerStats, error) {
	statsReader, err := s.dockerService.GetContainerStats(ctx, containerID, false)
	if err != nil {
		return nil, fmt.Errorf("获取容器统计信息失败: %w", err)
	}
	defer statsReader.Body.Close()

	var stats containerStats
	if err := json.NewDecoder(statsReader.Body).Decode(&stats); err != nil {
		return nil, fmt.Errorf("解析容器统计信息失败: %w", err)
	}

	return &stats, nil
}

// calculateCPUPercent 参考 Docker 的 CPU 计算公式
func calculateCPUPercent(stats *containerStats) float64 {
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage - stats.PreCPUStats.SystemUsage)

	onlineCPUs := float64(stats.CPUStats.OnlineCPUs)
	if onlineCPUs == 0 && len(stats.CPUStats.CPUUsage.PercpuUsage) > 0 {
		onlineCPUs = float64(len(stats.CPUStats.CPUUsage.PercpuUsage))
	}

	if cpuDelta > 0 && systemDelta > 0 && onlineCPUs > 0 {
		return (cpuDelta / systemDelta) * onlineCPUs * 100.0
	}
	return 0
}

// calculateMemoryPercent 计算内存使用率（去掉 cache）
func calculateMemoryPercent(stats *containerStats) float64 {
	usage := float64(stats.MemoryStats.Usage)
	if cache, ok := stats.MemoryStats.Stats["cache"]; ok {
		usage -= float64(cache)
	}

	limit := float64(stats.MemoryStats.Limit)
	if limit == 0 {
		return 0
	}

	return (usage / limit) * 100.0
}

// aggregateNetworkBytes 聚合所有网卡的收发字节
func aggregateNetworkBytes(stats *containerStats) (rx uint64, tx uint64) {
	for _, v := range stats.Networks {
		rx += v.RxBytes
		tx += v.TxBytes
	}
	return
}

func roundTwoDecimals(v float64) float64 {
	return math.Round(v*100) / 100
}

// CreateShellSession 为 Agent 创建交互式 shell 会话
func (s *agentService) CreateShellSession(ctx context.Context, agentID string, shell string) (*ExecSession, error) {
	agent, err := s.GetAgent(agentID)
	if err != nil {
		return nil, err
	}

	if agent.ContainerID == "" {
		return nil, fmt.Errorf("agent 没有关联的容器")
	}

	if agent.Status != "running" && agent.Status != "ready" {
		return nil, fmt.Errorf("agent 状态不是运行中: %s", agent.Status)
	}

	// 默认使用 /bin/sh
	if shell == "" {
		shell = "/bin/sh"
	}

	// 创建交互式配置
	config := &ExecConfig{
		Command:      []string{shell},
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
	}

	// 使用 Docker 服务创建交互式会话
	session, err := s.dockerService.ExecCommandInteractive(ctx, agent.ContainerID, config)
	if err != nil {
		return nil, fmt.Errorf("创建 shell 会话失败: %w", err)
	}

	return session, nil
}

// SwitchSandbox 切换 Agent 的 Sandbox（更新 mapping 的 upstream 指向新的 sandbox）
func (s *agentService) SwitchSandbox(ctx context.Context, agentID string, sandboxName string) error {
	if agentID == "" {
		return fmt.Errorf("agent ID 不能为空")
	}
	if sandboxName == "" {
		return fmt.Errorf("sandbox 名称不能为空")
	}

	// 检查 adbGatewayService 是否可用
	if s.adbGatewayService == nil {
		return fmt.Errorf("ADB Gateway 服务未配置")
	}

	// 查询 Agent
	agent, err := s.GetAgent(agentID)
	if err != nil {
		return fmt.Errorf("查询 Agent 失败: %w", err)
	}

	// 检查 Agent 是否有 mapping
	if agent.MappingID == "" {
		return fmt.Errorf("Agent 没有关联的 ADB 映射")
	}

	// 查询当前的 mapping
	mapping, err := s.adbGatewayService.GetMapping(agent.MappingID)
	if err != nil {
		return fmt.Errorf("查询 ADB 映射失败: %w", err)
	}

	// 构建新的 upstream 地址（使用 sandbox 容器名:5555）
	newUpstream := fmt.Sprintf("%s:5555", sandboxName)

	// 更新 mapping 的 upstream
	updateSpec := clients.MappingSpec{
		ID:              mapping.ID,
		Name:            mapping.Name,
		Note:            fmt.Sprintf("Agent %s 连接到 Sandbox %s", agentID, sandboxName),
		Listen:          mapping.Listen,
		Upstream:        newUpstream,
		ForceDisconnect: true, // 强制断开现有连接，立即切换
	}

	_, err = s.adbGatewayService.UpdateMapping(updateSpec)
	if err != nil {
		return fmt.Errorf("更新 ADB 映射失败: %w", err)
	}

	log.Printf("Agent %s 已切换到 Sandbox %s (upstream: %s)", agentID, sandboxName, newUpstream)
	return nil
}
