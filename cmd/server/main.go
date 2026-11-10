package main

import (
	"context"
	"log"

	"github.com/xingshanghe/x-phone/internal/auth"
	"github.com/xingshanghe/x-phone/internal/blob"
	"github.com/xingshanghe/x-phone/internal/config"
	"github.com/xingshanghe/x-phone/internal/database"
	"github.com/xingshanghe/x-phone/internal/httpapi"
	"github.com/xingshanghe/x-phone/internal/service"
	"go.uber.org/fx"
	"gorm.io/gorm"
)

func main() {
	app := fx.New(
		fx.Provide(
			config.Load,
			database.Open,
			blob.New,
			func(cfg config.Config) *auth.Signer { return auth.NewSigner(cfg.JWTSecret) },
			service.New,
			func(svc *service.Services, signer *auth.Signer) *httpapi.Engine {
				return httpapi.NewEngine(svc, signer)
			},
		),
		fx.Invoke(run),
	)
	app.Run()
}

func run(lc fx.Lifecycle, cfg config.Config, engine *httpapi.Engine, svc *service.Services, db *gorm.DB) {
	server := &httpServer{addr: cfg.Addr, handler: engine}
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := svc.Auth.EnsureAdmin(ctx); err != nil {
				return err
			}
			if err := svc.Profiles.Sync(ctx); err != nil {
				return err
			}
			go svc.RunSweep()
			go func() {
				if err := server.listen(); err != nil {
					log.Printf("http server stopped: %v", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			svc.Stop()
			sqlDB, err := db.DB()
			if err == nil {
				_ = sqlDB.Close()
			}
			return server.shutdown(ctx)
		},
	})
}
