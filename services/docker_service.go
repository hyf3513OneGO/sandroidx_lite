package services

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

// DockerService Docker 容器服务接口
type DockerService interface {
	// 容器操作
	CreateContainer(ctx context.Context, config *ContainerCreateConfig) (string, error)
	StartContainer(ctx context.Context, containerID string) error
	StopContainer(ctx context.Context, containerID string, timeout *int) error
	RestartContainer(ctx context.Context, containerID string, timeout *int) error
	RemoveContainer(ctx context.Context, containerID string, force bool) error
	PauseContainer(ctx context.Context, containerID string) error
	UnpauseContainer(ctx context.Context, containerID string) error
	KillContainer(ctx context.Context, containerID string, signal string) error

	// 容器执行
	ExecCommand(ctx context.Context, containerID string, config *ExecConfig) (*ExecResult, error)
	ExecCommandStream(ctx context.Context, containerID string, config *ExecConfig) (io.ReadCloser, error)
	ExecCommandInteractive(ctx context.Context, containerID string, config *ExecConfig) (*ExecSession, error)
	CreateExec(ctx context.Context, containerID string, config *ExecConfig) (string, error)
	StartExec(ctx context.Context, execID string, config *ExecConfig) (*types.HijackedResponse, error)
	InspectExec(ctx context.Context, execID string) (*ExecInspectResponse, error)

	// 容器查询
	ListContainers(ctx context.Context, all bool) ([]container.Summary, error)
	GetContainer(ctx context.Context, containerID string) (*container.InspectResponse, error)
	GetContainerLogs(ctx context.Context, containerID string, tail string, follow bool) (io.ReadCloser, error)
	GetContainerStats(ctx context.Context, containerID string, stream bool) (*container.StatsResponseReader, error)

	// 镜像操作
	ListImages(ctx context.Context) ([]image.Summary, error)
	ImageExists(ctx context.Context, imageName string) (bool, error)
	PullImage(ctx context.Context, imageName string) error
	RemoveImage(ctx context.Context, imageID string, force bool) error

	// 网络操作
	CreateNetwork(ctx context.Context, config *NetworkCreateConfig) (string, error)
	RemoveNetwork(ctx context.Context, networkID string) error
	ListNetworks(ctx context.Context) ([]network.Summary, error)
	GetNetwork(ctx context.Context, networkID string) (*network.Inspect, error)
	ConnectContainerToNetwork(ctx context.Context, networkID, containerID string, config *NetworkConnectConfig) error
	DisconnectContainerFromNetwork(ctx context.Context, networkID, containerID string, force bool) error
	InspectNetwork(ctx context.Context, networkID string) (*network.Inspect, error)
	PruneNetworks(ctx context.Context) error

	// 系统信息
	GetDockerInfo(ctx context.Context) (system.Info, error)
	GetDockerVersion(ctx context.Context) (types.Version, error)
}

// ContainerCreateConfig 容器创建配置
type ContainerCreateConfig struct {
	Image         string            // 镜像名称
	Name          string            // 容器名称
	Env           []string          // 环境变量
	Ports         map[string]string // 端口映射 "主机端口:容器端口"
	Volumes       map[string]string // 卷映射 "主机路径:容器路径"（已弃用，建议使用Binds）
	Binds         []string          // 卷绑定列表，格式："主机路径:容器路径[:ro]"
	NetworkMode   string            // 网络模式
	RestartPolicy string            // 重启策略: no, on-failure, always, unless-stopped
	Command       []string          // 启动命令
	WorkingDir    string            // 工作目录
	AutoRemove    bool              // 自动删除
	Detach        bool              // 后台运行
	Privileged    bool              // 特权模式 (--privileged)
	ExtraHosts    []string          // 额外的 host 映射，格式: "hostname:ip"
}

