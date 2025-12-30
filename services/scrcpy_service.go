package services

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"image/png"
	"io"
	"log"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"sandroidx.com/sandroidx_lite/models"
)

func parseWmSize(output string) (tapW, tapH int, ok bool) {
	out := strings.ReplaceAll(output, "\r", "")
	physicalW, physicalH := 0, 0
	overrideW, overrideH := 0, 0

	parseSize := func(s string) (int, int, bool) {
		s = strings.TrimSpace(s)
		parts := strings.Split(s, "x")
		if len(parts) != 2 {
			return 0, 0, false
		}
		w, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		h, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
			return 0, 0, false
		}
		return w, h, true
	}

	for _, line := range strings.Split(out, "\n") {
		l := strings.TrimSpace(line)
		if l == "" {
			continue
		}
		ll := strings.ToLower(l)
		if strings.HasPrefix(ll, "physical size:") {
			v := strings.TrimSpace(strings.SplitN(l, ":", 2)[1])
			if w, h, ok2 := parseSize(v); ok2 {
				physicalW, physicalH = w, h
			}
			continue
		}
		if strings.HasPrefix(ll, "override size:") {
			v := strings.TrimSpace(strings.SplitN(l, ":", 2)[1])
			if w, h, ok2 := parseSize(v); ok2 {
				overrideW, overrideH = w, h
			}
			continue
		}
	}

	// input 的坐标系通常对应 override（若存在），否则 physical
	if overrideW > 0 && overrideH > 0 {
		return overrideW, overrideH, true
	}
	if physicalW > 0 && physicalH > 0 {
		return physicalW, physicalH, true
	}
	return 0, 0, false
}

// ScrcpyService Scrcpy 服务接口
type ScrcpyService interface {
	// 启动 scrcpy 会话
	StartScrcpySession(ctx context.Context, sandboxID string) (*ScrcpySession, error)
	// 停止 scrcpy 会话
	StopScrcpySession(sandboxID string) error
	// 获取活跃的会话
	GetSession(sandboxID string) (*ScrcpySession, bool)
	// 获取设备分辨率（通过截图解析）
	GetDeviceResolution(ctx context.Context, sandboxID string) (int, int, error)
}

// NALUnitType H.264 NAL单元类型
type NALUnitType uint8

const (
	NALUnitTypeUnspecified NALUnitType = 0
	NALUnitTypeSlice       NALUnitType = 1
	NALUnitTypeDPA         NALUnitType = 2
	NALUnitTypeDPB         NALUnitType = 3
	NALUnitTypeDPC         NALUnitType = 4
	NALUnitTypeIDR         NALUnitType = 5 // IDR 关键帧
	NALUnitTypeSEI         NALUnitType = 6
	NALUnitTypeSPS         NALUnitType = 7 // 序列参数集
	NALUnitTypePPS         NALUnitType = 8 // 图像参数集
	NALUnitTypeAUD         NALUnitType = 9
)

// ScrcpySession scrcpy 会话
type ScrcpySession struct {
	ID            string
	SandboxID     string
	AdbMappingID  string
	ListenAddress string
	VideoReader   io.ReadCloser
	StdoutReader  io.ReadCloser
	Cancel        context.CancelFunc
	CreatedAt     time.Time
	mutex         sync.RWMutex
	subscribers   map[string]chan []byte

	// H.264 参数集缓存(用于新连接初始化)
	cachedSPS       []byte
	cachedPPS       []byte
	cachedIDR       []byte
	spsppsMutex     sync.RWMutex
	spsPpsLocked    bool // 锁定后不再更新SPS/PPS
	nalReadBuffer   []byte
	initBroadcasted bool // 是否已向现有订阅者广播过首包(SPS+PPS+IDR)
	closeOnce       sync.Once

	// 会话保持机制
	idleTimer      *time.Timer
	idleMutex      sync.Mutex
	gracePeriod    time.Duration // 优雅期：订阅者为0后多久才真正关闭会话
	markedForClose bool          // 标记为即将关闭
}

