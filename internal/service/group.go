package service

import (
	"context"
	"fmt"

	"github.com/xingshanghe/x-phone/internal/control"
	"github.com/xingshanghe/x-phone/internal/id"
	"github.com/xingshanghe/x-phone/internal/model"
	"gorm.io/gorm"
)

type Groups struct {
	db *gorm.DB
}

type Group struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	ProfileID   string   `json:"profileId"`
	InstanceIDs []string `json:"instanceIds"`
}

type BatchResult struct {
	ID      string   `json:"id"`
	GroupID string   `json:"groupId"`
	Type    string   `json:"type"`
	Status  string   `json:"status"`
	TaskIDs []string `json:"taskIds"`
}

func (g *Groups) Create(ctx context.Context, accountID, name string, instanceIDs []string) (Group, error) {
	if err := validateName(name); err != nil {
		return Group{}, err
	}
	ids, err := uniqueIDs(instanceIDs)
	if err != nil {
		return Group{}, err
	}
	var instances []model.Instance
	if err := g.db.WithContext(ctx).Where("id IN ?", ids).Find(&instances).Error; err != nil {
		return Group{}, err
	}
	if len(instances) != len(ids) {
		return Group{}, ErrNotFound
	}
	profileID := instances[0].ProfileID
	for _, instance := range instances[1:] {
		if instance.ProfileID != profileID {
			return Group{}, fmt.Errorf("%w: group requires one profile", ErrValidation)
		}
	}
	var taken int64
	if err := g.db.WithContext(ctx).Model(&model.ControlGroupMember{}).Where("instance_id IN ?", ids).Count(&taken).Error; err != nil {
		return Group{}, err
	}
	if taken > 0 {
		return Group{}, fmt.Errorf("%w: instance already belongs to a group", ErrConflict)
	}
	group := model.ControlGroup{Name: name, ProfileID: profileID}
	group.CreatedBy = accountID
	group.UpdatedBy = accountID
	err = g.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&group).Error; err != nil {
			return err
		}
		for position, instanceID := range ids {
			member := model.ControlGroupMember{ID: id.New(), GroupID: group.ID, InstanceID: instanceID, Position: position}
			if err := tx.Create(&member).Error; err != nil {
				return err
			}
		}
		return tx.Create(&model.AuditEvent{AccountID: accountID, Action: "group.create", Target: group.ID, Detail: name}).Error
	})
	if err != nil {
		return Group{}, err
	}
	return Group{ID: group.ID, Name: group.Name, ProfileID: group.ProfileID, InstanceIDs: ids}, nil
}

func (g *Groups) List(ctx context.Context) ([]Group, error) {
	var rows []model.ControlGroup
	if err := g.db.WithContext(ctx).Order("created_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Group, 0, len(rows))
	for _, row := range rows {
		members, err := g.members(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, Group{ID: row.ID, Name: row.Name, ProfileID: row.ProfileID, InstanceIDs: members})
	}
	return out, nil
}

func (g *Groups) Get(ctx context.Context, groupID string) (Group, error) {
	var row model.ControlGroup
	if err := g.db.WithContext(ctx).First(&row, "id = ?", groupID).Error; err != nil {
		return Group{}, notFound(err)
	}
	members, err := g.members(ctx, row.ID)
	if err != nil {
		return Group{}, err
	}
	return Group{ID: row.ID, Name: row.Name, ProfileID: row.ProfileID, InstanceIDs: members}, nil
}

func (g *Groups) Delete(ctx context.Context, accountID, groupID string) error {
	if _, err := g.Get(ctx, groupID); err != nil {
		return err
	}
	return g.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", groupID).Delete(&model.ControlGroupMember{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&model.ControlGroup{}, "id = ?", groupID).Error; err != nil {
			return err
		}
		return tx.Create(&model.AuditEvent{AccountID: accountID, Action: "group.delete", Target: groupID}).Error
	})
}

func (g *Groups) Targets(ctx context.Context, groupID string) ([]control.Target, error) {
	group, err := g.Get(ctx, groupID)
	if err != nil {
		return nil, err
	}
	return g.targets(ctx, group.InstanceIDs)
}

func (g *Groups) Target(ctx context.Context, instanceID string) (control.Target, error) {
	targets, err := g.targets(ctx, []string{instanceID})
	if err != nil {
		return control.Target{}, err
	}
	if len(targets) != 1 {
		return control.Target{}, ErrNotFound
	}
	return targets[0], nil
}