// NetworkCreateConfig 网络创建配置
type NetworkCreateConfig struct {
	Name       string            // 网络名称
	Driver     string            // 网络驱动: bridge, host, overlay, macvlan, none
	Internal   bool              // 是否为内部网络
	Attachable bool              // 是否允许手动附加容器
	EnableIPv6 bool              // 是否启用 IPv6
	Labels     map[string]string // 标签
	Options    map[string]string // 驱动选项
	IPAM       *NetworkIPAM      // IP 地址管理
}

// NetworkIPAM IP 地址管理配置
type NetworkIPAM struct {
	Driver  string              // IPAM 驱动
	Config  []NetworkIPAMConfig // IPAM 配置
	Options map[string]string   // IPAM 选项
}

// NetworkIPAMConfig IPAM 配置项
type NetworkIPAMConfig struct {
	Subnet  string // 子网
	Gateway string // 网关
	IPRange string // IP 范围
}

// NetworkConnectConfig 容器连接网络配置
type NetworkConnectConfig struct {
	Aliases        []string          // 网络别名
	IPAddress      string            // 指定 IP 地址
	IPv6Address    string            // 指定 IPv6 地址
	LinkLocalIPs   []string          // 本地链路 IP
	Links          []string          // 容器链接
	EndpointConfig map[string]string // 端点配置
}

// ExecConfig 容器执行命令配置
type ExecConfig struct {
	Cmd          []string // 要执行的命令（新增，与Command同义）
	Command      []string // 要执行的命令
	WorkingDir   string   // 工作目录
	Env          []string // 环境变量
	User         string   // 执行用户
	Privileged   bool     // 是否特权模式
	Tty          bool     // 是否分配 TTY
	AttachStdin  bool     // 是否附加标准输入
	AttachStdout bool     // 是否附加标准输出
	AttachStderr bool     // 是否附加标准错误
	Detach       bool     // 是否后台运行
}

// ExecResult 执行结果
type ExecResult struct {
	ExitCode int    // 退出码
	Output   string // 输出内容
	Error    string // 错误信息
}

// ExecSession 交互式执行会话
type ExecSession struct {
	ID     string                         // 执行 ID
	Conn   io.ReadWriteCloser             // 连接流（支持读写）
	Resize func(height, width uint) error // 调整终端大小
}

// ExecInspectResponse Exec检查响应
type ExecInspectResponse struct {
	ExitCode int  // 退出码
	Running  bool // 是否在运行
}

type dockerService struct {
	cli *client.Client
}

// NewDockerService 创建 Docker 服务实例
func NewDockerService() (DockerService, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("创建 Docker 客户端失败: %w", err)
	}

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = cli.Ping(ctx)
	if err != nil {
		return nil, fmt.Errorf("无法连接到 Docker 守护进程: %w", err)
	}

	return &dockerService{
		cli: cli,
	}, nil
}

// CreateContainer 创建容器
func (s *dockerService) CreateContainer(ctx context.Context, config *ContainerCreateConfig) (string, error) {
	// 检查是否有端口范围需要使用 CLI 方式创建
	hasPortRange := false
	if config.Ports != nil {
		for _, containerPort := range config.Ports {
			if strings.Contains(containerPort, "-") {
				hasPortRange = true
				break
			}
		}
	}

	// 如果有端口范围，使用 docker CLI 创建容器
	if hasPortRange {
		return s.createContainerWithCLI(ctx, config)
	}

	// 否则使用 API 创建（性能更好）
	return s.createContainerWithAPI(ctx, config)
}

