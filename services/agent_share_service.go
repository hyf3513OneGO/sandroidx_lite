package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"sandroidx.com/sandroidx_lite/models"
)

var (
	ErrShareNotFound = errors.New("分享不存在或已失效")
)

// AgentShareService 管理 Agent 分享 token
type AgentShareService interface {
	Create(ctx context.Context, agentID string, ttl time.Duration) (*models.AgentShare, error)
	GetValid(ctx context.Context, token string) (*models.AgentShare, error)
}

type agentShareService struct {
	db *gorm.DB
}

func NewAgentShareService(db *gorm.DB) AgentShareService {
	return &agentShareService{db: db}
}

func (s *agentShareService) Create(ctx context.Context, agentID string, ttl time.Duration) (*models.AgentShare, error) {
	if agentID == "" {
		return nil, fmt.Errorf("agentID 不能为空")
	}

	// 生成 URL-safe token（高熵，避免可枚举）
	token, err := generateShareToken(32)
	if err != nil {
		return nil, err
	}

	var expiresAt *time.Time
	if ttl > 0 {
		t := time.Now().Add(ttl)
		expiresAt = &t
	}

	share := &models.AgentShare{
		Token:     token,
		AgentID:   agentID,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	}

	if err := s.db.WithContext(ctx).Create(share).Error; err != nil {
		return nil, err
	}
	return share, nil
}

func (s *agentShareService) GetValid(ctx context.Context, token string) (*models.AgentShare, error) {
	if token == "" {
		return nil, ErrShareNotFound
	}

	var share models.AgentShare
	if err := s.db.WithContext(ctx).First(&share, "token = ?", token).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrShareNotFound
		}
		return nil, err
	}

	if share.ExpiresAt != nil && time.Now().After(*share.ExpiresAt) {
		return nil, ErrShareNotFound
	}

	return &share, nil
}

func generateShareToken(nBytes int) (string, error) {
	if nBytes <= 0 {
		nBytes = 32
	}
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// RawURLEncoding: 不带 '='，更适合放在 URL path
	return base64.RawURLEncoding.EncodeToString(b), nil
}


