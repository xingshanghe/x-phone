package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/xingshanghe/x-phone/internal/blob"
	"github.com/xingshanghe/x-phone/internal/model"
	"gorm.io/gorm"
)

type Instances struct {
	db    *gorm.DB
	blobs blob.Store
}

type CreateInstanceInput struct {
	Name      string
	HostID    string
	ProfileID string
	HTTPProxy string
	ADBSerial string
}

func (s *Instances) Create(ctx context.Context, accountID string, in CreateInstanceInput) (model.Instance, error) {
	if err := validateName(in.Name); err != nil {
		return model.Instance{}, err
	}
	proxy, err := cleanProxy(in.HTTPProxy)
	if err != nil {
		return model.Instance{}, err
	}
	var host model.Host
	if err := s.db.WithContext(ctx).First(&host, "id = ?", in.HostID).Error; err != nil {
		return model.Instance{}, notFound(err)
	}
	if host.Status != "online" {
		return model.Instance{}, fmt.Errorf("%w: host is offline", ErrConflict)
	}
	serial, adbPort, err := CleanSerial(in.ADBSerial)
	if err != nil {
		return model.Instance{}, err
	}
	profile, err := loadOptionalProfile(ctx, s.db, in.ProfileID)
	if err != nil {
		return model.Instance{}, err
	}
	if serial == "" && profile.Slug == "" {
		return model.Instance{}, fmt.Errorf("%w: profile is required", ErrValidation)
	}
	active, err := s.countActive(ctx, host.ID)
	if err != nil {
		return model.Instance{}, err
	}
	if serial == "" && host.Binderfs != "yes" && active >= 1 {
		return model.Instance{}, ErrBinderfs
	}
	if active >= host.Capacity {
		return model.Instance{}, fmt.Errorf("%w: host capacity reached", ErrConflict)
	}
	runtimeKind := "android-redroid"
	taskType := "create"
	if serial != "" {
		runtimeKind = "android-adb"
		taskType = "attach"
	}
	instance := model.Instance{
		Name: in.Name, HostID: host.ID, ProfileID: profile.Slug, Status: "creating",
		HTTPProxy: proxy, RuntimeKind: runtimeKind, ADBSerial: serial, ADBPort: adbPort,
	}
	instance.CreatedBy = accountID
	instance.UpdatedBy = accountID
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&instance).Error; err != nil {
			return err
		}
		payload, err := taskPayload(instance, profile, "")
		if err != nil {
			return err
		}
		task := model.InstanceTask{
			InstanceID: instance.ID, HostID: host.ID, Type: taskType, Payload: payload, Status: "pending",
		}
		task.CreatedBy = accountID
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditEvent{AccountID: accountID, Action: "instance.create", Target: instance.ID, Detail: in.Name}).Error
	})
	return instance, err
}

func (s *Instances) countActive(ctx context.Context, hostID string) (int, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&model.Instance{}).
		Where("host_id = ? AND status IN ?", hostID, []string{"creating", "booting", "running", "stopping", "deleting"}).
		Count(&count).Error
	return int(count), err
}

func (s *Instances) List(ctx context.Context, page, size int) ([]model.Instance, int64, error) {
	var total int64
	query := s.db.WithContext(ctx).Model(&model.Instance{})
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.Instance
	err := query.Order("created_at desc").Offset((page - 1) * size).Limit(size).Find(&items).Error
	return items, total, err
}

func (s *Instances) Get(ctx context.Context, id string) (model.Instance, error) {
	var instance model.Instance
	err := s.db.WithContext(ctx).First(&instance, "id = ?", id).Error
	if err != nil {
		return model.Instance{}, notFound(err)
	}
	return instance, nil
}

func (s *Instances) Transition(ctx context.Context, accountID, id, action string) (model.Instance, error) {
	instance, err := s.Get(ctx, id)
	if err != nil {
		return model.Instance{}, err
	}
	next, taskType, err := transitionPlan(action, instance.Status)
	if err != nil {
		return model.Instance{}, err
	}
	profile, err := loadOptionalProfile(ctx, s.db, instance.ProfileID)
	if err != nil {
		return model.Instance{}, err
	}
	payload, err := taskPayload(instance, profile, "")
	if err != nil {
		return model.Instance{}, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&instance).Update("status", next).Error; err != nil {
			return err
		}
		task := model.InstanceTask{InstanceID: instance.ID, HostID: instance.HostID, Type: taskType, Payload: payload, Status: "pending"}
		task.CreatedBy = accountID
		return tx.Create(&task).Error
	})
	if err != nil {
		return model.Instance{}, err
	}
	instance.Status = next
	return instance, nil
}

func (s *Instances) Delete(ctx context.Context, accountID, id string) (model.Instance, error) {
	instance, err := s.Get(ctx, id)
	if err != nil {
		return model.Instance{}, err
	}
	profile, err := loadOptionalProfile(ctx, s.db, instance.ProfileID)
	if err != nil {
		return model.Instance{}, err
	}
	payload, err := taskPayload(instance, profile, "")
	if err != nil {
		return model.Instance{}, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&instance).Update("status", "deleting").Error; err != nil {
			return err
		}
		if err := tx.Where("instance_id = ?", instance.ID).Delete(&model.ControlGroupMember{}).Error; err != nil {
			return err
		}
		task := model.InstanceTask{InstanceID: instance.ID, HostID: instance.HostID, Type: "delete", Payload: payload, Status: "pending"}
		task.CreatedBy = accountID
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditEvent{AccountID: accountID, Action: "instance.delete", Target: instance.ID}).Error
	})
	if err != nil {
		return model.Instance{}, err
	}
	instance.Status = "deleting"
	return instance, nil
}