// Close 统一清理，保证只执行一次
func (s *ScrcpySession) Close() {
	s.closeOnce.Do(func() {
		log.Printf("[scrcpy] 关闭会话: %s", s.ID)

		// 停止空闲计时器
		s.idleMutex.Lock()
		if s.idleTimer != nil {
			s.idleTimer.Stop()
			s.idleTimer = nil
		}
		s.idleMutex.Unlock()

		if s.Cancel != nil {
			s.Cancel()
		}
		if s.VideoReader != nil {
			_ = s.VideoReader.Close()
		}

		s.mutex.Lock()
		for id, ch := range s.subscribers {
			close(ch)
			delete(s.subscribers, id)
		}
		s.mutex.Unlock()
	})
}

// AddSubscriber 添加订阅者
func (s *ScrcpySession) AddSubscriber(id string) chan []byte {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// 取消空闲计时器（如果有的话）
	s.cancelIdleTimer()

	ch := make(chan []byte, 100) // 缓冲通道
	s.subscribers[id] = ch

	log.Printf("[scrcpy] 订阅者 %s 已添加，当前订阅者数: %d", id, len(s.subscribers))

	// 如果有缓存的初始化数据(SPS+PPS+IDR),立即发送给新订阅者
	initData := s.GetInitializationData()
	if len(initData) > 0 {
		log.Printf("[scrcpy] 向新订阅者 %s 发送初始化数据 (%d 字节)", id, len(initData))
		select {
		case ch <- initData:
			// 成功发送
		default:
			log.Printf("[scrcpy] 警告: 无法向新订阅者 %s 发送初始化数据", id)
		}
	}

	return ch
}

// RemoveSubscriber 移除订阅者
func (s *ScrcpySession) RemoveSubscriber(id string) {
	s.mutex.Lock()
	if ch, ok := s.subscribers[id]; ok {
		close(ch)
		delete(s.subscribers, id)
	}
	subscriberCount := len(s.subscribers)
	s.mutex.Unlock()

	log.Printf("[scrcpy] 订阅者 %s 已移除，剩余订阅者: %d", id, subscriberCount)

	// 如果没有订阅者了，启动空闲计时器
	if subscriberCount == 0 {
		s.startIdleTimer()
	}
}

// startIdleTimer 启动空闲计时器，在优雅期后关闭会话
func (s *ScrcpySession) startIdleTimer() {
	s.idleMutex.Lock()
	defer s.idleMutex.Unlock()

	// 如果已经标记为关闭，不再启动计时器
	if s.markedForClose {
		return
	}

	// 停止旧的计时器
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}

	gracePeriod := s.gracePeriod
	if gracePeriod == 0 {
		gracePeriod = 30 * time.Second // 默认30秒
	}

	log.Printf("[scrcpy] 会话 %s 无订阅者，将在 %v 后关闭", s.ID, gracePeriod)

	s.idleTimer = time.AfterFunc(gracePeriod, func() {
		s.idleMutex.Lock()
		// 再次检查是否有订阅者（可能在等待期间重新连接）
		s.mutex.RLock()
		subscriberCount := len(s.subscribers)
		s.mutex.RUnlock()

		if subscriberCount == 0 {
			s.markedForClose = true
			s.idleMutex.Unlock()
			log.Printf("[scrcpy] 会话 %s 空闲超时，准备关闭", s.ID)
			s.Close()
		} else {
			s.idleMutex.Unlock()
			log.Printf("[scrcpy] 会话 %s 有新订阅者加入，取消关闭", s.ID)
		}
	})
}

// cancelIdleTimer 取消空闲计时器（有新订阅者时调用）
func (s *ScrcpySession) cancelIdleTimer() {
	s.idleMutex.Lock()
	defer s.idleMutex.Unlock()

	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
		log.Printf("[scrcpy] 会话 %s 取消空闲计时器", s.ID)
	}
}

// Broadcast 广播视频数据给所有订阅者
func (s *ScrcpySession) Broadcast(data []byte) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	for id, ch := range s.subscribers {
		select {
		case ch <- data:
			// 成功发送
		default:
			// 通道已满，跳过这一帧
			log.Printf("订阅者 %s 的通道已满，跳过此帧", id)
		}
	}
}

// SubscriberCount 返回订阅者数量
func (s *ScrcpySession) SubscriberCount() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return len(s.subscribers)
}

