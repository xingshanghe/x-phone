package service

import (
	"context"
	"errors"
	"time"

	"github.com/xingshanghe/x-phone/internal/auth"
	"github.com/xingshanghe/x-phone/internal/model"
	"gorm.io/gorm"
)

type Auth struct {
	db            *gorm.DB
	signer        *auth.Signer
	adminUser     string
	adminPassword string
}

type Session struct {
	AccessToken string        `json:"accessToken"`
	Account     model.Account `json:"account"`
}

func (a *Auth) EnsureAdmin(ctx context.Context) error {
	var count int64
	if err := a.db.WithContext(ctx).Model(&model.Account{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if a.adminPassword == "" {
		return errors.New("XPHONE_ADMIN_PASSWORD is required to seed the first account")
	}
	hash, err := auth.HashPassword(a.adminPassword)
	if err != nil {
		return err
	}
	account := model.Account{Username: a.adminUser, PasswordHash: hash}
	return a.db.WithContext(ctx).Create(&account).Error
}

func (a *Auth) Login(ctx context.Context, username, password string, now time.Time) (Session, error) {
	var account model.Account
	err := a.db.WithContext(ctx).Where("username = ?", username).First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Session{}, ErrUnauthorized
	}
	if err != nil {
		return Session{}, err
	}
	if auth.CheckPassword(account.PasswordHash, password) != nil {
		return Session{}, ErrUnauthorized
	}
	token, err := a.signer.Issue(account.ID, now)
	if err != nil {
		return Session{}, err
	}
	_ = a.db.WithContext(ctx).Create(&model.AuditEvent{AccountID: account.ID, Action: "login", Target: account.ID}).Error
	return Session{AccessToken: token, Account: account}, nil
}
