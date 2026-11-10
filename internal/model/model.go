package model

import (
	"github.com/xingshanghe/x-phone/internal/id"

	"gorm.io/gorm"
)

type Base struct {
	ID        string         `json:"id" gorm:"column:id;type:varchar(32);primaryKey"`
	CreatedAt int64          `json:"createdAt" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt int64          `json:"updatedAt" gorm:"column:updated_at;autoUpdateTime"`
	CreatedBy string         `json:"createdBy" gorm:"column:created_by;type:varchar(32)"`
	UpdatedBy string         `json:"updatedBy" gorm:"column:updated_by;type:varchar(32)"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"column:deleted_at;index"`
}

func (b *Base) BeforeCreate(*gorm.DB) error {
	if b.ID == "" {
		b.ID = id.New()
	}
	return nil
}

type Account struct {
	Base
	Username     string `json:"username" gorm:"column:username;type:varchar(64);uniqueIndex"`
	PasswordHash string `json:"-" gorm:"column:password_hash;type:varchar(128)"`
}

func (Account) TableName() string { return "account" }

type Host struct {
	Base
	Name            string `json:"name" gorm:"column:name;type:varchar(128)"`
	Addr            string `json:"addr" gorm:"column:addr;type:varchar(256)"`
	Status          string `json:"status" gorm:"column:status;type:varchar(32);index"`
	Capacity        int    `json:"capacity" gorm:"column:capacity"`
	Binderfs        string `json:"binderfs" gorm:"column:binderfs;type:varchar(16)"`
	TokenHash       string `json:"-" gorm:"column:token_hash;type:varchar(64);uniqueIndex"`
	LastHeartbeatAt int64  `json:"lastHeartbeatAt" gorm:"column:last_heartbeat_at"`
	RunningCount    int    `json:"runningCount" gorm:"column:running_count"`
}

func (Host) TableName() string { return "host" }

type DeviceProfile struct {
	Base
	Slug         string `json:"slug" gorm:"column:slug;type:varchar(64);uniqueIndex"`
	Brand        string `json:"brand" gorm:"column:brand;type:varchar(64)"`
	Manufacturer string `json:"manufacturer" gorm:"column:manufacturer;type:varchar(64)"`
	Model        string `json:"model" gorm:"column:model;type:varchar(128)"`
	Device       string `json:"device" gorm:"column:device;type:varchar(64)"`
	Product      string `json:"product" gorm:"column:product;type:varchar(64)"`
	Fingerprint  string `json:"fingerprint" gorm:"column:fingerprint;type:varchar(256)"`
	Release      string `json:"release" gorm:"column:release;type:varchar(32)"`
	SDK          string `json:"sdk" gorm:"column:sdk;type:varchar(16)"`
	Width        int    `json:"width" gorm:"column:width"`
	Height       int    `json:"height" gorm:"column:height"`
	DPI          int    `json:"dpi" gorm:"column:dpi"`
	Locale       string `json:"locale" gorm:"column:locale;type:varchar(32)"`
	Timezone     string `json:"timezone" gorm:"column:timezone;type:varchar(64)"`
	GPU          string `json:"gpu" gorm:"column:gpu;type:varchar(64)"`
	CPUCores     int    `json:"cpuCores" gorm:"column:cpu_cores"`
	MemoryMB     int    `json:"memoryMb" gorm:"column:memory_mb"`
	TouchPoints  int    `json:"touchPoints" gorm:"column:touch_points"`
}

func (DeviceProfile) TableName() string { return "device_profile" }

type Instance struct {
	Base
	Name          string `json:"name" gorm:"column:name;type:varchar(64);index"`
	HostID        string `json:"hostId" gorm:"column:host_id;type:varchar(32);index"`
	ProfileID     string `json:"profileId" gorm:"column:profile_id;type:varchar(64);index"`
	Status        string `json:"status" gorm:"column:status;type:varchar(32);index"`
	HTTPProxy     string `json:"httpProxy,omitempty" gorm:"column:http_proxy;type:varchar(256)"`
	ADBPort       int    `json:"adbPort" gorm:"column:adb_port"`
	ADBSerial     string `json:"adbSerial,omitempty" gorm:"column:adb_serial;type:varchar(128)"`
	DisplayWidth  int    `json:"displayWidth,omitempty" gorm:"column:display_width"`
	DisplayHeight int    `json:"displayHeight,omitempty" gorm:"column:display_height"`
	ContainerID   string `json:"containerId,omitempty" gorm:"column:container_id;type:varchar(128)"`
	RuntimeKind   string `json:"runtimeKind" gorm:"column:runtime_kind;type:varchar(32)"`
	LastError     string `json:"lastError,omitempty" gorm:"column:last_error;type:text"`
	ProfileReport string `json:"profileReport,omitempty" gorm:"column:profile_report;type:text"`
}

func (Instance) TableName() string { return "instance" }

type InstanceTask struct {
	Base
	InstanceID string `json:"instanceId" gorm:"column:instance_id;type:varchar(32);index"`
	HostID     string `json:"hostId" gorm:"column:host_id;type:varchar(32);index"`
	Type       string `json:"type" gorm:"column:type;type:varchar(32);index"`
	Payload    string `json:"payload" gorm:"column:payload;type:text"`
	Status     string `json:"status" gorm:"column:status;type:varchar(32);index"`
	ClaimedAt  int64  `json:"claimedAt" gorm:"column:claimed_at"`
	FinishedAt int64  `json:"finishedAt" gorm:"column:finished_at"`
	Log        string `json:"log" gorm:"column:log;type:text"`
}

func (InstanceTask) TableName() string { return "instance_task" }

type AuditEvent struct {
	Base
	AccountID string `json:"accountId" gorm:"column:account_id;type:varchar(32);index"`
	Action    string `json:"action" gorm:"column:action;type:varchar(64)"`
	Target    string `json:"target" gorm:"column:target;type:varchar(64)"`
	Detail    string `json:"detail" gorm:"column:detail;type:text"`
}

func (AuditEvent) TableName() string { return "audit_event" }

type ControlGroup struct {
	Base
	Name      string `json:"name" gorm:"column:name;type:varchar(64)"`
	ProfileID string `json:"profileId" gorm:"column:profile_id;type:varchar(64);index"`
}

func (ControlGroup) TableName() string { return "control_group" }

type ControlGroupMember struct {
	ID         string `json:"id" gorm:"column:id;type:varchar(32);primaryKey"`
	GroupID    string `json:"groupId" gorm:"column:group_id;type:varchar(32);index"`
	InstanceID string `json:"instanceId" gorm:"column:instance_id;type:varchar(32);uniqueIndex"`
	Position   int    `json:"position" gorm:"column:position"`
	CreatedAt  int64  `json:"createdAt" gorm:"column:created_at;autoCreateTime"`
}

func (ControlGroupMember) TableName() string { return "control_group_member" }

type BatchTask struct {
	Base
	GroupID string `json:"groupId" gorm:"column:group_id;type:varchar(32);index"`
	Type    string `json:"type" gorm:"column:type;type:varchar(32);index"`
	Payload string `json:"payload" gorm:"column:payload;type:text"`
	Status  string `json:"status" gorm:"column:status;type:varchar(32);index"`
}

func (BatchTask) TableName() string { return "batch_task" }