func (s *Instances) RejectScript(ctx context.Context, instanceID string) error {
	if _, err := s.Get(ctx, instanceID); err != nil {
		return err
	}
	return ErrNotImplemented
}

func (s *Instances) SaveAPK(ctx context.Context, accountID, instanceID, packageName, filename string, r io.Reader, size int64) (model.InstanceTask, error) {
	instance, err := s.Get(ctx, instanceID)
	if err != nil {
		return model.InstanceTask{}, err
	}
	if instance.Status != "running" {
		return model.InstanceTask{}, fmt.Errorf("%w: instance is not running", ErrConflict)
	}
	if packageName == "" || strings.Contains(packageName, " ") {
		return model.InstanceTask{}, fmt.Errorf("%w: package name is required", ErrValidation)
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".apk") {
		return model.InstanceTask{}, fmt.Errorf("%w: file must be an apk", ErrValidation)
	}
	task := model.InstanceTask{InstanceID: instance.ID, HostID: instance.HostID, Type: "install", Status: "pending"}
	task.CreatedBy = accountID
	if err := s.db.WithContext(ctx).Create(&task).Error; err != nil {
		return model.InstanceTask{}, err
	}
	key := path.Join("apks", task.ID+".apk")
	if err := s.blobs.Put(ctx, key, r, size); err != nil {
		_ = s.db.WithContext(ctx).Model(&task).Updates(map[string]any{"status": "failed", "log": err.Error()}).Error
		return model.InstanceTask{}, err
	}
	payload, err := json.Marshal(map[string]string{"apkKey": key, "packageName": packageName, "fileName": filename})
	if err != nil {
		return model.InstanceTask{}, err
	}
	task.Payload = string(payload)
	if err := s.db.WithContext(ctx).Model(&task).Update("payload", task.Payload).Error; err != nil {
		return model.InstanceTask{}, err
	}
	return task, nil
}

func (s *Instances) Tasks(ctx context.Context, instanceID string) ([]model.InstanceTask, error) {
	if _, err := s.Get(ctx, instanceID); err != nil {
		return nil, err
	}
	var items []model.InstanceTask
	err := s.db.WithContext(ctx).Where("instance_id = ?", instanceID).Order("created_at desc").Find(&items).Error
	return items, err
}

func (s *Instances) profile(ctx context.Context, slug string) (model.DeviceProfile, error) {
	return profileBySlug(ctx, s.db, slug)
}

func loadOptionalProfile(ctx context.Context, db *gorm.DB, slug string) (model.DeviceProfile, error) {
	if slug == "" {
		return model.DeviceProfile{}, nil
	}
	return profileBySlug(ctx, db, slug)
}

func profileBySlug(ctx context.Context, db *gorm.DB, slug string) (model.DeviceProfile, error) {
	var profile model.DeviceProfile
	err := db.WithContext(ctx).Where("slug = ?", slug).First(&profile).Error
	if err != nil {
		return model.DeviceProfile{}, notFound(err)
	}
	return profile, nil
}

func transitionPlan(action, status string) (string, string, error) {
	switch action {
	case "start":
		if status != "stopped" && status != "error" {
			return "", "", fmt.Errorf("%w: instance cannot start from %s", ErrConflict, status)
		}
		return "creating", "start", nil
	case "stop":
		if status != "running" && status != "booting" {
			return "", "", fmt.Errorf("%w: instance cannot stop from %s", ErrConflict, status)
		}
		return "stopping", "stop", nil
	default:
		return "", "", fmt.Errorf("%w: unknown action", ErrValidation)
	}
}

func taskPayload(instance model.Instance, profile model.DeviceProfile, packageName string) (string, error) {
	body := map[string]any{
		"instanceId":    instance.ID,
		"containerName": "xphone-" + instance.ID,
		"adbPort":       instance.ADBPort,
		"serial":        instance.ADBSerial,
		"runtimeKind":   instance.RuntimeKind,
		"profileId":     profile.Slug,
		"proxy":         instance.HTTPProxy,
		"packageName":   packageName,
		"profile": map[string]any{
			"id": profile.Slug, "brand": profile.Brand, "manufacturer": profile.Manufacturer,
			"model": profile.Model, "device": profile.Device, "product": profile.Product,
			"fingerprint": profile.Fingerprint, "release": profile.Release, "sdk": profile.SDK,
			"width": profile.Width, "height": profile.Height, "dpi": profile.DPI,
			"locale": profile.Locale, "timezone": profile.Timezone, "gpu": profile.GPU,
			"cpuCores": profile.CPUCores, "memoryMb": profile.MemoryMB, "touchPoints": profile.TouchPoints,
		},
	}
	raw, err := json.Marshal(body)
	return string(raw), err
}

func cleanProxy(proxy string) (string, error) {
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		return "", nil
	}
	if strings.ContainsAny(proxy, " \r\n") || len(proxy) > 256 {
		return "", fmt.Errorf("%w: proxy must be host:port without spaces", ErrValidation)
	}
	return proxy, nil
}