// createContainerWithAPI 使用 Docker API 创建容器（不支持端口范围）
func (s *dockerService) createContainerWithAPI(ctx context.Context, config *ContainerCreateConfig) (string, error) {
	// 构建端口绑定
	portBindings := nat.PortMap{}
	exposedPorts := nat.PortSet{}

	if config.Ports != nil {
		for hostPort, containerPort := range config.Ports {
			// 解析主机端口
			// 支持格式:
			// - "hostIP:hostPort" 或 "hostPort"（默认 0.0.0.0）
			// - 如果 hostPort == containerPort，表示自动分配宿主机端口（设置为空字符串）
			hostIP := "0.0.0.0"
			actualHostPort := hostPort

			if strings.Contains(hostPort, ":") {
				parts := strings.SplitN(hostPort, ":", 2)
				if len(parts) == 2 {
					hostIP = parts[0]
					actualHostPort = parts[1]
				}
			}

			// 如果 hostPort 和 containerPort 相同，表示自动分配（使用空字符串）
			if hostPort == containerPort {
				actualHostPort = "" // 空字符串表示让 Docker 自动分配端口
			}

			// 单个端口格式
			port, err := nat.NewPort("tcp", containerPort)
			if err != nil {
				// 尝试 udp
				port, err = nat.NewPort("udp", containerPort)
				if err != nil {
					return "", fmt.Errorf("无效的端口格式: %s", containerPort)
				}
			}

			// 添加到 exposedPorts
			exposedPorts[port] = struct{}{}

			portBindings[port] = []nat.PortBinding{
				{
					HostIP:   hostIP,
					HostPort: actualHostPort,
				},
			}
		}
	}

	// 构建卷绑定
	binds := make([]string, 0)

	// 支持新的 Binds 格式（推荐）
	if config.Binds != nil {
		binds = append(binds, config.Binds...)
	}

	// 兼容旧的 Volumes 格式
	if config.Volumes != nil {
		for hostPath, containerPath := range config.Volumes {
			binds = append(binds, fmt.Sprintf("%s:%s", hostPath, containerPath))
		}
	}

	// 构建重启策略
	var restartPolicy container.RestartPolicyMode
	switch config.RestartPolicy {
	case "no", "":
		restartPolicy = container.RestartPolicyDisabled
	case "on-failure":
		restartPolicy = container.RestartPolicyOnFailure
	case "always":
		restartPolicy = container.RestartPolicyAlways
	case "unless-stopped":
		restartPolicy = container.RestartPolicyUnlessStopped
	default:
		restartPolicy = container.RestartPolicyDisabled
	}

	// 创建容器配置
	containerConfig := &container.Config{
		Image:        config.Image,
		Env:          config.Env,
		Cmd:          config.Command,
		WorkingDir:   config.WorkingDir,
		ExposedPorts: exposedPorts,
	}

	// 创建主机配置
	hostConfig := &container.HostConfig{
		PortBindings:  portBindings,
		Binds:         binds,
		RestartPolicy: container.RestartPolicy{Name: restartPolicy},
		AutoRemove:    config.AutoRemove,
		NetworkMode:   container.NetworkMode(config.NetworkMode),
		Privileged:    config.Privileged,
		ExtraHosts:    config.ExtraHosts,
	}

	// 创建容器
	resp, err := s.cli.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, config.Name)
	if err != nil {
		return "", fmt.Errorf("创建容器失败: %w", err)
	}

	return resp.ID, nil
}

// createContainerWithCLI 使用 docker CLI 创建容器（支持端口范围）
func (s *dockerService) createContainerWithCLI(ctx context.Context, config *ContainerCreateConfig) (string, error) {
	args := []string{"run", "-d"}

	// 容器名称
	if config.Name != "" {
		args = append(args, "--name", config.Name)
	}

	// 端口映射（支持范围）
	if config.Ports != nil {
		for hostPort, containerPort := range config.Ports {
			portMapping := fmt.Sprintf("%s:%s", hostPort, containerPort)
			args = append(args, "-p", portMapping)
		}
	}

	// 环境变量
	for _, env := range config.Env {
		args = append(args, "-e", env)
	}

	// 卷绑定
	if config.Binds != nil {
		for _, bind := range config.Binds {
			args = append(args, "-v", bind)
		}
	}
	if config.Volumes != nil {
		for hostPath, containerPath := range config.Volumes {
			args = append(args, "-v", fmt.Sprintf("%s:%s", hostPath, containerPath))
		}
	}

	// 网络模式
	if config.NetworkMode != "" {
		args = append(args, "--network", config.NetworkMode)
	}

	// 重启策略
	if config.RestartPolicy != "" {
		args = append(args, "--restart", config.RestartPolicy)
	}

	// 自动删除
	if config.AutoRemove {
		args = append(args, "--rm")
	}

	// 特权模式
	if config.Privileged {
		args = append(args, "--privileged")
	}

	// 额外的 host 映射
	for _, host := range config.ExtraHosts {
		args = append(args, "--add-host", host)
	}

	// 工作目录
	if config.WorkingDir != "" {
		args = append(args, "-w", config.WorkingDir)
	}

	// 镜像名称
	args = append(args, config.Image)

	// 命令
	args = append(args, config.Command...)

	// 执行 docker run 命令
	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("执行 docker run 失败: %w, 输出: %s", err, string(output))
	}

	// 返回容器 ID（去除换行符）
	containerID := strings.TrimSpace(string(output))
	return containerID, nil
}