// GetInitializationData 获取缓存的SPS+PPS+IDR用于初始化新连接
func (s *ScrcpySession) GetInitializationData() []byte {
	s.spsppsMutex.RLock()
	defer s.spsppsMutex.RUnlock()

	if len(s.cachedSPS) == 0 || len(s.cachedPPS) == 0 {
		return nil
	}

	// 返回 SPS + PPS (+ IDR 如果可用)
	var result []byte
	result = append(result, s.cachedSPS...)
	result = append(result, s.cachedPPS...)
	if len(s.cachedIDR) > 0 {
		result = append(result, s.cachedIDR...)
	}

	return result
}

// CacheNALUnits 解析并缓存SPS/PPS/IDR NAL单元
func (s *ScrcpySession) CacheNALUnits(data []byte) {
	nalUnits := findNALUnits(data)

	s.spsppsMutex.Lock()
	shouldBroadcastInit := false
	var initData []byte

	for _, nal := range nalUnits {
		nalType := nal.Type
		nalData := data[nal.Start : nal.Start+nal.Size]

		switch nalType {
		case NALUnitTypeSPS:
			// 只在未锁定时缓存SPS
			if !s.spsPpsLocked && len(s.cachedSPS) == 0 && nal.Size >= 10 {
				s.cachedSPS = make([]byte, len(nalData))
				copy(s.cachedSPS, nalData)
				log.Printf("[scrcpy] ✓ 缓存 SPS (%d 字节)", nal.Size)
			}
		case NALUnitTypePPS:
			// 只在未锁定时缓存PPS
			if !s.spsPpsLocked && len(s.cachedPPS) == 0 && nal.Size >= 6 {
				s.cachedPPS = make([]byte, len(nalData))
				copy(s.cachedPPS, nalData)
				log.Printf("[scrcpy] ✓ 缓存 PPS (%d 字节)", nal.Size)
			}
		case NALUnitTypeIDR:
			// IDR帧始终更新到最新的(用于快速开始播放)
			// 与客户端策略保持一致，要求 IDR 至少 1KB 以避免缓存不完整帧
			if len(s.cachedSPS) > 0 && len(s.cachedPPS) > 0 && nal.Size >= 1024 {
				isFirst := len(s.cachedIDR) == 0
				s.cachedIDR = make([]byte, len(nalData))
				copy(s.cachedIDR, nalData)
				if isFirst {
					log.Printf("[scrcpy] ✓ 缓存 IDR 帧 (%d 字节)", nal.Size)
				}
			}
		}
	}

	// 锁定SPS/PPS一旦都获取到
	if !s.spsPpsLocked && len(s.cachedSPS) > 0 && len(s.cachedPPS) > 0 {
		s.spsPpsLocked = true
		log.Printf("[scrcpy] 🔒 SPS/PPS 已锁定 (IDR 将继续更新)")
	}

	// 首次获取到完整的初始化三件套后，主动向所有订阅者补发首包
	if s.spsPpsLocked && len(s.cachedIDR) > 0 && !s.initBroadcasted {
		s.initBroadcasted = true
		initData = append(initData, s.cachedSPS...)
		initData = append(initData, s.cachedPPS...)
		initData = append(initData, s.cachedIDR...)
		shouldBroadcastInit = true
		log.Printf("[scrcpy] ✓ 首次初始化片段准备完毕，广播给现有订阅者 (%d 字节)", len(initData))
	}

	s.spsppsMutex.Unlock()

	if shouldBroadcastInit && len(initData) > 0 {
		s.Broadcast(initData)
	}
}

// NALUnit NAL单元信息
type NALUnit struct {
	Start      int
	Type       NALUnitType
	Size       int
	IsComplete bool
}

