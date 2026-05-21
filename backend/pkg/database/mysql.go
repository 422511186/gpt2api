// Package database 封装 MySQL（GORM）与 Redis 客户端。
package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/kleinai/backend/pkg/config"
	"github.com/kleinai/backend/pkg/logger"
)

// NewMySQL 用 GORM 创建 MySQL 连接（含连接池配置与慢查询日志）。
// 在生产环境下会重试连接以应对 Docker DNS 启动时序问题。
func NewMySQL(c *config.MySQL) (*gorm.DB, error) {
	if c.DSN == "" {
		return nil, fmt.Errorf("mysql dsn empty")
	}

	gormLog := gormlogger.New(
		zapWriter{l: logger.L()},
		gormlogger.Config{
			SlowThreshold:             c.SlowThreshold,
			LogLevel:                  gormlogger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)

	// 生产环境重试连接，应对 Docker DNS 解析时序问题
	// 通过 DSN 判断是否为生产环境（包含容器名如 mysql、redis 等）
	maxRetries := 1
	retryDelay := time.Duration(0)
	// 如果 DSN 中包含容器名（非 localhost/127.0.0.1），认为是容器环境需要重试
	if strings.Contains(c.DSN, "tcp(mysql:") || strings.Contains(c.DSN, "@tcp(mysql:") {
		maxRetries = 5
		retryDelay = 3 * time.Second
	}

	var db *gorm.DB
	var err error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		db, err = gorm.Open(mysql.Open(c.DSN), &gorm.Config{
			Logger:                                   gormLog,
			PrepareStmt:                              true,
			DisableForeignKeyConstraintWhenMigrating: true,
			NowFunc:                                  func() time.Time { return time.Now().UTC() },
		})
		if err == nil {
			break
		}
		if attempt < maxRetries {
			logger.L().Warn("mysql connection failed, retrying...",
				zap.Int("attempt", attempt),
				zap.Int("max_retries", maxRetries),
				zap.Duration("delay", retryDelay),
				zap.Error(err),
			)
			time.Sleep(retryDelay)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("gorm open: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql db: %w", err)
	}

	maxOpen := c.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = 100
	}
	maxIdle := c.MaxIdleConns
	if maxIdle <= 0 {
		maxIdle = 20
	}
	lifetime := c.ConnMaxLifetime
	if lifetime <= 0 {
		lifetime = time.Hour
	}

	sqlDB.SetMaxOpenConns(maxOpen)
	sqlDB.SetMaxIdleConns(maxIdle)
	sqlDB.SetConnMaxLifetime(lifetime)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	logger.L().Info("mysql connected",
		zap.Int("max_open", maxOpen),
		zap.Int("max_idle", maxIdle),
		zap.Duration("lifetime", lifetime),
	)
	return db, nil
}

// zapWriter 让 GORM logger 写入 zap。
type zapWriter struct{ l *zap.Logger }

func (z zapWriter) Printf(format string, args ...any) {
	z.l.Sugar().Infof(format, args...)
}