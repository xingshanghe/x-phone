package database

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/xingshanghe/x-phone/internal/config"
	"github.com/xingshanghe/x-phone/internal/model"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Open(cfg config.Config) (*gorm.DB, error) {
	dialector, err := dialectorFor(cfg)
	if err != nil {
		return nil, err
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.New(log.New(os.Stdout, "", log.LstdFlags), logger.Config{
		SlowThreshold:             time.Second,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: true,
	})})
	if err != nil {
		return nil, err
	}
	err = db.AutoMigrate(
		&model.Account{},
		&model.Host{},
		&model.DeviceProfile{},
		&model.Instance{},
		&model.InstanceTask{},
		&model.AuditEvent{},
		&model.ControlGroup{},
		&model.ControlGroupMember{},
		&model.BatchTask{},
	)
	return db, err
}

func dialectorFor(cfg config.Config) (gorm.Dialector, error) {
	switch cfg.Driver {
	case "mysql":
		return mysql.Open(cfg.DSN), nil
	case "sqlite", "":
		if err := os.MkdirAll(filepath.Dir(cfg.DSN), 0o755); err != nil && filepath.Dir(cfg.DSN) != "." {
			return nil, err
		}
		return sqlite.Open(cfg.DSN), nil
	default:
		return nil, fmt.Errorf("unsupported database driver %q", cfg.Driver)
	}
}