// findNALUnits 在H.264数据中查找NAL单元
func findNALUnits(data []byte) []NALUnit {
	var nalUnits []NALUnit
	dataLen := len(data)
	i := 0

	for i < dataLen-4 {
		startCodeLen := 0

		// 查找起始码: 0x00 0x00 0x00 0x01 或 0x00 0x00 0x01
		if i+3 < dataLen && data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x00 && data[i+3] == 0x01 {
			startCodeLen = 4
		} else if i+2 < dataLen && data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x01 {
			startCodeLen = 3
		} else {
			i++
			continue
		}

		// NAL单元类型在起始码后第一个字节的低5位
		nalStart := i + startCodeLen
		if nalStart >= dataLen {
			break
		}

		nalType := NALUnitType(data[nalStart] & 0x1F)

		// 查找下一个起始码以确定NAL单元大小
		nextStart := nalStart + 1
		foundNext := false
		for nextStart < dataLen-3 {
			if (nextStart+3 < dataLen && data[nextStart] == 0x00 && data[nextStart+1] == 0x00 &&
				data[nextStart+2] == 0x00 && data[nextStart+3] == 0x01) ||
				(nextStart+2 < dataLen && data[nextStart] == 0x00 && data[nextStart+1] == 0x00 &&
					data[nextStart+2] == 0x01) {
				foundNext = true
				break
			}
			nextStart++
		}

		if !foundNext {
			nextStart = dataLen
		}

		nalSize := nextStart - i
		nalUnits = append(nalUnits, NALUnit{
			Start:      i,
			Type:       nalType,
			Size:       nalSize,
			IsComplete: foundNext,
		})

		i = nextStart
	}

	return nalUnits
}

// readCloser 组合 reader 与 closer
type readCloser struct {
	r io.Reader
	c io.Closer
}

func (rc *readCloser) Read(p []byte) (int, error) { return rc.r.Read(p) }
func (rc *readCloser) Close() error               { return rc.c.Close() }

func isAnnexBStartCode4(b []byte) bool {
	return len(b) >= 4 && b[0] == 0 && b[1] == 0 && b[2] == 0 && b[3] == 1
}

func isAnnexBStartCode3(b []byte) bool {
	return len(b) >= 3 && b[0] == 0 && b[1] == 0 && b[2] == 1
}

// readNALUnit 从VideoReader读取一个完整的NAL单元
func (s *ScrcpySession) readNALUnit() ([]byte, error) {
	chunk := make([]byte, 512*1024)
	for {
		// 在缓冲区中查找起始码
		buffer := s.nalReadBuffer
		startPositions := []int{}

		// 查找所有起始码位置
		for i := 0; i < len(buffer)-3; i++ {
			if buffer[i] == 0x00 && buffer[i+1] == 0x00 {
				if i+3 < len(buffer) && buffer[i+2] == 0x00 && buffer[i+3] == 0x01 {
					startPositions = append(startPositions, i)
					i += 3
				} else if buffer[i+2] == 0x01 {
					startPositions = append(startPositions, i)
					i += 2
				}
			}
		}

		// 如果有至少2个起始码,可以提取第一个完整NAL单元
		if len(startPositions) >= 2 {
			nalUnit := buffer[startPositions[0]:startPositions[1]]
			s.nalReadBuffer = buffer[startPositions[1]:]

			// 缓存参数集
			s.CacheNALUnits(nalUnit)

			return nalUnit, nil
		}

		// 需要更多数据 - 从socket读取
		n, err := s.VideoReader.Read(chunk)

		if err != nil {
			if err == io.EOF {
				// Socket关闭 - 返回剩余缓冲数据作为最后的NAL单元
				if len(s.nalReadBuffer) > 0 {
					finalNal := s.nalReadBuffer
					s.nalReadBuffer = nil
					s.CacheNALUnits(finalNal)
					return finalNal, nil
				}
			}
			return nil, err
		}

		if n > 0 {
			s.nalReadBuffer = append(s.nalReadBuffer, chunk[:n]...)
		}

		// 缓冲区过大时保留末尾 1KB，避免异常数据导致内存膨胀
		if len(s.nalReadBuffer) > 5*1024*1024 {
			log.Printf("[scrcpy] 警告: NAL 读取缓冲区过大，截断保留末尾 1KB (当前: %d)", len(s.nalReadBuffer))
			if len(s.nalReadBuffer) > 1024 {
				s.nalReadBuffer = s.nalReadBuffer[len(s.nalReadBuffer)-1024:]
			}
		}
	}
}

type scrcpyService struct {
	sessions          map[string]*ScrcpySession
	sessionsMutex     sync.RWMutex
	adbGatewayService AdbGatewayService
	sandboxService    SandboxService
}

// NewScrcpyService 创建新的 Scrcpy 服务
func NewScrcpyService(adbGatewayService AdbGatewayService, sandboxService SandboxService) ScrcpyService {
	return &scrcpyService{
		sessions:          make(map[string]*ScrcpySession),
		adbGatewayService: adbGatewayService,
		sandboxService:    sandboxService,
	}
}