// StartContainer 启动容器
func (s *dockerService) StartContainer(ctx context.Context, containerID string) error {
	err := s.cli.ContainerStart(ctx, containerID, container.StartOptions{})
	if err != nil {
		return fmt.Errorf("启动容器失败: %w", err)
	}
	return nil
}

// StopContainer 停止容器
func (s *dockerService) StopContainer(ctx context.Context, containerID string, timeout *int) error {
	opts := container.StopOptions{}
	if timeout != nil {
		opts.Timeout = timeout
	}
	err := s.cli.ContainerStop(ctx, containerID, opts)
	if err != nil {
		return fmt.Errorf("停止容器失败: %w", err)
	}
	return nil
}

// RestartContainer 重启容器
func (s *dockerService) RestartContainer(ctx context.Context, containerID string, timeout *int) error {
	opts := container.StopOptions{}
	if timeout != nil {
		opts.Timeout = timeout
	}
	err := s.cli.ContainerRestart(ctx, containerID, opts)
	if err != nil {
		return fmt.Errorf("重启容器失败: %w", err)
	}
	return nil
}

// RemoveContainer 删除容器
func (s *dockerService) RemoveContainer(ctx context.Context, containerID string, force bool) error {
	err := s.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: force})
	if err != nil {
		return fmt.Errorf("删除容器失败: %w", err)
	}
	return nil
}

// PauseContainer 暂停容器
func (s *dockerService) PauseContainer(ctx context.Context, containerID string) error {
	err := s.cli.ContainerPause(ctx, containerID)
	if err != nil {
		return fmt.Errorf("暂停容器失败: %w", err)
	}
	return nil
}

// UnpauseContainer 恢复容器
func (s *dockerService) UnpauseContainer(ctx context.Context, containerID string) error {
	err := s.cli.ContainerUnpause(ctx, containerID)
	if err != nil {
		return fmt.Errorf("恢复容器失败: %w", err)
	}
	return nil
}

// KillContainer 强制停止容器
func (s *dockerService) KillContainer(ctx context.Context, containerID string, signal string) error {
	if signal == "" {
		signal = "SIGKILL"
	}
	err := s.cli.ContainerKill(ctx, containerID, signal)
	if err != nil {
		return fmt.Errorf("强制停止容器失败: %w", err)
	}
	return nil
}

