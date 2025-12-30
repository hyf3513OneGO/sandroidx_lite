package handlers

import (
	"net/http"

	"sandroidx.com/sandroidx_lite/models"
	"sandroidx.com/sandroidx_lite/services"

	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	userService    *services.UserService
	authService    *services.AuthService
	settingService *services.SystemSettingService
}

func NewAuthHandler(userService *services.UserService, authService *services.AuthService, settingService *services.SystemSettingService) *AuthHandler {
	return &AuthHandler{
		userService:    userService,
		authService:    authService,
		settingService: settingService,
	}
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type RegisterRequest struct {
	Name     string `json:"name" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

type SetupAdminRequest struct {
	Name     string `json:"name" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

// LoginResponse 登录成功响应
type LoginResponse struct {
	Token string      `json:"token"`
	User  models.User `json:"user"`
}

// StatusResponse 状态查询响应
type StatusResponse struct {
	AdminInitialized  bool `json:"admin_initialized"`
	AllowRegistration bool `json:"allow_registration"`
	AllowSandboxStart bool `json:"allow_sandbox_start"`
	MaintenanceMode   bool `json:"maintenance_mode"`
}

// Login 用户登录
// @Summary 用户登录
// @Tags auth
// @Accept json
// @Produce json
// @Param request body LoginRequest true "登录参数"
// @Success 200 {object} LoginResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Router /auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	token, user, err := h.authService.Login(req.Email, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, LoginResponse{
		Token: token,
		User:  *user,
	})
}

// Register 普通用户注册（默认 guest 角色）
// @Summary 用户注册
// @Tags auth
// @Accept json
// @Produce json
// @Param request body RegisterRequest true "注册参数"
// @Success 201 {object} LoginResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /auth/register [post]
func (h *AuthHandler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	settings, err := h.settingService.GetSettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	if !settings.AdminInitialized {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "请先完成管理员初始化"})
		return
	}

	user, err := h.userService.RegisterUser(req.Name, req.Email, req.Password)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	token, err := h.authService.GenerateToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusCreated, LoginResponse{
		Token: token,
		User:  *user,
	})
}

// SetupAdmin 首次启动设置管理员密码
// @Summary 初始化管理员
// @Tags auth
// @Accept json
// @Produce json
// @Param request body SetupAdminRequest true "初始化参数"
// @Success 201 {object} LoginResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /auth/setup-admin [post]
func (h *AuthHandler) SetupAdmin(c *gin.Context) {
	var req SetupAdminRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	user, err := h.userService.InitAdminUser(req.Name, req.Email, req.Password)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	token, err := h.authService.GenerateToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	_, _ = h.settingService.UpdateSettings(services.RuntimeSettingsUpdate{AdminInitialized: ptrBool(true)})

	c.JSON(http.StatusCreated, LoginResponse{
		Token: token,
		User:  *user,
	})
}

// Status 返回系统设置概况（用于前端判断是否需要初始化）
// @Summary 系统状态
// @Tags auth
// @Produce json
// @Success 200 {object} StatusResponse
// @Failure 500 {object} ErrorResponse
// @Router /auth/status [get]
func (h *AuthHandler) Status(c *gin.Context) {
	settings, err := h.settingService.GetSettings()
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	hasAdmin, err := h.userService.HasAdminUser()
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, StatusResponse{
		AdminInitialized:  settings.AdminInitialized && hasAdmin,
		AllowRegistration: settings.AllowRegistration,
		AllowSandboxStart: settings.AllowSandboxStart,
		MaintenanceMode:   settings.MaintenanceMode,
	})
}

func ptrBool(v bool) *bool {
	return &v
}
