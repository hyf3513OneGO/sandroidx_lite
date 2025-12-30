package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"sandroidx.com/sandroidx_lite/services"
)

// OverviewHandler 总览处理器
type OverviewHandler struct {
	overviewService services.OverviewService
}

func NewOverviewHandler(overviewService services.OverviewService) *OverviewHandler {
	return &OverviewHandler{overviewService: overviewService}
}

func (h *OverviewHandler) RegisterRoutes(router *gin.RouterGroup) {
	router.GET("/overview", h.GetOverview)
}

// GetOverview 获取系统总览信息
// @Summary 获取系统总览信息
// @Tags overview
// @Produce json
// @Success 200 {object} services.OverviewResponse
// @Failure 500 {object} ErrorResponse
// @Router /overview [get]
func (h *OverviewHandler) GetOverview(c *gin.Context) {
	resp, err := h.overviewService.GetOverview(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, resp)
}