// parseDockerMultiplexedStream 解析 Docker 多路复用流格式
// Docker 在非 TTY 模式下使用多路复用流格式：
// 每个数据块前有8字节头部：[stream_type(1)][reserved(3)][size(4, big-endian)]
// stream_type: 0x01 = stdout, 0x02 = stderr
func parseDockerMultiplexedStream(data []byte) (stdout, stderr []byte) {
	var stdoutBuf, stderrBuf bytes.Buffer
	offset := 0

	for offset < len(data) {
		// 检查是否有足够的字节读取头部
		if offset+8 > len(data) {
			break
		}

		// 读取流类型
		streamType := data[offset]
		// 跳过保留的3个字节，读取数据长度（大端序）
		size := binary.BigEndian.Uint32(data[offset+4 : offset+8])
		offset += 8

		// 检查是否有足够的数据
		if offset+int(size) > len(data) {
			break
		}

		// 提取实际数据
		payload := data[offset : offset+int(size)]
		offset += int(size)

		// 根据流类型分类
		switch streamType {
		case 0x01: // stdout
			stdoutBuf.Write(payload)
		case 0x02: // stderr
			stderrBuf.Write(payload)
		}
	}

	return stdoutBuf.Bytes(), stderrBuf.Bytes()
}

// ExecCommand 在容器中执行命令并返回结果
func (s *dockerService) ExecCommand(ctx context.Context, containerID string, config *ExecConfig) (*ExecResult, error) {
	// 设置默认值
	if !config.AttachStdout && !config.AttachStderr {
		config.AttachStdout = true
		config.AttachStderr = true
	}

	// 创建执行配置
	execOptions := container.ExecOptions{
		Cmd:          config.Command,
		WorkingDir:   config.WorkingDir,
		Env:          config.Env,
		User:         config.User,
		Privileged:   config.Privileged,
		Tty:          config.Tty,
		AttachStdin:  config.AttachStdin,
		AttachStdout: config.AttachStdout,
		AttachStderr: config.AttachStderr,
	}

	// 创建执行实例
	execIDResp, err := s.cli.ContainerExecCreate(ctx, containerID, execOptions)
	if err != nil {
		return nil, fmt.Errorf("创建执行实例失败: %w", err)
	}

	// 开始执行命令
	resp, err := s.cli.ContainerExecAttach(ctx, execIDResp.ID, container.ExecAttachOptions{
		Tty: config.Tty,
	})
	if err != nil {
		return nil, fmt.Errorf("执行命令失败: %w", err)
	}
	defer resp.Close()

	// 读取输出
	rawOutput, err := io.ReadAll(resp.Reader)
	if err != nil {
		return nil, fmt.Errorf("读取命令输出失败: %w", err)
	}

	// 解析输出
	var outputStr, errorStr string
	if config.Tty {
		// TTY 模式下直接使用原始输出
		outputStr = string(rawOutput)
	} else {
		// 非 TTY 模式下需要解析多路复用流格式
		stdout, stderr := parseDockerMultiplexedStream(rawOutput)
		outputStr = string(stdout)
		errorStr = string(stderr)
	}

	// 获取执行结果
	inspect, err := s.cli.ContainerExecInspect(ctx, execIDResp.ID)
	if err != nil {
		return nil, fmt.Errorf("获取执行结果失败: %w", err)
	}

	result := &ExecResult{
		ExitCode: inspect.ExitCode,
		Output:   outputStr,
		Error:    errorStr,
	}

	return result, nil
}

// ExecCommandStream 在容器中执行命令并返回流式输出
func (s *dockerService) ExecCommandStream(ctx context.Context, containerID string, config *ExecConfig) (io.ReadCloser, error) {
	// 设置默认值
	if !config.AttachStdout && !config.AttachStderr {
		config.AttachStdout = true
		config.AttachStderr = true
	}

	// 创建执行配置
	execOptions := container.ExecOptions{
		Cmd:          config.Command,
		WorkingDir:   config.WorkingDir,
		Env:          config.Env,
		User:         config.User,
		Privileged:   config.Privileged,
		Tty:          config.Tty,
		AttachStdin:  config.AttachStdin,
		AttachStdout: config.AttachStdout,
		AttachStderr: config.AttachStderr,
	}

	// 创建执行实例
	execIDResp, err := s.cli.ContainerExecCreate(ctx, containerID, execOptions)
	if err != nil {
		return nil, fmt.Errorf("创建执行实例失败: %w", err)
	}

	// 开始执行命令
	resp, err := s.cli.ContainerExecAttach(ctx, execIDResp.ID, container.ExecAttachOptions{
		Tty: config.Tty,
	})
	if err != nil {
		return nil, fmt.Errorf("执行命令失败: %w", err)
	}

	// 返回一个包装的 ReadCloser
	return &execStreamWrapper{resp: &resp}, nil
}

