package services

import (
	"errors"
	"strings"
	"time"

	"sandroidx.com/sandroidx_lite/models"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type UserService struct {
	settings *SystemSettingService
}

func NewUserService(settings *SystemSettingService) *UserService {
	return &UserService{settings: settings}
}

func (s *UserService) CreateUser(name, email, password, role string) (*models.User, error) {
	if password == "" {
		return nil, errors.New("密码不能为空")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	user := &models.User{
		Name:         name,
		Email:        strings.ToLower(email),
		PasswordHash: string(hash),
		Role:         normalizeRole(role),
	}

	if err := models.DB.Create(user).Error; err != nil {
		return nil, err
	}

	return user, nil
}

// RegisterUser 用于普通用户注册，默认 guest 角色。
func (s *UserService) RegisterUser(name, email, password string) (*models.User, error) {
	if s.settings != nil {
		settings, err := s.settings.GetSettings()
		if err != nil {
			return nil, err
		}
		if !settings.AllowRegistration {
			return nil, errors.New("当前不允许新用户注册")
		}
	}
	return s.CreateUser(name, email, password, models.RoleGuest)
}

// InitAdminUser 首次启动设置管理员密码，已有管理员则报错。
func (s *UserService) InitAdminUser(name, email, password string) (*models.User, error) {
	hasAdmin, err := s.HasAdminUser()
	if err != nil {
		return nil, err
	}
	if hasAdmin {
		return nil, errors.New("管理员账户已存在，无法再次初始化")
	}

	user, err := s.CreateUser(name, email, password, models.RoleAdmin)
	if err != nil {
		return nil, err
	}

	if s.settings != nil {
		_ = s.settings.MarkAdminInitialized(true)
	}

	return user, nil
}

func (s *UserService) GetUserByID(id uint) (*models.User, error) {
	var user models.User
	if err := models.DB.First(&user, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("用户不存在")
		}
		return nil, err
	}
	return &user, nil
}

func (s *UserService) GetUserByEmail(email string) (*models.User, error) {
	var user models.User
	if err := models.DB.Where("email = ?", strings.ToLower(email)).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("用户不存在")
		}
		return nil, err
	}
	return &user, nil
}

func (s *UserService) GetAllUsers() ([]models.User, error) {
	var users []models.User
	if err := models.DB.Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

func (s *UserService) UpdateUser(id uint, name, email, password, role string) (*models.User, error) {
	user, err := s.GetUserByID(id)
	if err != nil {
		return nil, err
	}

	if name != "" {
		user.Name = name
	}
	if email != "" {
		user.Email = strings.ToLower(email)
	}
	if password != "" {
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if hashErr != nil {
			return nil, hashErr
		}
		user.PasswordHash = string(hash)
	}
	if role != "" {
		user.Role = normalizeRole(role)
	}

	if err := models.DB.Save(user).Error; err != nil {
		return nil, err
	}

	return user, nil
}

func (s *UserService) DeleteUser(id uint) error {
	if err := models.DB.Delete(&models.User{}, id).Error; err != nil {
		return err
	}
	return nil
}

// HasAdminUser 判断是否已有管理员。
func (s *UserService) HasAdminUser() (bool, error) {
	var count int64
	if err := models.DB.Model(&models.User{}).Where("role = ?", models.RoleAdmin).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// UpdateLastLogin 更新最近登录时间。
func (s *UserService) UpdateLastLogin(userID uint, t time.Time) error {
	return models.DB.Model(&models.User{}).Where("id = ?", userID).Update("last_login", t).Error
}

// ValidatePassword 校验密码是否匹配。
func (s *UserService) ValidatePassword(user *models.User, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
}

func normalizeRole(role string) string {
	switch strings.ToLower(role) {
	case models.RoleAdmin:
		return models.RoleAdmin
	case models.RoleUser:
		return models.RoleUser
	default:
		return models.RoleGuest
	}
}
