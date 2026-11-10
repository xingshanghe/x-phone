package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/xingshanghe/x-phone/internal/auth"
	"github.com/xingshanghe/x-phone/internal/blob"
	"github.com/xingshanghe/x-phone/internal/config"
	"gorm.io/gorm"
)

type Services struct {
	DB        *gorm.DB
	Auth      *Auth
	Hosts     *Hosts
	Profiles  *Profiles
	Instances *Instances
	Groups    *Groups
	Agent     *Agent
	stop      chan struct{}
}

func New(db *gorm.DB, cfg config.Config, store blob.Store, signer *auth.Signer) (*Services, error) {
	var rdb *redis.Client
	if cfg.RedisAddr != "" {
		rdb = redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB})
	}
	svc := &Services{
		DB:        db,
		stop:      make(chan struct{}),
		Auth:      &Auth{db: db, signer: signer, adminUser: cfg.AdminUser, adminPassword: cfg.AdminPassword},
		Hosts:     &Hosts{db: db, redis: rdb},
		Profiles:  &Profiles{db: db, dir: cfg.ProfilesDir},
		Instances: &Instances{db: db, blobs: store},
		Groups:    &Groups{db: db},
	}
	svc.Agent = &Agent{db: db, instances: svc.Instances, hosts: svc.Hosts}
	return svc, nil
}

func (s *Services) RunSweep() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			_ = s.Hosts.MarkStale(context.Background(), time.Now())
			_ = s.Agent.RequeueStale(context.Background(), time.Now())
		}
	}
}

func (s *Services) Stop() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
}

func validateName(name string) error {
	if name == "" || len(name) > 64 {
		return fmt.Errorf("%w: name must be 1-64 characters", ErrValidation)
	}
	return nil
}

func notFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}
