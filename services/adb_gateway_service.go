package services

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"sandroidx.com/sandroidx_lite/clients"
	"sandroidx.com/sandroidx_lite/models"

	"gorm.io/gorm"
)

// AdbGatewayService ADB Gateway 服务接口
type AdbGatewayService interface {
	// 映射管理
	ListMappings() ([]clients.Mapping, error)
	GetMapping(id string) (*clients.Mapping, error)
	CreateMapping(spec clients.MappingSpec) (*clients.Mapping, error)
	UpdateMapping(spec clients.MappingSpec) (*clients.Mapping, error)
	RemoveMapping(id string) error

	// 日志查询
	GetAdbCommandLogs(mappingID string, start, end time.Time) ([]clients.AdbCommandLogEntry, error)

	// 数据库操作
	ListMappingsFromDB() ([]models.Mapping, error)
	GetMappingFromDB(id string) (*models.Mapping, error)

	// 同步操作
	SyncMappings() error
	StartPeriodicSync(interval time.Duration) (context.CancelFunc, error)
	StopPeriodicSync()

	// 容器配置管理
	UpdateContainerConfig(ctx context.Context, portRanges []string) error
}

// adbGatewayService ADB Gateway 服务实现
type adbGatewayService struct {
	client      *clients.AdbGatewayClient
	initService *AdbGatewayInitService
	syncCtx     context.Context
	syncCancel  context.CancelFunc
	syncMutex   sync.Mutex
	isSyncing   bool
}

// NewAdbGatewayService 创建新的 ADB Gateway 服务
// client: ADB Gateway 客户端实例
func NewAdbGatewayService(client *clients.AdbGatewayClient) AdbGatewayService {
	return &adbGatewayService{
		client: client,
	}
}

// NewAdbGatewayServiceWithInit 创建新的 ADB Gateway 服务（带初始化服务）
// client: ADB Gateway 客户端实例
// initService: ADB Gateway 初始化服务实例
func NewAdbGatewayServiceWithInit(client *clients.AdbGatewayClient, initService *AdbGatewayInitService) AdbGatewayService {
	return &adbGatewayService{
		client:      client,
		initService: initService,
	}
}

// ListMappings 查询所有映射
func (s *adbGatewayService) ListMappings() ([]clients.Mapping, error) {
	mappings, err := s.client.ListMappings()
	if err != nil {
		return nil, fmt.Errorf("查询映射列表失败: %w", err)
	}
	return mappings, nil
}

// GetMapping 查询单个映射
func (s *adbGatewayService) GetMapping(id string) (*clients.Mapping, error) {
	if id == "" {
		return nil, fmt.Errorf("映射 ID 不能为空")
	}

	mapping, err := s.client.GetMapping(id)
	if err != nil {
		return nil, fmt.Errorf("查询映射失败: %w", err)
	}
	return mapping, nil
}

// CreateMapping 创建映射
func (s *adbGatewayService) CreateMapping(spec clients.MappingSpec) (*clients.Mapping, error) {
	// 验证必填字段
	if spec.Name == "" {
		return nil, fmt.Errorf("映射名称不能为空")
	}

	mapping, err := s.client.CreateMapping(spec)
	if err != nil {
		return nil, fmt.Errorf("创建映射失败: %w", err)
	}

	// 同步到数据库
	if err := s.saveMappingToDB(mapping); err != nil {
		log.Printf("保存映射到数据库失败: %v", err)
		// 不返回错误，因为 API 调用已成功
	}

	return mapping, nil
}

// UpdateMapping 更新映射
func (s *adbGatewayService) UpdateMapping(spec clients.MappingSpec) (*clients.Mapping, error) {
	// 验证必填字段
	if spec.ID == "" {
		return nil, fmt.Errorf("更新映射时 ID 不能为空")
	}
	if spec.Name == "" {
		return nil, fmt.Errorf("映射名称不能为空")
	}

	mapping, err := s.client.UpdateMapping(spec)
	if err != nil {
		return nil, fmt.Errorf("更新映射失败: %w", err)
	}

	// 同步到数据库
	if err := s.saveMappingToDB(mapping); err != nil {
		log.Printf("更新映射到数据库失败: %v", err)
		// 不返回错误，因为 API 调用已成功
	}

	return mapping, nil
}

// RemoveMapping 删除映射
func (s *adbGatewayService) RemoveMapping(id string) error {
	if id == "" {
		return fmt.Errorf("映射 ID 不能为空")
	}

	if err := s.client.RemoveMapping(id); err != nil {
		return fmt.Errorf("删除映射失败: %w", err)
	}

	// 从数据库删除
	if err := models.DB.Delete(&models.Mapping{}, "id = ?", id).Error; err != nil {
		log.Printf("从数据库删除映射失败: %v", err)
		// 不返回错误，因为 API 调用已成功
	}

	return nil
}