func (g *Groups) Expand(ctx context.Context, accountID, groupID, action string) (BatchResult, error) {
	group, err := g.Get(ctx, groupID)
	if err != nil {
		return BatchResult{}, err
	}
	if len(group.InstanceIDs) == 0 {
		return BatchResult{}, fmt.Errorf("%w: group is empty", ErrValidation)
	}
	var instances []model.Instance
	if err := g.db.WithContext(ctx).Where("id IN ?", group.InstanceIDs).Find(&instances).Error; err != nil {
		return BatchResult{}, err
	}
	if len(instances) != len(group.InstanceIDs) {
		return BatchResult{}, ErrNotFound
	}
	type planned struct {
		instance model.Instance
		next     string
		taskType string
		payload  string
	}
	plans := make([]planned, 0, len(instances))
	for _, instance := range instances {
		next, taskType, err := transitionPlan(action, instance.Status)
		if err != nil {
			return BatchResult{}, err
		}
		profile, err := loadOptionalProfile(ctx, g.db, instance.ProfileID)
		if err != nil {
			return BatchResult{}, err
		}
		payload, err := taskPayload(instance, profile, "")
		if err != nil {
			return BatchResult{}, err
		}
		plans = append(plans, planned{instance: instance, next: next, taskType: taskType, payload: payload})
	}
	batch := model.BatchTask{GroupID: groupID, Type: action, Status: "expanded"}
	batch.CreatedBy = accountID
	taskIDs := make([]string, 0, len(plans))
	err = g.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&batch).Error; err != nil {
			return err
		}
		for _, plan := range plans {
			result := tx.Model(&model.Instance{}).Where("id = ? AND status = ?", plan.instance.ID, plan.instance.Status).Update("status", plan.next)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("%w: instance status changed", ErrConflict)
			}
			task := model.InstanceTask{
				InstanceID: plan.instance.ID, HostID: plan.instance.HostID,
				Type: plan.taskType, Payload: plan.payload, Status: "pending",
			}
			task.CreatedBy = accountID
			if err := tx.Create(&task).Error; err != nil {
				return err
			}
			taskIDs = append(taskIDs, task.ID)
		}
		return tx.Create(&model.AuditEvent{AccountID: accountID, Action: "group.batch", Target: groupID, Detail: action}).Error
	})
	if err != nil {
		return BatchResult{}, err
	}
	return BatchResult{ID: batch.ID, GroupID: groupID, Type: action, Status: batch.Status, TaskIDs: taskIDs}, nil
}

func (g *Groups) members(ctx context.Context, groupID string) ([]string, error) {
	var rows []model.ControlGroupMember
	if err := g.db.WithContext(ctx).Where("group_id = ?", groupID).Order("position asc, created_at asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.InstanceID)
	}
	return ids, nil
}

func (g *Groups) targets(ctx context.Context, instanceIDs []string) ([]control.Target, error) {
	if len(instanceIDs) == 0 {
		return nil, ErrNotFound
	}
	var instances []model.Instance
	if err := g.db.WithContext(ctx).Where("id IN ?", instanceIDs).Find(&instances).Error; err != nil {
		return nil, err
	}
	byID := make(map[string]model.Instance, len(instances))
	for _, instance := range instances {
		byID[instance.ID] = instance
	}
	out := make([]control.Target, 0, len(instanceIDs))
	for _, instanceID := range instanceIDs {
		instance, ok := byID[instanceID]
		if !ok {
			return nil, ErrNotFound
		}
		if instance.DisplayWidth > 0 && instance.DisplayHeight > 0 {
			out = append(out, control.Target{InstanceID: instance.ID, Width: instance.DisplayWidth, Height: instance.DisplayHeight})
			continue
		}
		profile, err := loadOptionalProfile(ctx, g.db, instance.ProfileID)
		if err != nil {
			return nil, err
		}
		out = append(out, control.Target{InstanceID: instance.ID, Width: profile.Width, Height: profile.Height})
	}
	return out, nil
}

func uniqueIDs(ids []string) ([]string, error) {
	if len(ids) < 2 {
		return nil, fmt.Errorf("%w: a group needs at least two instances", ErrValidation)
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ids))
	for _, item := range ids {
		if item == "" {
			return nil, fmt.Errorf("%w: instance id is empty", ErrValidation)
		}
		if _, ok := seen[item]; ok {
			return nil, fmt.Errorf("%w: duplicate instance", ErrValidation)
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out, nil
}
