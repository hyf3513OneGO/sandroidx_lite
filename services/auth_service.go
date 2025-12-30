package services

import (
	"errors"
	"time"

	"sandroidx.com/sandroidx_lite/configs"
	"sandroidx.com/sandroidx_lite/models"

	"github.com/golang-jwt/jwt/v5"
)

type AuthService struct {
	userService     *UserService
	settingService  *SystemSettingService
	jwtSecret       []byte
	tokenExpiration time.Duration
}

type TokenClaims struct {
	UserID uint   `json:"user_id"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

func NewAuthService(userService *UserService, settingService *SystemSettingService) *AuthService {
	ttl := configs.AppConfig.Auth.TokenTTLHours
	if ttl <= 0 {
		ttl = 24
	}
	secret := configs.AppConfig.Auth.JWTSecret
	if secret == "" {
		secret = "please_change_me"
	}
	return &AuthService{
		userService:     userService,
		settingService:  settingService,
		jwtSecret:       []byte(secret),
		tokenExpiration: time.Duration(ttl) * time.Hour,
	}
}

func (s *AuthService) Login(email, password string) (string, *models.User, error) {
	if s.settingService != nil {
		settings, err := s.settingService.GetSettings()
		if err != nil {
			return "", nil, err
		}
		if !settings.AdminInitialized {
			return "", nil, errors.New("尚未设置管理员账户，请先执行初始化")
		}
	}

	user, err := s.userService.GetUserByEmail(email)
	if err != nil {
		return "", nil, err
	}

	if err := s.userService.ValidatePassword(user, password); err != nil {
		return "", nil, errors.New("邮箱或密码错误")
	}

	token, err := s.GenerateToken(user)
	if err != nil {
		return "", nil, err
	}

	now := time.Now()
	_ = s.userService.UpdateLastLogin(user.ID, now)

	return token, user, nil
}

func (s *AuthService) GenerateToken(user *models.User) (string, error) {
	claims := TokenClaims{
		UserID: user.ID,
		Role:   user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.tokenExpiration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}

func (s *AuthService) ParseToken(tokenStr string) (*TokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &TokenClaims{}, func(t *jwt.Token) (interface{}, error) {
		return s.jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*TokenClaims)
	if !ok || !token.Valid {
		return nil, errors.New("无效的令牌")
	}
	return claims, nil
}