// GetAdbCommandLogs 查询 ADB 命令日志
func (s *adbGatewayService) GetAdbCommandLogs(mappingID string, start, end time.Time) ([]clients.AdbCommandLogEntry, error) {
	if mappingID == "" {
		return nil, fmt.Errorf("映射 ID 不能为空")
	}

	if end.Before(start) || end.Equal(start) {
		return nil, fmt.Errorf("结束时间必须晚于起始时间")
	}

	// 转换为 RFC3339 格式
	startStr := start.UTC().Format(time.RFC3339)
	endStr := end.UTC().Format(time.RFC3339)

	logs, err := s.client.GetAdbCommandLogs(mappingID, startStr, endStr)
	if err != nil {
		return nil, fmt.Errorf("查询 ADB 命令日志失败: %w", err)
	}
	return logs, nil
}

// ListMappingsFromDB 从数据库查询所有映射
func (s *adbGatewayService) ListMappingsFromDB() ([]models.Mapping, error) {
	var mappings []models.Mapping
	if err := models.DB.Find(&mappings).Error; err != nil {
		return nil, fmt.Errorf("从数据库查询映射列表失败: %w", err)
	}
	return mappings, nil
}

// GetMappingFromDB 从数据库查询单个映射
func (s *adbGatewayService) GetMappingFromDB(id string) (*models.Mapping, error) {
	if id == "" {
		return nil, fmt.Errorf("映射 ID 不能为空")
	}

	var mapping models.Mapping
	if err := models.DB.First(&mapping, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("映射不存在")
		}
		return nil, fmt.Errorf("从数据库查询映射失败: %w", err)
	}
	return &mapping, nil
}

// saveMappingToDB 保存映射到数据库
func (s *adbGatewayService) saveMappingToDB(mapping *clients.Mapping) error {
	// 解析创建时间
	createdAt, err := time.Parse(time.RFC3339, mapping.CreatedAt)
	if err != nil {
		createdAt = time.Now()
	}

	dbMapping := models.Mapping{
		ID:        mapping.ID,
		ProjectID: mapping.ProjectID,
		FromID:    mapping.FromID,
		ToID:      mapping.ToID,
		Name:      mapping.Name,
		Note:      mapping.Note,
		Listen:    mapping.Listen,
		Upstream:  mapping.Upstream,
		Status:    mapping.Status,
		LastError: mapping.LastError,
		CreatedAt: createdAt,
		SyncedAt:  time.Now(),
	}

	// 如果 listen 字段不为空，检查是否存在其他映射使用了相同的 listen 值
	if dbMapping.Listen != "" {
		var existingMapping models.Mapping
		if err := models.DB.Where("listen = ? AND id != ?", dbMapping.Listen, dbMapping.ID).First(&existingMapping).Error; err == nil {
			// 发现冲突：存在另一个不同 ID 的映射使用了相同的 listen 值
			// 删除旧映射，因为 API 的数据是最新的（以 API 为准）
			log.Printf("发现 listen 冲突: 映射 %s 使用 %s，将被删除以便保存映射 %s", existingMapping.ID, dbMapping.Listen, dbMapping.ID)
			if err := models.DB.Delete(&existingMapping).Error; err != nil {
				log.Printf("删除冲突的映射 %s 失败: %v", existingMapping.ID, err)
			}
		} else if err != gorm.ErrRecordNotFound {
			// 查询出错
			return fmt.Errorf("检查 listen 冲突失败: %w", err)
		}
	}

	// 使用 Save 方法，如果存在则更新，不存在则创建
	if err := models.DB.Save(&dbMapping).Error; err != nil {
		return fmt.Errorf("保存映射到数据库失败: %w", err)
	}

	return nil
}