// GetDeviceResolution 通过 adb 截图解析设备分辨率
func (s *scrcpyService) GetDeviceResolution(ctx context.Context, sandboxID string) (int, int, error) {
	if s.sandboxService == nil {
		return 0, 0, fmt.Errorf("sandboxService 未初始化")
	}

	adbDevice, err := s.sandboxService.GetAdbDeviceAddress(ctx, sandboxID)
	if err != nil {
		return 0, 0, fmt.Errorf("获取 ADB 设备失败: %w", err)
	}

	// 确保连接在线
	connectCmd := exec.CommandContext(ctx, "adb", "connect", adbDevice)
	if output, err := connectCmd.CombinedOutput(); err != nil {
		out := strings.TrimSpace(string(output))
		if !strings.Contains(out, "already connected") && !strings.Contains(out, "connected to") {
			return 0, 0, fmt.Errorf("adb connect 失败: %w, 输出: %s", err, out)
		}
	}

	// 优先使用 wm size（更贴近 input tap 的坐标系；存在 Override 时优先 Override）
	wmCmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "shell", "wm", "size")
	if output, err := wmCmd.CombinedOutput(); err == nil {
		if w, h, ok := parseWmSize(string(output)); ok {
			return w, h, nil
		}
	}

	// 直接拉取 PNG 截图
	captureCmd := exec.CommandContext(ctx, "adb", "-s", adbDevice, "exec-out", "screencap", "-p")
	var buf bytes.Buffer
	captureCmd.Stdout = &buf
	if err := captureCmd.Run(); err != nil {
		return 0, 0, fmt.Errorf("获取截图失败: %w", err)
	}

	cfg, err := png.DecodeConfig(bytes.NewReader(buf.Bytes()))
	if err != nil {
		return 0, 0, fmt.Errorf("解析截图失败: %w", err)
	}

	return cfg.Width, cfg.Height, nil
}

// StartScrcpySession 启动 scrcpy 会话
func (s *scrcpyService) StartScrcpySession(ctx context.Context, sandboxID string) (*ScrcpySession, error) {
	// 1. 查询 Sandbox 信息
	var sandbox models.Sandbox
	if err := models.DB.First(&sandbox, "id = ?", sandboxID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("sandbox 不存在")
		}
		return nil, fmt.Errorf("查询 sandbox 失败: %w", err)
	}

	// 2. 检查是否已有会话（广播模式：多个订阅者共享一个会话）
	s.sessionsMutex.RLock()
	existingSession, exists := s.sessions[sandboxID]
	s.sessionsMutex.RUnlock()

	if exists {
		log.Printf("[scrcpy] Sandbox %s 已有活跃会话: %s (订阅者数: %d)",
			sandboxID, existingSession.ID, existingSession.SubscriberCount())
		log.Printf("[scrcpy] 新订阅者将加入现有会话（广播模式），无需重启进程")
		return existingSession, nil
	}

	// 3. 没有会话，需要启动新的 scrcpy-server
	log.Printf("[scrcpy] Sandbox %s 无活跃会话，准备启动新的 scrcpy-server", sandboxID)

	// 3.1 确保端口已分配
	scrcpyPort, err := s.ensureScrcpyPort(ctx, sandboxID)
	if err != nil {
		return nil, fmt.Errorf("确保 scrcpy 端口失败: %w", err)
	}

	// 3.2 启动 scrcpy-server 进程
	scrcpyPort, err = s.setupScrcpyServer(ctx, sandboxID)
	if err != nil {
		return nil, fmt.Errorf("启动 scrcpy-server 失败: %w", err)
	}

	// 4. 尝试建立会话，失败后自动重试一次
	session, err := s.createSession(ctx, sandbox, scrcpyPort)
	if err == nil {
		return session, nil
	}

	log.Printf("[scrcpy] 首次连接失败，重试一次: %v", err)
	scrcpyPort, err = s.setupScrcpyServer(ctx, sandboxID)
	if err != nil {
		return nil, fmt.Errorf("重试启动 scrcpy-server 失败: %w", err)
	}

	session, err = s.createSession(ctx, sandbox, scrcpyPort)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (s *scrcpyService) ensureScrcpyPort(ctx context.Context, sandboxID string) (int, error) {
	if s.sandboxService == nil {
		return 0, fmt.Errorf("sandboxService 未初始化")
	}
	return s.sandboxService.EnsureScrcpyForward(ctx, sandboxID)
}

