package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"sandroidx.com/sandroidx_lite/models"
	"sandroidx.com/sandroidx_lite/utils"
)

// TemplateService 模板存储服务接口
type TemplateService interface {
	CreateTemplate(name, description string, content json.RawMessage) (*models.Template, error)
	GetTemplate(id string) (*models.Template, error)
	ListTemplates() ([]models.Template, error)
	UpdateTemplate(id string, name, description *string, content *json.RawMessage) (*models.Template, error)
	DeleteTemplate(id string) error
}

type templateService struct{}

// NewTemplateService 创建模板服务实例
func NewTemplateService() TemplateService {
	return &templateService{}
}

// CreateTemplate 新建模板
func (s *templateService) CreateTemplate(name, description string, content json.RawMessage) (*models.Template, error) {
	if name == "" {
		return nil, errors.New("模板名称不能为空")
	}
	if len(content) == 0 || !json.Valid(content) {
		return nil, errors.New("模板内容必须是有效的 JSON")
	}

	// 校验重名
	var cnt int64
	if err := models.DB.Model(&models.Template{}).Where("name = ?", name).Count(&cnt).Error; err != nil {
		return nil, fmt.Errorf("检查模板名称失败: %w", err)
	}
	if cnt > 0 {
		return nil, fmt.Errorf("模板名称已存在")
	}

	tpl := &models.Template{
		ID:          utils.GenerateTemplateID(),
		Name:        name,
		Description: description,
		Content:     datatypes.JSON(content),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := models.DB.Create(tpl).Error; err != nil {
		return nil, fmt.Errorf("创建模板失败: %w", err)
	}

	return tpl, nil
}

// GetTemplate 查询模板详情
func (s *templateService) GetTemplate(id string) (*models.Template, error) {
	if id == "" {
		return nil, errors.New("Template ID 不能为空")
	}

	var tpl models.Template
	if err := models.DB.First(&tpl, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("Template 不存在")
		}
		return nil, fmt.Errorf("查询模板失败: %w", err)
	}

	return &tpl, nil
}

// ListTemplates 列表
func (s *templateService) ListTemplates() ([]models.Template, error) {
	var list []models.Template
	if err := models.DB.Order("created_at desc").Find(&list).Error; err != nil {
		return nil, fmt.Errorf("查询模板列表失败: %w", err)
	}
	return list, nil
}

// UpdateTemplate 更新模板
func (s *templateService) UpdateTemplate(id string, name, description *string, content *json.RawMessage) (*models.Template, error) {
	if id == "" {
		return nil, errors.New("Template ID 不能为空")
	}

	tpl, err := s.GetTemplate(id)
	if err != nil {
		return nil, err
	}

	updates := map[string]interface{}{
		"updated_at": time.Now(),
	}
	changed := false

	if name != nil {
		if *name == "" {
			return nil, errors.New("模板名称不能为空")
		}
		var cnt int64
		if err := models.DB.Model(&models.Template{}).Where("name = ? AND id <> ?", *name, id).Count(&cnt).Error; err != nil {
			return nil, fmt.Errorf("检查模板名称失败: %w", err)
		}
		if cnt > 0 {
			return nil, fmt.Errorf("模板名称已存在")
		}
		updates["name"] = *name
		changed = true
	}

	if description != nil {
		updates["description"] = *description
		changed = true
	}

	if content != nil {
		if len(*content) == 0 || !json.Valid(*content) {
			return nil, errors.New("模板内容必须是有效的 JSON")
		}
		updates["content"] = datatypes.JSON(*content)
		changed = true
	}

	if !changed {
		return tpl, nil
	}

	if err := models.DB.Model(&models.Template{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("更新模板失败: %w", err)
	}

	return s.GetTemplate(id)
}

// DeleteTemplate 删除模板
func (s *templateService) DeleteTemplate(id string) error {
	if id == "" {
		return errors.New("Template ID 不能为空")
	}

	res := models.DB.Delete(&models.Template{}, "id = ?", id)
	if res.Error != nil {
		return fmt.Errorf("删除模板失败: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return errors.New("Template 不存在")
	}
	return nil
}