// ExecCommandInteractive 在容器中执行交互式命令
func (s *dockerService) ExecCommandInteractive(ctx context.Context, containerID string, config *ExecConfig) (*ExecSession, error) {
	// 强制设置交互式参数
	config.AttachStdin = true
	config.AttachStdout = true
	config.AttachStderr = true
	config.Tty = true

	// 创建执行配置
	execOptions := container.ExecOptions{
		Cmd:          config.Command,
		WorkingDir:   config.WorkingDir,
		Env:          config.Env,
		User:         config.User,
		Privileged:   config.Privileged,
		Tty:          true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	}

	// 创建执行实例
	execIDResp, err := s.cli.ContainerExecCreate(ctx, containerID, execOptions)
	if err != nil {
		return nil, fmt.Errorf("创建执行实例失败: %w", err)
	}

	// 开始执行命令
	resp, err := s.cli.ContainerExecAttach(ctx, execIDResp.ID, container.ExecAttachOptions{
		Tty: true,
	})
	if err != nil {
		return nil, fmt.Errorf("执行命令失败: %w", err)
	}

	// 创建会话
	session := &ExecSession{
		ID:   execIDResp.ID,
		Conn: &execStreamWrapper{resp: &resp},
		Resize: func(height, width uint) error {
			return s.cli.ContainerExecResize(ctx, execIDResp.ID, container.ResizeOptions{
				Height: height,
				Width:  width,
			})
		},
	}

	return session, nil
}

// CreateExec 创建一个exec实例
func (s *dockerService) CreateExec(ctx context.Context, containerID string, config *ExecConfig) (string, error) {
	// 设置默认值
	if config.Cmd == nil {
		config.Cmd = config.Command
	}

	// 创建执行配置
	execOptions := container.ExecOptions{
		Cmd:          config.Cmd,
		WorkingDir:   config.WorkingDir,
		Env:          config.Env,
		User:         config.User,
		Privileged:   config.Privileged,
		Tty:          config.Tty,
		AttachStdin:  config.AttachStdin,
		AttachStdout: config.AttachStdout,
		AttachStderr: config.AttachStderr,
	}

	// 创建执行实例
	execIDResp, err := s.cli.ContainerExecCreate(ctx, containerID, execOptions)
	if err != nil {
		return "", fmt.Errorf("创建执行实例失败: %w", err)
	}

	return execIDResp.ID, nil
}

// StartExec 启动一个exec实例并返回hijacked响应
func (s *dockerService) StartExec(ctx context.Context, execID string, config *ExecConfig) (*types.HijackedResponse, error) {
	// 开始执行命令
	resp, err := s.cli.ContainerExecAttach(ctx, execID, container.ExecAttachOptions{
		Tty: config.Tty,
	})
	if err != nil {
		return nil, fmt.Errorf("启动exec失败: %w", err)
	}

	return &resp, nil
}

// InspectExec 检查exec实例的状态
func (s *dockerService) InspectExec(ctx context.Context, execID string) (*ExecInspectResponse, error) {
	inspect, err := s.cli.ContainerExecInspect(ctx, execID)
	if err != nil {
		return nil, fmt.Errorf("检查exec状态失败: %w", err)
	}

	return &ExecInspectResponse{
		ExitCode: inspect.ExitCode,
		Running:  inspect.Running,
	}, nil
}

// execStreamWrapper 包装 HijackedResponse 以实现 io.ReadWriteCloser
type execStreamWrapper struct {
	resp *types.HijackedResponse
}

func (w *execStreamWrapper) Read(p []byte) (n int, err error) {
	return w.resp.Reader.Read(p)
}