func (s *scrcpyService) setupScrcpyServer(ctx context.Context, sandboxID string) (int, error) {
	if s.sandboxService == nil {
		return 0, fmt.Errorf("sandboxService 未初始化")
	}
	return s.sandboxService.SetupScrcpyForwardIfNeeded(ctx, sandboxID)
}

func (s *scrcpyService) createSession(ctx context.Context, sandbox models.Sandbox, scrcpyPort int) (*ScrcpySession, error) {
	ctx2, cancel := context.WithCancel(ctx)

	forwardAddress := fmt.Sprintf("127.0.0.1:%d", scrcpyPort)
	log.Printf("连接到 scrcpy forward 端口: %s", forwardAddress)

	const (
		maxConnAttempts   = 3
		firstWait         = 1500 * time.Millisecond // 让 scrcpy-server 有时间启动
		retryWait         = 1500 * time.Millisecond
		firstReadDeadline = 3 * time.Second
	)

	var lastErr error

	for attempt := 1; attempt <= maxConnAttempts; attempt++ {
		if attempt == 1 {
			time.Sleep(firstWait)
		} else {
			time.Sleep(retryWait)
		}

		log.Printf("[scrcpy] 尝试连接 %s (第 %d/%d 次)", forwardAddress, attempt, maxConnAttempts)
		conn, err := net.DialTimeout("tcp", forwardAddress, 5*time.Second)
		if err != nil {
			lastErr = err
			log.Printf("[scrcpy] TCP 连接失败，重试 %d/%d: %v", attempt, maxConnAttempts, err)
			continue
		}

		log.Printf("[scrcpy] TCP 连接成功，等待视频数据...")

		// 为读取协议头加超时，避免长时间阻塞
		_ = conn.SetReadDeadline(time.Now().Add(firstReadDeadline))
		br := bufio.NewReaderSize(conn, 1024*1024)

		// 跳过任意前置数据直到遇到 AnnexB 起始码（不假定 64B 设备名）
		const maxMetadata = 1024
		discarded := 0
		metadataErr := false
		for {
			peek4, err := br.Peek(4)
			if err != nil {
				if err == io.EOF {
					log.Printf("[scrcpy] 读取数据时遇到 EOF (已丢弃 %d 字节)，可能 scrcpy-server 未就绪", discarded)
				} else {
					log.Printf("[scrcpy] 读取数据失败: %v (已丢弃 %d 字节)", err, discarded)
				}
				conn.Close()
				lastErr = fmt.Errorf("读取视频数据失败: %w", err)
				metadataErr = true
				break
			}
			if isAnnexBStartCode4(peek4) || isAnnexBStartCode3(peek4[:3]) {
				if discarded > 0 {
					log.Printf("[scrcpy] 丢弃前置 metadata: %d 字节", discarded)
				}
				log.Printf("[scrcpy] 找到 H.264 起始码，视频流就绪")
				break
			}
			if discarded >= maxMetadata {
				conn.Close()
				lastErr = fmt.Errorf("metadata 超过 %d 字节仍未找到起始码", maxMetadata)
				log.Printf("[scrcpy] %v", lastErr)
				metadataErr = true
				break
			}
			if _, err := br.Discard(1); err != nil {
				conn.Close()
				lastErr = fmt.Errorf("丢弃 metadata 失败: %w", err)
				log.Printf("[scrcpy] %v", lastErr)
				metadataErr = true
				break
			}
			discarded++
		}
		if metadataErr {
			log.Printf("[scrcpy] 第 %d/%d 次尝试失败，将重试", attempt, maxConnAttempts)
			continue
		}

		_ = conn.SetReadDeadline(time.Time{})

		sessionID := fmt.Sprintf("scrcpy_%s_%d", sandbox.ID, time.Now().Unix())
		rc := &readCloser{r: br, c: conn}

		// 后端系统操作使用 AdbMappingID（系统映射）
		session := &ScrcpySession{
			ID:            sessionID,
			SandboxID:     sandbox.ID,
			AdbMappingID:  sandbox.AdbMappingID,
			ListenAddress: forwardAddress,
			VideoReader:   rc,
			StdoutReader:  rc,
			Cancel:        cancel,
			CreatedAt:     time.Now(),
			subscribers:   make(map[string]chan []byte),
			nalReadBuffer: make([]byte, 0, 1024*1024),
			gracePeriod:   30 * time.Second, // 30秒优雅期，允许页面刷新后快速重连
		}

		s.sessionsMutex.Lock()
		s.sessions[sandbox.ID] = session
		s.sessionsMutex.Unlock()

		go s.broadcastVideo(ctx2, session)

		log.Printf("Scrcpy 会话已创建: %s (端口: %d)", sessionID, scrcpyPort)
		return session, nil
	}

	cancel()
	return nil, fmt.Errorf("连接 scrcpy forward 端口失败: %w", lastErr)
}