// SyncMappings 同步映射数据（从 API 同步到数据库）
func (s *adbGatewayService) SyncMappings() error {
	s.syncMutex.Lock()
	if s.isSyncing {
		s.syncMutex.Unlock()
		return fmt.Errorf("同步正在进行中")
	}
	s.isSyncing = true
	s.syncMutex.Unlock()

	defer func() {
		s.syncMutex.Lock()
		s.isSyncing = false
		s.syncMutex.Unlock()
	}()

	log.Println("开始同步映射数据...")

	// 从 API 获取所有映射
	apiMappings, err := s.client.ListMappings()
	if err != nil {
		return fmt.Errorf("从 API 获取映射列表失败: %w", err)
	}

	// 获取数据库中所有映射的 ID
	var dbMappingIDs []string
	if err := models.DB.Model(&models.Mapping{}).Pluck("id", &dbMappingIDs).Error; err != nil {
		return fmt.Errorf("查询数据库映射 ID 失败: %w", err)
	}

	// 创建映射 ID 集合
	dbIDMap := make(map[string]bool)
	for _, id := range dbMappingIDs {
		dbIDMap[id] = true
	}

	// 同步每个映射
	syncedCount := 0
	createdCount := 0
	updatedCount := 0

	for _, apiMapping := range apiMappings {
		// 解析创建时间
		createdAt, err := time.Parse(time.RFC3339, apiMapping.CreatedAt)
		if err != nil {
			createdAt = time.Now()
		}

		dbMapping := models.Mapping{
			ID:        apiMapping.ID,
			ProjectID: apiMapping.ProjectID,
			FromID:    apiMapping.FromID,
			ToID:      apiMapping.ToID,
			Name:      apiMapping.Name,
			Note:      apiMapping.Note,
			Listen:    apiMapping.Listen,
			Upstream:  apiMapping.Upstream,
			Status:    apiMapping.Status,
			LastError: apiMapping.LastError,
			CreatedAt: createdAt,
			SyncedAt:  time.Now(),
		}

		// 检查是否已存在
		if dbIDMap[apiMapping.ID] {
			// 更新现有记录
			// 如果 listen 字段不为空，先检查是否存在冲突
			if dbMapping.Listen != "" {
				var existingMapping models.Mapping
				if err := models.DB.Where("listen = ? AND id != ?", dbMapping.Listen, dbMapping.ID).First(&existingMapping).Error; err == nil {
					// 发现冲突：删除旧映射，以 API 数据为准
					log.Printf("同步时发现 listen 冲突: 映射 %s 使用 %s，将被删除以便更新映射 %s", existingMapping.ID, dbMapping.Listen, dbMapping.ID)
					if err := models.DB.Delete(&existingMapping).Error; err != nil {
						log.Printf("删除冲突的映射 %s 失败: %v", existingMapping.ID, err)
					}
				}
			}
			if err := models.DB.Model(&models.Mapping{}).Where("id = ?", apiMapping.ID).Updates(dbMapping).Error; err != nil {
				log.Printf("更新映射 %s 失败: %v", apiMapping.ID, err)
				continue
			}
			updatedCount++
		} else {
			// 创建新记录
			// 如果 listen 字段不为空，先检查是否存在冲突
			if dbMapping.Listen != "" {
				var existingMapping models.Mapping
				if err := models.DB.Where("listen = ?", dbMapping.Listen).First(&existingMapping).Error; err == nil {
					// 发现冲突：删除旧映射，以 API 数据为准
					log.Printf("同步时发现 listen 冲突: 映射 %s 使用 %s，将被删除以便创建映射 %s", existingMapping.ID, dbMapping.Listen, dbMapping.ID)
					if err := models.DB.Delete(&existingMapping).Error; err != nil {
						log.Printf("删除冲突的映射 %s 失败: %v", existingMapping.ID, err)
					}
				}
			}
			if err := models.DB.Create(&dbMapping).Error; err != nil {
				log.Printf("创建映射 %s 失败: %v", apiMapping.ID, err)
				continue
			}
			createdCount++
		}
		syncedCount++
	}

	// 删除数据库中已不存在的映射（可选，根据需求决定是否启用）
	// 如果 API 中不再有某个映射，但数据库中还存在，可以选择删除
	apiIDMap := make(map[string]bool)
	for _, m := range apiMappings {
		apiIDMap[m.ID] = true
	}

	deletedCount := 0
	for _, dbID := range dbMappingIDs {
		if !apiIDMap[dbID] {
			if err := models.DB.Delete(&models.Mapping{}, "id = ?", dbID).Error; err != nil {
				log.Printf("删除映射 %s 失败: %v", dbID, err)
				continue
			}
			deletedCount++
		}
	}

	log.Printf("同步完成: 总计 %d 个映射，新增 %d 个，更新 %d 个，删除 %d 个", syncedCount, createdCount, updatedCount, deletedCount)

	return nil
}

// StartPeriodicSync 启动定期同步
// interval: 同步间隔时间
// 返回取消函数，用于停止同步
func (s *adbGatewayService) StartPeriodicSync(interval time.Duration) (context.CancelFunc, error) {
	if interval <= 0 {
		return nil, fmt.Errorf("同步间隔必须大于 0")
	}

	// 如果已有同步任务在运行，先停止
	if s.syncCancel != nil {
		s.StopPeriodicSync()
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.syncCtx = ctx
	s.syncCancel = cancel

	// 立即执行一次同步
	go func() {
		if err := s.SyncMappings(); err != nil {
			log.Printf("初始同步失败: %v", err)
		}
	}()

	// 启动定期同步
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Println("定期同步已停止")
				return
			case <-ticker.C:
				if err := s.SyncMappings(); err != nil {
					log.Printf("定期同步失败: %v", err)
				}
			}
		}
	}()

	log.Printf("定期同步已启动，间隔: %v", interval)
	return cancel, nil
}

// StopPeriodicSync 停止定期同步
func (s *adbGatewayService) StopPeriodicSync() {
	if s.syncCancel != nil {
		s.syncCancel()
		s.syncCancel = nil
		log.Println("定期同步已停止")
	}
}

// UpdateContainerConfig 更新容器配置（如端口范围）
func (s *adbGatewayService) UpdateContainerConfig(ctx context.Context, portRanges []string) error {
	if s.initService == nil {
		return fmt.Errorf("初始化服务未设置，无法更新容器配置")
	}
	return s.initService.UpdateContainerConfig(ctx, portRanges)
}
