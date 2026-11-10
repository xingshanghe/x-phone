package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/xingshanghe/x-phone/internal/model"
	"gorm.io/gorm"
)

type Agent struct {
	db        *gorm.DB
	instances *Instances
	hosts     *Hosts
}

type CompleteInput struct {
	OK          bool
	Error       string
	Log         string
	ADBPort     int
	ContainerID string
	Serial      string
	Width       int
	Height      int
	Report      json.RawMessage
}

func (a *Agent) Pending(ctx context.Context, hostID string) ([]model.InstanceTask, error) {
	var items []model.InstanceTask
	err := a.db.WithContext(ctx).Where("host_id = ? AND status = ?", hostID, "pending").Order("created_at asc").Find(&items).Error
	return items, err
}

func (a *Agent) Claim(ctx context.Context, hostID, taskID string, now time.Time) (model.InstanceTask, error) {
	var task model.InstanceTask
	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND host_id = ? AND status = ?", taskID, hostID, "pending").First(&task).Error; err != nil {
			return err
		}
		task.Status = "running"
		task.ClaimedAt = now.Unix()
		if err := tx.Model(&task).Updates(map[string]any{"status": "running", "claimed_at": task.ClaimedAt}).Error; err != nil {
			return err
		}
		next := ""
		switch task.Type {
		case "create", "start", "attach":
			next = "booting"
		case "stop":
			next = "stopping"
		case "delete":
			next = "deleting"
		}
		if next != "" {
			return tx.Model(&model.Instance{}).Where("id = ?", task.InstanceID).Update("status", next).Error
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.InstanceTask{}, ErrConflict
	}
	return task, err
}

func (a *Agent) Complete(ctx context.Context, hostID, taskID string, in CompleteInput, now time.Time) (model.InstanceTask, error) {
	var task model.InstanceTask
	err := a.db.WithContext(ctx).Where("id = ? AND host_id = ?", taskID, hostID).First(&task).Error
	if err != nil {
		return model.InstanceTask{}, notFound(err)
	}
	if task.Status != "running" {
		return model.InstanceTask{}, ErrConflict
	}
	status := "succeeded"
	if !in.OK {
		status = "failed"
	}
	logText := in.Log
	if in.Error != "" {
		logText = in.Error + "\n" + logText
	}
	if a.instances != nil && a.instances.blobs != nil && logText != "" {
		_ = a.instances.blobs.Put(ctx, "logs/"+task.ID+".log", stringsReader(logText), int64(len(logText)))
	}
	err = a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&task).Updates(map[string]any{
			"status": status, "finished_at": now.Unix(), "log": logText,
		}).Error; err != nil {
			return err
		}
		return applyInstance(tx, task, in)
	})
	if err != nil {
		return model.InstanceTask{}, err
	}
	task.Status = status
	task.Log = logText
	task.FinishedAt = now.Unix()
	return task, nil
}

func applyInstance(tx *gorm.DB, task model.InstanceTask, in CompleteInput) error {
	updates := map[string]any{}
	switch task.Type {
	case "create", "start", "attach":
		if in.OK {
			updates["status"] = "running"
			updates["adb_port"] = in.ADBPort
			updates["container_id"] = in.ContainerID
			updates["last_error"] = ""
			if in.Serial != "" {
				updates["adb_serial"] = in.Serial
			}
			if in.Width > 0 && in.Height > 0 {
				updates["display_width"] = in.Width
				updates["display_height"] = in.Height
			}
			if len(in.Report) > 0 {
				updates["profile_report"] = string(in.Report)
			}
		} else {
			updates["status"] = "error"
			updates["last_error"] = in.Error
		}
	case "stop":
		if in.OK {
			updates["status"] = "stopped"
			updates["last_error"] = ""
		} else {
			updates["status"] = "error"
			updates["last_error"] = in.Error
		}
	case "delete":
		if in.OK {
			return tx.Delete(&model.Instance{}, "id = ?", task.InstanceID).Error
		}
		updates["status"] = "error"
		updates["last_error"] = in.Error
	case "install":
		if !in.OK {
			updates["last_error"] = in.Error
		} else {
			updates["last_error"] = ""
		}
	}
	if len(updates) == 0 {
		return nil
	}
	return tx.Model(&model.Instance{}).Where("id = ?", task.InstanceID).Updates(updates).Error
}

type RunningInstance struct {
	model.Instance
	Width  int `json:"width"`
	Height int `json:"height"`
}

func (a *Agent) Running(ctx context.Context, hostID string) ([]RunningInstance, error) {
	var items []model.Instance
	if err := a.db.WithContext(ctx).Where("host_id = ? AND status = ?", hostID, "running").Find(&items).Error; err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	slugs := make([]string, 0, len(items))
	for _, item := range items {
		if _, ok := seen[item.ProfileID]; ok || item.ProfileID == "" {
			continue
		}
		seen[item.ProfileID] = struct{}{}
		slugs = append(slugs, item.ProfileID)
	}
	profiles := map[string]model.DeviceProfile{}
	if len(slugs) > 0 {
		var rows []model.DeviceProfile
		if err := a.db.WithContext(ctx).Where("slug IN ?", slugs).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			profiles[row.Slug] = row
		}
	}
	out := make([]RunningInstance, 0, len(items))
	for _, item := range items {
		profile := profiles[item.ProfileID]
		width, height := profile.Width, profile.Height
		if item.DisplayWidth > 0 && item.DisplayHeight > 0 {
			width, height = item.DisplayWidth, item.DisplayHeight
		}
		out = append(out, RunningInstance{Instance: item, Width: width, Height: height})
	}
	return out, nil
}

func (a *Agent) OpenAPK(ctx context.Context, hostID, taskID string) (io.ReadCloser, error) {
	var task model.InstanceTask
	err := a.db.WithContext(ctx).Where("id = ? AND host_id = ? AND type = ?", taskID, hostID, "install").First(&task).Error
	if err != nil {
		return nil, notFound(err)
	}
	var payload struct {
		APKKey string `json:"apkKey"`
	}
	if err := json.Unmarshal([]byte(task.Payload), &payload); err != nil || payload.APKKey == "" {
		return nil, ErrNotFound
	}
	return a.instances.blobs.Open(ctx, payload.APKKey)
}

func (a *Agent) RequeueStale(ctx context.Context, now time.Time) error {
	cutoff := now.Add(-10 * time.Minute).Unix()
	return a.db.WithContext(ctx).Model(&model.InstanceTask{}).
		Where("status = ? AND claimed_at > 0 AND claimed_at < ?", "running", cutoff).
		Updates(map[string]any{"status": "pending", "claimed_at": 0}).Error
}

type stringReader struct {
	s string
	i int
}

func stringsReader(s string) io.Reader { return &stringReader{s: s} }

func (r *stringReader) Read(p []byte) (int, error) {
	if r.i >= len(r.s) {
		return 0, io.EOF
	}
	n := copy(p, r.s[r.i:])
	r.i += n
	return n, nil
}
