package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/xingshanghe/x-phone/internal/model"
	"gorm.io/gorm"
)

type Profiles struct {
	db  *gorm.DB
	dir string
}

type profileFile struct {
	ID           string `json:"id"`
	Brand        string `json:"brand"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Device       string `json:"device"`
	Product      string `json:"product"`
	Fingerprint  string `json:"fingerprint"`
	Release      string `json:"release"`
	SDK          string `json:"sdk"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	DPI          int    `json:"dpi"`
	Locale       string `json:"locale"`
	Timezone     string `json:"timezone"`
	GPU          string `json:"gpu"`
	CPUCores     int    `json:"cpuCores"`
	MemoryMB     int    `json:"memoryMb"`
	TouchPoints  int    `json:"touchPoints"`
}

func (p *Profiles) Sync(ctx context.Context) error {
	entries, err := os.ReadDir(p.dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(p.dir, entry.Name()))
		if err != nil {
			return err
		}
		var file profileFile
		if err := json.Unmarshal(raw, &file); err != nil {
			return err
		}
		if err := p.upsert(ctx, file.toModel()); err != nil {
			return err
		}
	}
	return nil
}

func (f profileFile) toModel() model.DeviceProfile {
	return model.DeviceProfile{
		Slug: f.ID, Brand: f.Brand, Manufacturer: f.Manufacturer, Model: f.Model,
		Device: f.Device, Product: f.Product, Fingerprint: f.Fingerprint, Release: f.Release,
		SDK: f.SDK, Width: f.Width, Height: f.Height, DPI: f.DPI, Locale: f.Locale,
		Timezone: f.Timezone, GPU: f.GPU, CPUCores: f.CPUCores, MemoryMB: f.MemoryMB,
		TouchPoints: f.TouchPoints,
	}
}

func (p *Profiles) upsert(ctx context.Context, incoming model.DeviceProfile) error {
	var existing model.DeviceProfile
	err := p.db.WithContext(ctx).Where("slug = ?", incoming.Slug).First(&existing).Error
	if err == gorm.ErrRecordNotFound {
		return p.db.WithContext(ctx).Create(&incoming).Error
	}
	if err != nil {
		return err
	}
	incoming.Base = existing.Base
	incoming.DeletedAt = existing.DeletedAt
	return p.db.WithContext(ctx).Save(&incoming).Error
}

func (p *Profiles) List(ctx context.Context) ([]model.DeviceProfile, error) {
	var items []model.DeviceProfile
	err := p.db.WithContext(ctx).Order("slug asc").Find(&items).Error
	return items, err
}

func (p *Profiles) BySlug(ctx context.Context, slug string) (model.DeviceProfile, error) {
	var profile model.DeviceProfile
	err := p.db.WithContext(ctx).Where("slug = ?", slug).First(&profile).Error
	if err != nil {
		return model.DeviceProfile{}, notFound(err)
	}
	return profile, nil
}