// broadcastVideo 读取视频流并广播给所有订阅者
func (s *scrcpyService) broadcastVideo(ctx context.Context, session *ScrcpySession) {
	defer func() {
		log.Printf("[scrcpy] 视频广播结束: %s", session.ID)

		// 确保会话从管理器中移除
		s.sessionsMutex.Lock()
		if s.sessions[session.SandboxID] == session {
			delete(s.sessions, session.SandboxID)
		}
		s.sessionsMutex.Unlock()

		// 关闭会话资源
		session.Close()
	}()

	log.Printf("[scrcpy] 开始读取视频流: %s", session.ID)

	// 使用NAL单元方式读取(确保每次发送完整的NAL单元)
	nalCount := 0
	for {
		select {
		case <-ctx.Done():
			log.Printf("[scrcpy] 上下文取消，停止视频广播: %s", session.ID)
			return
		default:
		}

		// 检查是否被标记为关闭
		session.idleMutex.Lock()
		markedForClose := session.markedForClose
		session.idleMutex.Unlock()

		if markedForClose {
			log.Printf("[scrcpy] 会话已标记关闭，停止视频广播: %s", session.ID)
			return
		}

		nalUnit, err := session.readNALUnit()
		if err != nil {
			if err != io.EOF {
				log.Printf("[scrcpy] ✗ 读取NAL单元失败: %v (sandbox: %s)", err, session.SandboxID)
			} else {
				log.Printf("[scrcpy] 视频流读取到 EOF (sandbox: %s)", session.SandboxID)
			}
			return
		}

		if len(nalUnit) > 0 {
			nalCount++

			// 打印前几次读取的NAL单元类型，用于调试
			if nalCount <= 10 {
				nalType := NALUnitType(0)
				// 查找第一个NAL类型
				nals := findNALUnits(nalUnit)
				if len(nals) > 0 {
					nalType = nals[0].Type
				}
				log.Printf("[scrcpy] NAL#%d: %d 字节, 类型=%d, 缓冲区=%d, 订阅者=%d",
					nalCount, len(nalUnit), nalType, len(session.nalReadBuffer), session.SubscriberCount())
			} else if nalCount%100 == 0 {
				log.Printf("[scrcpy] 已读取 %d 个NAL单元 (订阅者: %d, 缓冲区=%d)", nalCount, session.SubscriberCount(), len(session.nalReadBuffer))
			}

			// 广播NAL单元
			session.Broadcast(nalUnit)
		}
	}
}

// StopScrcpySession 停止 scrcpy 会话
func (s *scrcpyService) StopScrcpySession(sandboxID string) error {
	s.sessionsMutex.Lock()
	session, exists := s.sessions[sandboxID]
	if exists {
		delete(s.sessions, sandboxID)
	}
	s.sessionsMutex.Unlock()

	if !exists {
		return fmt.Errorf("会话不存在")
	}

	session.Close()
	log.Printf("已停止 scrcpy 会话: %s", session.ID)
	return nil
}

// GetSession 获取会话
func (s *scrcpyService) GetSession(sandboxID string) (*ScrcpySession, bool) {
	s.sessionsMutex.RLock()
	defer s.sessionsMutex.RUnlock()

	session, exists := s.sessions[sandboxID]
	return session, exists
}