func (w *execStreamWrapper) Write(p []byte) (n int, err error) {
	return w.resp.Conn.Write(p)
}

func (w *execStreamWrapper) Close() error {
	w.resp.Close()
	return nil
}

// ListContainers 获取容器列表
func (s *dockerService) ListContainers(ctx context.Context, all bool) ([]container.Summary, error) {
	containers, err := s.cli.ContainerList(ctx, container.ListOptions{All: all})
	if err != nil {
		return nil, fmt.Errorf("获取容器列表失败: %w", err)
	}
	return containers, nil
}

// GetContainer 获取容器详情
func (s *dockerService) GetContainer(ctx context.Context, containerID string) (*container.InspectResponse, error) {
	containerInfo, err := s.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("获取容器信息失败: %w", err)
	}
	return &containerInfo, nil
}

// GetContainerLogs 获取容器日志
func (s *dockerService) GetContainerLogs(ctx context.Context, containerID string, tail string, follow bool) (io.ReadCloser, error) {
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tail,
	}

	logs, err := s.cli.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return nil, fmt.Errorf("获取容器日志失败: %w", err)
	}
	return logs, nil
}

// GetContainerStats 获取容器统计信息
// 返回的 StatsResponseReader 需要调用者关闭 Body
func (s *dockerService) GetContainerStats(ctx context.Context, containerID string, stream bool) (*container.StatsResponseReader, error) {
	stats, err := s.cli.ContainerStats(ctx, containerID, stream)
	if err != nil {
		return nil, fmt.Errorf("获取容器统计信息失败: %w", err)
	}
	return &stats, nil
}

// ListImages 获取镜像列表
func (s *dockerService) ListImages(ctx context.Context) ([]image.Summary, error) {
	images, err := s.cli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("获取镜像列表失败: %w", err)
	}
	return images, nil
}

// ImageExists 检查镜像是否存在
func (s *dockerService) ImageExists(ctx context.Context, imageName string) (bool, error) {
	_, _, err := s.cli.ImageInspectWithRaw(ctx, imageName)
	if err != nil {
		if client.IsErrNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("检查镜像是否存在失败: %w", err)
	}
	return true, nil
}

// PullImage 拉取镜像
func (s *dockerService) PullImage(ctx context.Context, imageName string) error {
	reader, err := s.cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("拉取镜像失败: %w", err)
	}
	defer reader.Close()

	// 读取拉取进度
	_, err = io.Copy(io.Discard, reader)
	if err != nil {
		return fmt.Errorf("读取镜像拉取进度失败: %w", err)
	}
	return nil
}

// RemoveImage 删除镜像
func (s *dockerService) RemoveImage(ctx context.Context, imageID string, force bool) error {
	_, err := s.cli.ImageRemove(ctx, imageID, image.RemoveOptions{Force: force})
	if err != nil {
		return fmt.Errorf("删除镜像失败: %w", err)
	}
	return nil
}

// GetDockerInfo 获取 Docker 系统信息
func (s *dockerService) GetDockerInfo(ctx context.Context) (system.Info, error) {
	info, err := s.cli.Info(ctx)
	if err != nil {
		return system.Info{}, fmt.Errorf("获取 Docker 信息失败: %w", err)
	}
	return info, nil
}

// GetDockerVersion 获取 Docker 版本信息
func (s *dockerService) GetDockerVersion(ctx context.Context) (types.Version, error) {
	version, err := s.cli.ServerVersion(ctx)
	if err != nil {
		return types.Version{}, fmt.Errorf("获取 Docker 版本失败: %w", err)
	}
	return version, nil
}

