package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"sandroidx.com/sandroidx_lite/clients"
	"sandroidx.com/sandroidx_lite/configs"
	"sandroidx.com/sandroidx_lite/handlers"
	"sandroidx.com/sandroidx_lite/middlewares"
	"sandroidx.com/sandroidx_lite/models"
	"sandroidx.com/sandroidx_lite/services"
	"sandroidx.com/sandroidx_lite/utils"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "sandroidx.com/sandroidx_lite/docs" // swagger docs
)

// @title           SAndroidX Lite API
// @version         1.0
// @description     这是一个 SAndroidX Lite 的 API 文档
// @termsOfService  http://swagger.io/terms/

// @contact.name   API Support
// @contact.url    http://www.swagger.io/support
// @contact.email  support@swagger.io

// @license.name  Apache 2.0
// @license.url   http://www.apache.org/licenses/LICENSE-2.0.html

// @host      localhost:8080
// @BasePath  /api/v1

// @schemes   http https
func main() {
	// 加载配置
	if err := configs.LoadConfig("configs/config.json"); err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 初始化数据库
	if err := models.InitDB(); err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}

	// 自动迁移数据库表
	if err := models.AutoMigrate(
		&models.User{},
		&models.Mapping{},
		&models.AdbCommandLog{},
		&models.Agent{},
		&models.AgentShare{},
		&models.Volume{},
		&models.AgentVolume{},
		&models.AdbGateway{},
		&models.Sandbox{},
		&models.SandboxVolume{},
		&models.SystemSetting{},
		&models.Template{},
		&models.Apk{},
	); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}

	// 初始化系统设置和用户/鉴权服务
	settingService := services.NewSystemSettingService()
	runtimeSettings, err := settingService.EnsureDefaults()
	if err != nil {
		log.Fatalf("初始化系统设置失败: %v", err)
	}

	userService := services.NewUserService(settingService)
	authService := services.NewAuthService(userService, settingService)

	hasAdmin, err := userService.HasAdminUser()
	if err != nil {
		log.Printf("检查管理员账户状态失败: %v", err)
	} else {
		if hasAdmin && runtimeSettings != nil && !runtimeSettings.AdminInitialized {
			_ = settingService.MarkAdminInitialized(true)
		}
		if !hasAdmin {
			log.Printf("尚未检测到管理员账户，请调用 /api/v1/auth/setup-admin 初始化密码")
		}
	}

	// 确保共享 APK 系统卷存在
	if configs.AppConfig.Server.DataPath != "" {
		if _, _, err := services.EnsureSharedApkVolume(configs.AppConfig.Server.DataPath); err != nil {
			log.Printf("警告: 初始化共享 APK 卷失败: %v", err)
		}
	}

	// 初始化 Docker 服务
	dockerService, err := services.NewDockerService()
	if err != nil {
		log.Fatalf("初始化 Docker 服务失败: %v", err)
	}

	// 确保 scrcpy-server 文件存在
	if configs.AppConfig.Server.DataPath != "" {
		if err := utils.EnsureScrcpyServer(configs.AppConfig.Server.DataPath); err != nil {
			log.Printf("警告: 下载 scrcpy-server 失败: %v", err)
		}
	}

	// 初始化 ADB Gateway 容器
	if configs.AppConfig.Server.DataPath != "" {
		adbGatewayInitService := services.NewAdbGatewayInitService(dockerService)
		if err := adbGatewayInitService.Initialize(context.Background()); err != nil {
			log.Printf("警告: ADB Gateway 容器初始化失败: %v", err)
		}
	}

	// 设置 Gin 模式
	gin.SetMode(configs.AppConfig.Server.Mode)

	// 创建 Gin 路由
	r := gin.Default()

	// 设置 multipart 表单大小限制（从配置读取，默认 1GB）
	maxMultipartMemory := int64(32 << 20) // 默认 32MB（Gin 默认值）
	if configs.AppConfig.Upload.MaxSizeBytes > 0 {
		maxMultipartMemory = configs.AppConfig.Upload.MaxSizeBytes
	}
	r.MaxMultipartMemory = maxMultipartMemory

	// 请求指标采集（用于总览页 QPS 等）
	reqMetrics := services.NewRequestMetricsCollector(10)
	r.Use(middlewares.NewRequestMetricsMiddleware(reqMetrics))

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":  "ok",
			"message": "服务运行正常",
		})
	})

	// Swagger 文档路由
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// 静态文件服务 - 测试页面
	r.Static("/html", "./html")

	// 初始化处理器与中间件
	authHandler := handlers.NewAuthHandler(userService, authService, settingService)
	userHandler := handlers.NewUserHandler(userService)
	systemSettingHandler := handlers.NewSystemSettingHandler(settingService)
	adbCommandLogHandler := handlers.NewAdbCommandLogHandler()
	authMiddleware := middlewares.NewAuthMiddleware(authService)
	adminOnly := middlewares.RequireRoles(models.RoleAdmin)
	overviewService := services.NewOverviewService(reqMetrics)
	overviewHandler := handlers.NewOverviewHandler(overviewService)

	// 初始化 ADB Gateway 客户端和服务
	var adbGatewayHandler *handlers.AdbGatewayHandler
	var adbGatewayService services.AdbGatewayService
	var agentHandler *handlers.AgentHandler
	var agentService services.AgentService
	var volumeHandler *handlers.VolumeHandler
	var sandboxService services.SandboxService
	var sandboxHandler *handlers.SandboxHandler
	var sandboxAdbHandler *handlers.SandboxAdbHandler
	var sandboxScrcpyHandler *handlers.SandboxScrcpyHandler
	var templateHandler *handlers.TemplateHandler
	var apkHandler *handlers.ApkHandler
	var agentShareHandler *handlers.AgentShareHandler

	// 初始化 Volume 服务和处理器
	volumeService := services.NewVolumeService()
	volumeHandler = handlers.NewVolumeHandler(volumeService)

	// 初始化 Template 服务和处理器
	templateService := services.NewTemplateService()
	templateHandler = handlers.NewTemplateHandler(templateService)

	// 初始化 Apk 服务和处理器
	apkService := services.NewApkService()
	apkHandler = handlers.NewApkHandler(apkService)

	gatewayConfig := configs.AppConfig.AdbGateway
	if gatewayConfig.GatewayHost != "" && gatewayConfig.GatewayAPIPort > 0 {
		baseURL := fmt.Sprintf("http://%s:%d", gatewayConfig.GatewayHost, gatewayConfig.GatewayAPIPort)
		client := clients.NewAdbGatewayClient(
			baseURL,
			gatewayConfig.GatewayToken,
		)
		// 创建 ADB Gateway 初始化服务
		var adbGatewayInitService *services.AdbGatewayInitService
		if configs.AppConfig.Server.DataPath != "" {
			adbGatewayInitService = services.NewAdbGatewayInitService(dockerService)
		}
		// 创建 AdbGatewayService，如果存在 initService 则传入
		if adbGatewayInitService != nil {
			adbGatewayService = services.NewAdbGatewayServiceWithInit(client, adbGatewayInitService)
		} else {
			adbGatewayService = services.NewAdbGatewayService(client)
		}
		adbGatewayHandler = handlers.NewAdbGatewayHandler(adbGatewayService)

		// 初始化 Agent 服务和处理器
		agentService = services.NewAgentService(dockerService, adbGatewayService)
		agentHandler = handlers.NewAgentHandler(agentService)

		// 如果配置了自动同步，则在服务启动时自动启动定期同步
		if gatewayConfig.AutoSyncEnabled {
			interval := gatewayConfig.SyncIntervalSec
			if interval <= 0 {
				interval = 300 // 默认 5 分钟
			}
			syncInterval := time.Duration(interval) * time.Second
			cancel, err := adbGatewayService.StartPeriodicSync(syncInterval)
			if err != nil {
				log.Printf("自动启动定期同步失败: %v", err)
			} else {
				log.Printf("已自动启动定期同步，间隔: %v", syncInterval)
				_ = cancel // 保存 cancel 函数，但当前无法在服务关闭时调用
			}
		}
	}

	// 初始化 Sandbox 服务和处理器（在 ADB Gateway 服务之后）
	sandboxService = services.NewSandboxService(dockerService, adbGatewayService, apkService)
	sandboxHandler = handlers.NewSandboxHandler(sandboxService)
	sandboxAdbHandler = handlers.NewSandboxAdbHandler(dockerService, sandboxService)

	// 初始化 Scrcpy 服务和处理器
	scrcpyService := services.NewScrcpyService(adbGatewayService, sandboxService)
	sandboxScrcpyHandler = handlers.NewSandboxScrcpyHandler(scrcpyService)

	// 初始化 Agent 分享服务和处理器（需要 AgentService + ScrcpyService）
	if agentService != nil {
		shareService := services.NewAgentShareService(models.DB)
		agentShareHandler = handlers.NewAgentShareHandler(shareService, agentService, scrcpyService, sandboxService)
	}

	// 用户路由
	api := r.Group("/api/v1")
	{
		auth := api.Group("/auth")
		{
			auth.POST("/login", authHandler.Login)
			auth.POST("/register", authHandler.Register)
			auth.POST("/setup-admin", authHandler.SetupAdmin)
			auth.GET("/status", authHandler.Status)
		}

		// 公开端点：APK 图标 API（浏览器直接加载图片无法携带认证头）
		if apkHandler != nil {
			api.GET("/apks/icon/*filepath", apkHandler.ServeIcon)
		}

		// 需鉴权 + 管理员权限的分组
		adminRequired := api.Group("")
		adminRequired.Use(authMiddleware, adminOnly)

		// 需鉴权的分组
		authRequired := api.Group("")
		authRequired.Use(authMiddleware)

		// 总览（需鉴权）
		if overviewHandler != nil {
			overviewHandler.RegisterRoutes(authRequired)
		}

		system := adminRequired.Group("/system")
		{
			system.GET("/settings", systemSettingHandler.GetSettings)
			system.PATCH("/settings", systemSettingHandler.UpdateSettings)
		}

		users := adminRequired.Group("/users")
		{
			users.POST("", userHandler.CreateUser)
			users.GET("", userHandler.GetAllUsers)
			users.GET("/:id", userHandler.GetUser)
			users.PUT("/:id", userHandler.UpdateUser)
			users.DELETE("/:id", userHandler.DeleteUser)
			users.PUT("/:id/role", userHandler.UpdateUserRole)
		}

		// ADB Gateway 相关接口
		adbGateway := api.Group("/adb-gateway")
		{
			// 上传接口（带鉴权）
			adbGateway.POST("/upload", adbCommandLogHandler.AuthMiddleware(), adbCommandLogHandler.UploadCommandLog)

			securedGateway := adbGateway.Group("")
			securedGateway.Use(authMiddleware, adminOnly)

			// 命令日志查询接口（需用户鉴权且仅管理员）
			securedGateway.GET("/command-logs", adbCommandLogHandler.GetCommandLogs)
			securedGateway.GET("/command-logs/mapping/:id", adbCommandLogHandler.GetCommandLogsByMappingID)
			securedGateway.POST("/command-logs/delete", adbCommandLogHandler.DeleteCommandLogs)
			securedGateway.POST("/command-logs/mapping/:id/clear", adbCommandLogHandler.ClearCommandLogsByMappingID)

			// ADB Gateway 映射管理接口
			if adbGatewayHandler != nil {
				// 映射管理（从 API）
				securedGateway.GET("/mappings", adbGatewayHandler.ListMappings)
				securedGateway.GET("/mappings/:id", adbGatewayHandler.GetMapping)
				securedGateway.POST("/mappings/create", adbGatewayHandler.CreateMapping)
				securedGateway.POST("/mappings/update", adbGatewayHandler.UpdateMapping)
				securedGateway.POST("/mappings/remove", adbGatewayHandler.RemoveMapping)

				// ADB 命令日志查询（从 API）
				securedGateway.GET("/logs/adb-commands", adbGatewayHandler.GetAdbCommandLogs)

				// 数据库操作
				securedGateway.GET("/mappings/db", adbGatewayHandler.ListMappingsFromDB)
				securedGateway.GET("/mappings/db/:id", adbGatewayHandler.GetMappingFromDB)

				// 同步操作
				securedGateway.POST("/sync", adbGatewayHandler.SyncMappings)
				securedGateway.POST("/sync/start", adbGatewayHandler.StartPeriodicSync)
				securedGateway.POST("/sync/stop", adbGatewayHandler.StopPeriodicSync)

				// 容器配置管理
				securedGateway.POST("/container/config", adbGatewayHandler.UpdateContainerConfig)
			}
		}

		// Agent 管理接口
		if agentHandler != nil {
			agentHandler.RegisterRoutes(authRequired)
		}

		// Agent 分享接口
		if agentShareHandler != nil {
			agentShareHandler.RegisterRoutes(authRequired, api)
		}

		// Volume 管理接口
		if volumeHandler != nil {
			volumeHandler.RegisterRoutes(authRequired)
		}

		// Template 管理接口
		if templateHandler != nil {
			templateHandler.RegisterRoutes(authRequired)
		}

		// Apk 管理接口
		if apkHandler != nil {
			apkHandler.RegisterRoutes(authRequired)
		}

		// Sandbox 管理接口
		if sandboxHandler != nil {
			sandboxHandler.RegisterRoutes(authRequired)
		}

		// Sandbox ADB Shell 接口
		if sandboxAdbHandler != nil {
			sandboxAdbHandler.RegisterRoutes(authRequired)
		}

		// Sandbox Scrcpy 屏幕镜像接口
		if sandboxScrcpyHandler != nil {
			sandboxScrcpyHandler.RegisterRoutes(authRequired)
		}
	}

	// 前端静态文件服务（在 API 路由之后注册，避免拦截 API 请求）
	// 提供前端构建后的静态文件
	r.Static("/assets", "./frontend/dist/assets")
	r.StaticFile("/favicon.ico", "./frontend/dist/favicon.ico")
	r.StaticFile("/vite.svg", "./frontend/dist/vite.svg")

	// SPA 路由回退：所有非 API 路由都返回 index.html（用于 Vue Router）
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		// 如果请求的是 API 路径，返回 404
		if len(path) >= 4 && path[:4] == "/api" {
			c.JSON(404, gin.H{
				"error": "API endpoint not found",
			})
			return
		}
		// 如果请求的是 swagger、html、health 路径，不处理
		if len(path) >= 8 && path[:8] == "/swagger" {
			c.JSON(404, gin.H{
				"error": "Not found",
			})
			return
		}
		if len(path) >= 5 && path[:5] == "/html" {
			c.JSON(404, gin.H{
				"error": "Not found",
			})
			return
		}
		if path == "/health" {
			c.JSON(404, gin.H{
				"error": "Not found",
			})
			return
		}
		// 如果请求的是静态资源路径，已经由上面的路由处理了，这里不应该到达
		// 否则返回前端 index.html（让前端路由处理）
		c.File("./frontend/dist/index.html")
	})

	// 启动服务器（使用自定义 HTTP 服务器以设置超时）
	addr := fmt.Sprintf("%s:%d", configs.AppConfig.Server.Host, configs.AppConfig.Server.Port)

	// 从配置读取超时时间（默认 30 分钟）
	timeoutSeconds := int64(1800)
	if configs.AppConfig.Upload.TimeoutSeconds > 0 {
		timeoutSeconds = configs.AppConfig.Upload.TimeoutSeconds
	}

	server := &http.Server{
		Addr:           addr,
		Handler:        r,
		ReadTimeout:    time.Duration(timeoutSeconds) * time.Second,
		WriteTimeout:   time.Duration(timeoutSeconds) * time.Second,
		MaxHeaderBytes: 1 << 20, // 1MB
	}

	log.Printf("服务器启动在 %s (上传超时: %d 秒, 最大文件大小: %d 字节)", addr, timeoutSeconds, maxMultipartMemory)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("服务器启动失败: %v", err)
	}
}
