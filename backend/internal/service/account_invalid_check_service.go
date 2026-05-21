package service

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/repo"
	"github.com/kleinai/backend/pkg/logger"
)

// AccountInvalidCheckService 后台定时检测失效账号服务。
type AccountInvalidCheckService struct {
	cfg       *SystemConfigService
	account   *AccountAdminService
	testSvc   *AccountTestService
	accountRepo *repo.AccountRepo
}

// NewAccountInvalidCheckService 构造。
func NewAccountInvalidCheckService(
	cfg *SystemConfigService,
	account *AccountAdminService,
	testSvc *AccountTestService,
	accountRepo *repo.AccountRepo,
) *AccountInvalidCheckService {
	return &AccountInvalidCheckService{
		cfg:         cfg,
		account:     account,
		testSvc:     testSvc,
		accountRepo: accountRepo,
	}
}

// Start 启动后台定时检测循环。
func (s *AccountInvalidCheckService) Start(ctx context.Context) {
	if s == nil || s.cfg == nil {
		return
	}
	go s.loop(ctx)
}

func (s *AccountInvalidCheckService) loop(ctx context.Context) {
	// 启动后等待一小段时间再开始首次检测
	time.Sleep(30 * time.Second)
	s.checkOnce(ctx)

	ticker := time.NewTicker(s.cfg.InvalidCheckInterval(ctx))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkOnce(ctx)
			ticker.Reset(s.cfg.InvalidCheckInterval(ctx))
		}
	}
}

func (s *AccountInvalidCheckService) checkOnce(parent context.Context) {
	if !s.cfg.InvalidCheckEnabled(parent) {
		return
	}

	logger.L().Info("account invalid check started")

	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()

	// 获取所有启用的账号
	providers := []string{model.ProviderGPT, model.ProviderGROK}
	totalChecked := 0
	totalDisabled := 0
	var mu sync.Mutex
	sem := make(chan struct{}, 8) // 并发限制
	var wg sync.WaitGroup

	for _, provider := range providers {
		page := 1
		pageSize := 100
		for {
			items, total, err := s.accountRepo.List(ctx, repo.AccountListFilter{
				Provider: provider,
				Status:   int8Ptr(model.AccountStatusEnabled),
				Page:     page,
				PageSize: pageSize,
			})
			if err != nil {
				logger.L().Warn("account invalid check list failed",
					zap.String("provider", provider),
					zap.Error(err))
				break
			}
			if len(items) == 0 {
				break
			}

			for _, acc := range items {
				a := acc
				wg.Add(1)
				go func() {
					defer wg.Done()
					select {
					case sem <- struct{}{}:
					case <-ctx.Done():
						return
					}
					defer func() { <-sem }()

					res, err := s.testSvc.TestWithInvalidCheck(ctx, a)
					mu.Lock()
					defer mu.Unlock()
					totalChecked++

					if err != nil {
						return
					}

					if res.ShouldDisable {
						// 系统检测失效，标记为 Invalid 状态
						if err := s.accountRepo.Update(ctx, a.ID, map[string]any{
							"status":     model.AccountStatusInvalid,
							"last_error": res.Error,
						}); err == nil {
							totalDisabled++
							logger.L().Warn("account marked invalid by check",
								zap.Uint64("id", a.ID),
								zap.String("name", a.Name),
								zap.String("provider", a.Provider),
								zap.String("error", res.Error))
						}
					}
				}()
			}

			wg.Wait()

			if page*pageSize >= int(total) {
				break
			}
			page++
		}
	}

	// 刷新账号池
	s.account.reloadPoolGPTAndGrok()

	// 记录检测结果
	now := time.Now().UTC().Unix()
	result := map[string]any{
		"checked":  totalChecked,
		"disabled": totalDisabled,
		"at":       now,
	}
	_ = s.cfg.UpsertMany(ctx, map[string]any{
		SettingInvalidCheckLastRunAt:  now,
		SettingInvalidCheckLastResult: result,
	}, 0)

	logger.L().Info("account invalid check completed",
		zap.Int("checked", totalChecked),
		zap.Int("disabled", totalDisabled))
}