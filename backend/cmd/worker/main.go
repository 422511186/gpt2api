// Command worker 异步任务消费者（asynq）。
//
// 监听队列：critical / default / low
// 详见 docs/02-后端规范.md §8。
package main

import (
	"context"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"

	"github.com/kleinai/backend/internal/bootstrap"
	"github.com/kleinai/backend/internal/repo"
	"github.com/kleinai/backend/internal/service"
	"github.com/kleinai/backend/pkg/logger"
)

const serviceName = "worker"

// Task type 常量（与发布端保持一致）。
const (
	TaskGenImage    = "gen:image"
	TaskGenVideo    = "gen:video"
	TaskPoolHealth  = "pool:health"
	TaskBillSettle  = "bill:settle"
	TaskEmailSend   = "email:send"
	TaskWebhookSend = "webhook:notify"
)

func main() {
	deps, err := bootstrap.Init(serviceName)
	if err != nil {
		panic(err)
	}
	defer logger.Sync()

	if deps.Cfg.Redis.Addr == "" {
		logger.L().Fatal("worker requires redis")
	}

	if deps.DB != nil && deps.AES != nil {
		// 初始化 repos
		sysCfgRepo := repo.NewSystemConfigRepo(deps.DB)
		accountRepo := repo.NewAccountRepo(deps.DB)
		proxyRepo := repo.NewProxyRepo(deps.DB)

		// 初始化 services
		sysCfgSvc := service.NewSystemConfigService(sysCfgRepo)
		proxySvc := service.NewProxyService(proxyRepo, deps.AES)
		pool := service.NewAccountPool(accountRepo, 30*time.Second)
		accountAdmin := service.NewAccountAdminService(accountRepo, pool, deps.AES)
		openaiOAuth := service.NewOpenAIOAuthService(sysCfgSvc)
		accountTest := service.NewAccountTestService(accountRepo, pool, proxySvc, sysCfgSvc, openaiOAuth, deps.AES)
		accountAdmin.SetTestService(accountTest)

		// Grok CF Refresh 服务
		service.NewGrokCFRefreshService(sysCfgSvc, proxySvc).Start(context.Background())

		// 账号失效检测服务
		service.NewAccountInvalidCheckService(sysCfgSvc, accountAdmin, accountTest, accountRepo).Start(context.Background())
	}

	srv := asynq.NewServer(
		asynq.RedisClientOpt{
			Addr:     deps.Cfg.Redis.Addr,
			Password: deps.Cfg.Redis.Password,
			DB:       deps.Cfg.Redis.DB,
		},
		asynq.Config{
			Concurrency: 16,
			Queues: map[string]int{
				"critical": 6,
				"default":  3,
				"low":      1,
			},
			Logger:          &asynqZap{l: logger.L()},
			ShutdownTimeout: deps.Cfg.Server.ShutdownTimeout,
			HealthCheckFunc: func(err error) {
				if err != nil {
					logger.L().Warn("asynq health", zap.Error(err))
				}
			},
		},
	)

	mux := asynq.NewServeMux()

	// TODO Sprint 5+: 注册具体任务 handler
	mux.HandleFunc(TaskPoolHealth, func(ctx context.Context, t *asynq.Task) error {
		logger.FromCtx(ctx).Info("pool health tick", zap.String("task", t.Type()))
		return nil
	})

	go func() {
		if err := srv.Run(mux); err != nil {
			logger.L().Fatal("asynq run", zap.Error(err))
		}
	}()

	logger.L().Info("worker started", zap.String("redis", deps.Cfg.Redis.Addr))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	srv.Shutdown()
	logger.L().Info("worker shutdown done")
}

// asynqZap 把 asynq 日志转 zap。
type asynqZap struct{ l *zap.Logger }

func (a *asynqZap) Debug(args ...any) { a.l.Sugar().Debug(args...) }
func (a *asynqZap) Info(args ...any)  { a.l.Sugar().Info(args...) }
func (a *asynqZap) Warn(args ...any)  { a.l.Sugar().Warn(args...) }
func (a *asynqZap) Error(args ...any) { a.l.Sugar().Error(args...) }
func (a *asynqZap) Fatal(args ...any) { a.l.Sugar().Fatal(args...) }
