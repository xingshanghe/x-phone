package service

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/xingshanghe/x-phone/internal/auth"
	"github.com/xingshanghe/x-phone/internal/id"
	"github.com/xingshanghe/x-phone/internal/model"
	"gorm.io/gorm"
)

type Hosts struct {
	db    *gorm.DB
	redis *redis.Client
}

type RegisteredHost struct {
	Host       model.Host `json:"host"`
	AgentToken string     `json:"agentToken"`
}

type HeartbeatInput struct {
	Binderfs     bool
	Capacity     int
	RunningCount int
}

func (h *Hosts) Register(ctx context.Context, accountID, name, addr string, capacity int) (RegisteredHost, error) {
	if err := validateName(name); err != nil {
		return RegisteredHost{}, err
	}
	if addr == "" {
		return RegisteredHost{}, fmt.Errorf("%w: addr is required", ErrValidation)
	}
	if capacity <= 0 {
		capacity = 4
	}
	token := id.Token()
	host := model.Host{
		Name:      name,
		Addr:      addr,
		Status:    "pending",
		Capacity:  capacity,
		Binderfs:  "unknown",
		TokenHash: auth.HashToken(token),
	}
	host.CreatedBy = accountID
	host.UpdatedBy = accountID
	if err := h.db.WithContext(ctx).Create(&host).Error; err != nil {
		return RegisteredHost{}, err
	}
	return RegisteredHost{Host: host, AgentToken: token}, nil
}

func (h *Hosts) List(ctx context.Context, page, size int) ([]model.Host, int64, error) {
	var total int64
	query := h.db.WithContext(ctx).Model(&model.Host{})
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.Host
	err := query.Order("created_at desc").Offset((page - 1) * size).Limit(size).Find(&items).Error
	return items, total, err
}

func (h *Hosts) ByToken(ctx context.Context, token string) (model.Host, error) {
	var host model.Host
	err := h.db.WithContext(ctx).Where("token_hash = ?", auth.HashToken(token)).First(&host).Error
	if err != nil {
		return model.Host{}, notFound(err)
	}
	return host, nil
}

func (h *Hosts) Heartbeat(ctx context.Context, hostID string, in HeartbeatInput, now time.Time) (model.Host, error) {
	var host model.Host
	err := h.db.WithContext(ctx).First(&host, "id = ?", hostID).Error
	if err != nil {
		return model.Host{}, notFound(err)
	}
	binder := "no"
	if in.Binderfs {
		binder = "yes"
	}
	if in.Capacity > 0 {
		host.Capacity = in.Capacity
	}
	host.Binderfs = binder
	host.RunningCount = in.RunningCount
	host.Status = "online"
	host.LastHeartbeatAt = now.Unix()
	if err := h.db.WithContext(ctx).Save(&host).Error; err != nil {
		return model.Host{}, err
	}
	if h.redis != nil {
		_ = h.redis.Set(ctx, "host:"+host.ID, "1", 45*time.Second).Err()
	}
	return host, nil
}

func (h *Hosts) MarkStale(ctx context.Context, now time.Time) error {
	cutoff := now.Add(-45 * time.Second).Unix()
	return h.db.WithContext(ctx).Model(&model.Host{}).
		Where("status = ? AND last_heartbeat_at > 0 AND last_heartbeat_at < ?", "online", cutoff).
		Update("status", "offline").Error
}
