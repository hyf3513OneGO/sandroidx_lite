package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"sandroidx.com/sandroidx_lite/services"
)

// TemplateHandler 模板管理
type TemplateHandler struct {
	templateService services.TemplateService
}

// NewTemplateHandler 创建模板处理器
func NewTemplateHandler(templateService services.TemplateService) *TemplateHandler {
	return &TemplateHandler{templateService: templateService}
}

// RegisterRoutes 注册模板路由
func (h *TemplateHandler) RegisterRoutes(router *gin.RouterGroup) {
	templates := router.Group("/templates")
	{
		templates.POST("/create", h.CreateTemplate)
		templates.GET("/list", h.ListTemplates)
		templates.GET("/detail/:id", h.GetTemplate)
		templates.POST("/update/:id", h.UpdateTemplate)
		templates.POST("/delete/:id", h.DeleteTemplate)
	}
}

// CreateTemplateRequest 创建请求
type CreateTemplateRequest struct {
	Name        string          `json:"name" binding:"required"`
	Description string          `json:"description"`
	Content     json.RawMessage `json:"content" binding:"required" swaggertype:"object"`
}

// TemplateResponse 用于 swagger 展示，Content 视为任意 JSON 对象
type TemplateResponse struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Content     json.RawMessage `json:"content" swaggertype:"object"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

// UpdateTemplateRequest 更新请求
type UpdateTemplateRequest struct {
	Name        *string          `json:"name"`
	Description *string          `json:"description"`
	Content     *json.RawMessage `json:"content" swaggertype:"object"`
}

// CreateTemplate 新建模板
// @Summary 创建模板
// @Description 存储一份 Agent/Sandbox 模板
// @Tags templates
// @Accept json
// @Produce json
// @Param request body CreateTemplateRequest true "模板数据"
// @Success 200 {object} TemplateResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /templates/create [post]
func (h *TemplateHandler) CreateTemplate(c *gin.Context) {
	var req CreateTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "无效的请求参数: " + err.Error()})
		return
	}

	tpl, err := h.templateService.CreateTemplate(req.Name, req.Description, req.Content)
	if err != nil {
		log.Printf("创建模板失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, tpl)
}

// GetTemplate 查询模板详情
// @Summary 查询模板详情
// @Description 根据 ID 查询模板内容
// @Tags templates
// @Produce json
// @Param id path string true "Template ID"
// @Success 200 {object} TemplateResponse
// @Failure 404 {object} ErrorResponse
// @Failure 400 {object} ErrorResponse
// @Router /templates/detail/{id} [get]
func (h *TemplateHandler) GetTemplate(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Template ID 不能为空"})
		return
	}

	tpl, err := h.templateService.GetTemplate(id)
	if err != nil {
		if err.Error() == "Template 不存在" {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("查询模板失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, tpl)
}

// ListTemplates 模板列表
// @Summary 模板列表
// @Description 获取模板列表
// @Tags templates
// @Produce json
// @Success 200 {array} TemplateResponse
// @Failure 500 {object} ErrorResponse
// @Router /templates/list [get]
func (h *TemplateHandler) ListTemplates(c *gin.Context) {
	list, err := h.templateService.ListTemplates()
	if err != nil {
		log.Printf("查询模板列表失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// UpdateTemplate 更新模板
// @Summary 更新模板
// @Description 修改模板名称/描述/内容
// @Tags templates
// @Accept json
// @Produce json
// @Param id path string true "Template ID"
// @Param request body UpdateTemplateRequest true "待更新字段"
// @Success 200 {object} TemplateResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /templates/update/{id} [post]
func (h *TemplateHandler) UpdateTemplate(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Template ID 不能为空"})
		return
	}

	var req UpdateTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "无效的请求参数: " + err.Error()})
		return
	}

	tpl, err := h.templateService.UpdateTemplate(id, req.Name, req.Description, req.Content)
	if err != nil {
		if err.Error() == "Template 不存在" {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("更新模板失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, tpl)
}

// DeleteTemplate 删除模板
// @Summary 删除模板
// @Description 根据 ID 删除模板
// @Tags templates
// @Produce json
// @Param id path string true "Template ID"
// @Success 200 {object} SuccessResponse
// @Failure 404 {object} ErrorResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /templates/delete/{id} [post]
func (h *TemplateHandler) DeleteTemplate(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Template ID 不能为空"})
		return
	}

	if err := h.templateService.DeleteTemplate(id); err != nil {
		if err.Error() == "Template 不存在" {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("删除模板失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{Message: "模板已删除"})
}
