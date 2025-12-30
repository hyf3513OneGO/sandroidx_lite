package middlewares

import (
	"net/http"
	"strings"

	"sandroidx.com/sandroidx_lite/services"

	"github.com/gin-gonic/gin"
)

const (
	ContextUserIDKey   = "user_id"
	ContextUserRoleKey = "user_role"
)

// NewAuthMiddleware 校验 JWT 并将用户信息写入上下文。
func NewAuthMiddleware(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		// 兼容 WebSocket/无法自定义 Header 的场景：允许从 query token 获取
		if authHeader == "" {
			// 尝试从 Sec-WebSocket-Protocol 读取 Bearer token（浏览器 subprotocol 传递）
			if proto := c.GetHeader("Sec-WebSocket-Protocol"); proto != "" {
				parts := strings.Split(proto, ",")
				for _, p := range parts {
					p = strings.TrimSpace(p)
					if strings.HasPrefix(strings.ToLower(p), "bearer ") {
						authHeader = p
						break
					}
				}
			}
		}
		if authHeader == "" {
			if token := c.Query("token"); token != "" {
				authHeader = "Bearer " + token
			}
		}
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少 Authorization 头"})
			c.Abort()
			return
		}
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization 格式必须为 Bearer token"})
			c.Abort()
			return
		}

		claims, err := authService.ParseToken(parts[1])
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "令牌无效或已过期"})
			c.Abort()
			return
		}

		c.Set(ContextUserIDKey, claims.UserID)
		c.Set(ContextUserRoleKey, claims.Role)
		c.Next()
	}
}

// RequireRoles 限制只有指定角色可以访问。
func RequireRoles(roles ...string) gin.HandlerFunc {
	allowed := map[string]struct{}{}
	for _, r := range roles {
		allowed[r] = struct{}{}
	}
	return func(c *gin.Context) {
		roleValue, exists := c.Get(ContextUserRoleKey)
		if !exists {
			c.JSON(http.StatusForbidden, gin.H{"error": "缺少用户角色信息"})
			c.Abort()
			return
		}
		role, _ := roleValue.(string)
		if _, ok := allowed[role]; !ok {
			c.JSON(http.StatusForbidden, gin.H{"error": "无访问权限"})
			c.Abort()
			return
		}
		c.Next()
	}
}