// CreateNetwork 创建网络
func (s *dockerService) CreateNetwork(ctx context.Context, config *NetworkCreateConfig) (string, error) {
	// 构建 IPAM 配置
	var ipamConfig *network.IPAM
	if config.IPAM != nil {
		ipamConfigs := make([]network.IPAMConfig, 0)
		for _, ipamCfg := range config.IPAM.Config {
			ipamConfigs = append(ipamConfigs, network.IPAMConfig{
				Subnet:  ipamCfg.Subnet,
				Gateway: ipamCfg.Gateway,
				IPRange: ipamCfg.IPRange,
			})
		}
		ipamConfig = &network.IPAM{
			Driver:  config.IPAM.Driver,
			Config:  ipamConfigs,
			Options: config.IPAM.Options,
		}
	}

	// 设置默认驱动
	driver := config.Driver
	if driver == "" {
		driver = "bridge"
	}

	// 创建网络
	enableIPv6 := config.EnableIPv6
	createOptions := network.CreateOptions{
		Driver:     driver,
		Internal:   config.Internal,
		Attachable: config.Attachable,
		EnableIPv6: &enableIPv6,
		IPAM:       ipamConfig,
		Labels:     config.Labels,
		Options:    config.Options,
	}

	resp, err := s.cli.NetworkCreate(ctx, config.Name, createOptions)
	if err != nil {
		return "", fmt.Errorf("创建网络失败: %w", err)
	}

	return resp.ID, nil
}

// RemoveNetwork 删除网络
func (s *dockerService) RemoveNetwork(ctx context.Context, networkID string) error {
	err := s.cli.NetworkRemove(ctx, networkID)
	if err != nil {
		return fmt.Errorf("删除网络失败: %w", err)
	}
	return nil
}

// ListNetworks 获取网络列表
func (s *dockerService) ListNetworks(ctx context.Context) ([]network.Summary, error) {
	networks, err := s.cli.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("获取网络列表失败: %w", err)
	}
	return networks, nil
}

// GetNetwork 获取网络详情
func (s *dockerService) GetNetwork(ctx context.Context, networkID string) (*network.Inspect, error) {
	networkInfo, err := s.cli.NetworkInspect(ctx, networkID, network.InspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("获取网络信息失败: %w", err)
	}
	return &networkInfo, nil
}

// ConnectContainerToNetwork 连接容器到网络
func (s *dockerService) ConnectContainerToNetwork(ctx context.Context, networkID, containerID string, config *NetworkConnectConfig) error {
	endpointConfig := &network.EndpointSettings{}

	if config != nil {
		endpointConfig.Aliases = config.Aliases
		endpointConfig.Links = config.Links

		// 设置 IP 地址配置
		if config.IPAddress != "" || config.IPv6Address != "" {
			endpointConfig.IPAMConfig = &network.EndpointIPAMConfig{
				IPv4Address: config.IPAddress,
				IPv6Address: config.IPv6Address,
			}
		}
	}

	err := s.cli.NetworkConnect(ctx, networkID, containerID, endpointConfig)
	if err != nil {
		return fmt.Errorf("连接容器到网络失败: %w", err)
	}
	return nil
}

// DisconnectContainerFromNetwork 断开容器与网络的连接
func (s *dockerService) DisconnectContainerFromNetwork(ctx context.Context, networkID, containerID string, force bool) error {
	err := s.cli.NetworkDisconnect(ctx, networkID, containerID, force)
	if err != nil {
		return fmt.Errorf("断开容器与网络的连接失败: %w", err)
	}
	return nil
}

// InspectNetwork 检查网络
func (s *dockerService) InspectNetwork(ctx context.Context, networkID string) (*network.Inspect, error) {
	networkInfo, err := s.cli.NetworkInspect(ctx, networkID, network.InspectOptions{Verbose: true})
	if err != nil {
		return nil, fmt.Errorf("检查网络失败: %w", err)
	}
	return &networkInfo, nil
}

// PruneNetworks 清理未使用的网络
func (s *dockerService) PruneNetworks(ctx context.Context) error {
	_, err := s.cli.NetworksPrune(ctx, filters.NewArgs())
	if err != nil {
		return fmt.Errorf("清理网络失败: %w", err)
	}
	return nil
}
