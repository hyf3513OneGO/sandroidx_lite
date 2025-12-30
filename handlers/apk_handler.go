package handlers

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"sandroidx.com/sandroidx_lite/configs"
	"sandroidx.com/sandroidx_lite/services"
)

// ApkHandler APK 管理
type ApkHandler struct {
	apkService services.ApkService
}

func NewApkHandler(apkService services.ApkService) *ApkHandler {
	return &ApkHandler{apkService: apkService}
}

// RegisterRoutes 注册 APK 路由
// 注意：图标 API (/apks/icon/*filepath) 已在 main.go 中注册为公开端点，这里不再重复注册
func (h *ApkHandler) RegisterRoutes(router *gin.RouterGroup) {
	apks := router.Group("/apks")
	{
		apks.POST("/create", h.CreateApk)
		apks.POST("/upload", h.UploadApk)
		apks.POST("/upload-preview", h.UploadApkPreview)
		apks.POST("/download/:id", h.DownloadApk)
		apks.POST("/download-preview", h.DownloadApkPreview)
		apks.GET("/list", h.ListApks)
		apks.GET("/detail/:id", h.GetApk)
		apks.POST("/update/:id", h.UpdateApk)
		apks.POST("/delete/:id", h.DeleteApk)
	}
}

// UploadApk 上传本地 APK（multipart/form-data）
// @Summary 上传本地 APK
// @Tags apks
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "APK 文件"
// @Param name formData string true "名称"
// @Param package_name formData string true "包名"
// @Param version formData string true "版本"
// @Param description formData string false "描述"
// @Success 200 {object} models.Apk
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /apks/upload [post]
func (h *ApkHandler) UploadApk(c *gin.Context) {
	// 检查是否有临时文件路径（预览后保存）
	tempPath := c.PostForm("temp_path")
	if tempPath != "" {
		// 使用临时文件创建 APK
		name := c.PostForm("name")
		pkg := c.PostForm("package_name")
		ver := c.PostForm("version")
		desc := c.PostForm("description")

		apk, err := h.apkService.UploadLocalApkFromTemp(tempPath, name, pkg, ver, desc)
		if err != nil {
			if err.Error() == "临时文件不存在" ||
				err.Error() == "名称不能为空" ||
				err.Error() == "无法解析包名，请手动填写" ||
				err.Error() == "无法解析版本，请手动填写" ||
				err.Error() == "包名+版本已存在" ||
				strings.HasPrefix(err.Error(), "移动文件失败") ||
				strings.HasPrefix(err.Error(), "data_path") {
				c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
				return
			}
			log.Printf("从临时文件创建 APK 失败: %v", err)
			c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}
		c.JSON(http.StatusOK, apk)
		return
	}

	// 传统方式：上传文件
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "未选择文件"})
		return
	}
	name := c.PostForm("name")
	pkg := c.PostForm("package_name")
	ver := c.PostForm("version")
	desc := c.PostForm("description")

	apk, err := h.apkService.UploadLocalApk(file, name, pkg, ver, desc)
	if err != nil {
		if err.Error() == "未选择文件" ||
			err.Error() == "名称不能为空" ||
			err.Error() == "无法解析包名，请手动填写" ||
			err.Error() == "无法解析版本，请手动填写" ||
			err.Error() == "包名+版本已存在" ||
			strings.HasPrefix(err.Error(), "data_path") {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("上传 APK 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, apk)
}

// DownloadApk 下载 remote APK 并落盘
// @Summary 下载 remote APK
// @Tags apks
// @Produce json
// @Param id path string true "APK ID"
// @Success 200 {object} models.Apk
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /apks/download/{id} [post]
func (h *ApkHandler) DownloadApk(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "ID 不能为空"})
		return
	}
	apk, err := h.apkService.DownloadRemoteApk(id)
	if err != nil {
		if err.Error() == "APK 不存在" {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		if err.Error() == "仅 remote 类型支持下载" ||
			err.Error() == "remote 类型必须提供 url" ||
			err.Error() == "url 必须是有效 URL" ||
			err.Error() == "url 仅支持 http/https" ||
			strings.HasPrefix(err.Error(), "下载失败") ||
			strings.HasPrefix(err.Error(), "data_path") {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("下载 APK 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, apk)
}

// UploadApkPreview 上传本地 APK 并解析信息（不创建记录）
// @Summary 上传本地 APK 预览（解析包名和版本）
// @Tags apks
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "APK 文件"
// @Success 200 {object} services.ApkPreviewResult
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /apks/upload-preview [post]
func (h *ApkHandler) UploadApkPreview(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "未选择文件"})
		return
	}

	result, err := h.apkService.UploadLocalApkPreview(file)
	if err != nil {
		if strings.HasPrefix(err.Error(), "上传的文件不是有效的 APK") ||
			strings.HasPrefix(err.Error(), "打开上传文件失败") ||
			strings.HasPrefix(err.Error(), "保存文件失败") ||
			strings.HasPrefix(err.Error(), "data_path") {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("上传 APK 预览失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// DownloadApkPreview 根据 URL 下载 APK 并解析信息（不创建记录）
// @Summary 下载 remote APK 预览（解析包名和版本）
// @Tags apks
// @Accept json
// @Produce json
// @Param request body map[string]string true "包含 url 字段的对象"
// @Success 200 {object} services.ApkPreviewResult
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /apks/download-preview [post]
func (h *ApkHandler) DownloadApkPreview(c *gin.Context) {
	var req struct {
		URL string `json:"url" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "无效的请求参数: " + err.Error()})
		return
	}

	result, err := h.apkService.DownloadRemoteApkPreview(req.URL)
	if err != nil {
		if strings.HasPrefix(err.Error(), "url 必须是有效 URL") ||
			strings.HasPrefix(err.Error(), "url 仅支持 http/https") ||
			strings.HasPrefix(err.Error(), "下载失败") ||
			strings.HasPrefix(err.Error(), "下载的文件不是有效的 APK") ||
			strings.HasPrefix(err.Error(), "data_path") {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("下载 APK 预览失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

type CreateApkRequest struct {
	Name        string `json:"name" binding:"required"`
	PackageName string `json:"package_name"`            // 可选，优先自动解析
	Version     string `json:"version"`                 // 可选，优先自动解析
	Type        string `json:"type" binding:"required"` // remote / local
	URL         string `json:"url"`                     // remote 必填
	Description string `json:"description"`
}

type UpdateApkRequest struct {
	Name        *string `json:"name"`
	PackageName *string `json:"package_name"`
	Version     *string `json:"version"`
	Type        *string `json:"type"`
	URL         *string `json:"url"`
	Description *string `json:"description"`
}

// CreateApk 新建 APK
// @Summary 创建 APK
// @Description 创建一条 APK 记录（本地路径或远程 URL）
// @Tags apks
// @Accept json
// @Produce json
// @Param request body CreateApkRequest true "APK 数据"
// @Success 200 {object} models.Apk
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /apks/create [post]
func (h *ApkHandler) CreateApk(c *gin.Context) {
	var req CreateApkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "无效的请求参数: " + err.Error()})
		return
	}

	apk, err := h.apkService.CreateApk(req.Name, req.PackageName, req.Version, req.URL, req.Type, req.Description)
	if err != nil {
		// 约定：参数校验/冲突一律按 400 返回，其余按 500
		if err.Error() == "名称不能为空" ||
			err.Error() == "无法解析包名，请手动填写" ||
			err.Error() == "无法解析版本，请手动填写" ||
			err.Error() == "包名+版本已存在" ||
			err.Error() == "local 类型必须通过上传创建" ||
			err.Error() == "remote 类型必须提供 url" ||
			err.Error() == "url 必须是有效 URL" ||
			err.Error() == "url 仅支持 http/https" ||
			strings.HasPrefix(err.Error(), "type 不") {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("创建 APK 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, apk)
}

// GetApk 查询 APK 详情
// @Summary 查询 APK 详情
// @Tags apks
// @Produce json
// @Param id path string true "APK ID"
// @Success 200 {object} models.Apk
// @Failure 404 {object} ErrorResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /apks/detail/{id} [get]
func (h *ApkHandler) GetApk(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "ID 不能为空"})
		return
	}

	apk, err := h.apkService.GetApk(id)
	if err != nil {
		if err.Error() == "APK 不存在" {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("查询 APK 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, apk)
}

// ListApks APK 列表
// @Summary APK 列表
// @Tags apks
// @Produce json
// @Success 200 {array} models.Apk
// @Failure 500 {object} ErrorResponse
// @Router /apks/list [get]
func (h *ApkHandler) ListApks(c *gin.Context) {
	list, err := h.apkService.ListApks()
	if err != nil {
		log.Printf("查询 APK 列表失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(http.StatusOK, list)
}

// UpdateApk 更新 APK
// @Summary 更新 APK
// @Tags apks
// @Accept json
// @Produce json
// @Param id path string true "APK ID"
// @Param request body UpdateApkRequest true "待更新字段"
// @Success 200 {object} models.Apk
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /apks/update/{id} [post]
func (h *ApkHandler) UpdateApk(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "ID 不能为空"})
		return
	}

	var req UpdateApkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "无效的请求参数: " + err.Error()})
		return
	}

	apk, err := h.apkService.UpdateApk(id, req.Name, req.PackageName, req.Version, req.URL, req.Type, req.Description)
	if err != nil {
		if err.Error() == "APK 不存在" {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		if err.Error() == "名称不能为空" ||
			err.Error() == "无法解析包名，请手动填写" ||
			err.Error() == "无法解析版本，请手动填写" ||
			err.Error() == "包名+版本已存在" ||
			err.Error() == "local 类型必须通过上传创建" ||
			err.Error() == "local 类型不允许设置 url" ||
			err.Error() == "remote 类型必须提供 url" ||
			err.Error() == "url 必须是有效 URL" ||
			err.Error() == "url 仅支持 http/https" ||
			strings.HasPrefix(err.Error(), "type 不") {
			c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("更新 APK 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, apk)
}

// DeleteApk 删除 APK
// @Summary 删除 APK
// @Tags apks
// @Produce json
// @Param id path string true "APK ID"
// @Success 200 {object} SuccessResponse
// @Failure 404 {object} ErrorResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /apks/delete/{id} [post]
func (h *ApkHandler) DeleteApk(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "ID 不能为空"})
		return
	}

	if err := h.apkService.DeleteApk(id); err != nil {
		if err.Error() == "APK 不存在" {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: err.Error()})
			return
		}
		log.Printf("删除 APK 失败: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, SuccessResponse{Message: "APK 已删除"})
}

// ServeIcon 提供图标文件服务
// @Summary 获取 APK 图标
// @Tags apks
// @Produce image/png
// @Param filepath path string true "图标文件路径（相对于 apks 目录）"
// @Success 200 {file} image/png
// @Failure 404 {object} ErrorResponse
// @Router /apks/icon/{filepath} [get]
func (h *ApkHandler) ServeIcon(c *gin.Context) {
	filePathParam := c.Param("filepath")
	if filePathParam == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "文件路径不能为空"})
		return
	}

	// filePathParam 可能以 / 开头，需要去除
	if len(filePathParam) > 0 && filePathParam[0] == '/' {
		filePathParam = filePathParam[1:]
	}

	// 读取配置获取 data_path
	dataPath := configs.AppConfig.Server.DataPath
	if dataPath == "" {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "data_path 未配置"})
		return
	}

	// 构建完整路径（图标文件存储在 apks 目录下）
	iconPath := filepath.Join(dataPath, "apks", filePathParam)

	// 安全检查：确保路径在 data_path/apks 目录下，防止路径遍历攻击
	apksDir := filepath.Join(dataPath, "apks")
	absIconPath, err := filepath.Abs(iconPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "无效的文件路径"})
		return
	}
	absApksDir, err := filepath.Abs(apksDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "无法解析目录路径"})
		return
	}
	if !strings.HasPrefix(absIconPath, absApksDir) {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "不允许访问该路径"})
		return
	}

	// 检查文件是否存在
	if _, err := os.Stat(iconPath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "图标文件不存在"})
		return
	}

	c.File(iconPath)
}
