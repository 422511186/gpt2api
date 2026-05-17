package register

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/repo"
	"github.com/kleinai/backend/pkg/crypto"
)

// RegisterMode defines the registration mode
type RegisterMode string

const (
	ModeTotal     RegisterMode = "total"
	ModeQuota     RegisterMode = "quota"
	ModeAvailable RegisterMode = "available"
)

// RegisterStats holds runtime statistics
type RegisterStats struct {
	JobID            string  `json:"job_id,omitempty"`
	Success          int     `json:"success"`
	Fail             int     `json:"fail"`
	Done             int     `json:"done"`
	Running          int     `json:"running"`
	Threads          int     `json:"threads"`
	ElapsedSeconds   float64 `json:"elapsed_seconds"`
	AvgSeconds       float64 `json:"avg_seconds"`
	SuccessRate      float64 `json:"success_rate"`
	CurrentQuota     int     `json:"current_quota"`
	CurrentAvailable int     `json:"current_available"`
	StartedAt        string  `json:"started_at,omitempty"`
	UpdatedAt        string  `json:"updated_at,omitempty"`
	FinishedAt       string  `json:"finished_at,omitempty"`
}

// RegisterLog represents a log entry
type RegisterLog struct {
	Time  string `json:"time"`
	Text  string `json:"text"`
	Level string `json:"level"` // "info", "green", "red", "yellow"
}

// RegisterConfig holds the registration configuration
type RegisterConfig struct {
	Mail            MailConfig    `json:"mail"`
	Proxy           string        `json:"proxy"`
	Total           int           `json:"total"`
	Threads         int           `json:"threads"`
	Mode            RegisterMode  `json:"mode"`
	TargetQuota     int           `json:"target_quota"`
	TargetAvailable int           `json:"target_available"`
	CheckInterval   int           `json:"check_interval"`
	Enabled         bool          `json:"enabled"`
	Stats           RegisterStats `json:"stats"`
	Logs            []RegisterLog `json:"logs"`
}

// DefaultRegisterConfig returns the default configuration
func DefaultRegisterConfig() RegisterConfig {
	return RegisterConfig{
		Mail:          DefaultMailConfig(),
		Total:         10,
		Threads:       3,
		Mode:          ModeTotal,
		TargetQuota:   100,
		TargetAvailable: 10,
		CheckInterval: 5,
		Enabled:       false,
		Stats: RegisterStats{
			Threads: 3,
		},
		Logs: []RegisterLog{},
	}
}

// RegisterService manages the registration process
type RegisterService struct {
	mu          sync.RWMutex
	config      RegisterConfig
	accountRepo *repo.AccountRepo
	aes         *crypto.AESGCM
	pool        AccountPoolReloader
	cancel      context.CancelFunc
	startTime   time.Time

	// Log subscribers
	subscribersMu sync.RWMutex
	subscribers   map[chan RegisterConfig]struct{}
}

// AccountPoolReloader is an interface for reloading the account pool
type AccountPoolReloader interface {
	Reload(provider string)
}

// NewRegisterService creates a new register service
func NewRegisterService(
	accountRepo *repo.AccountRepo,
	aes *crypto.AESGCM,
	pool AccountPoolReloader,
) *RegisterService {
	return &RegisterService{
		accountRepo: accountRepo,
		aes:         aes,
		pool:        pool,
		config:      DefaultRegisterConfig(),
		subscribers: make(map[chan RegisterConfig]struct{}),
	}
}

// GetConfig returns the current configuration
func (s *RegisterService) GetConfig() RegisterConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// UpdateConfig updates the configuration
func (s *RegisterService) UpdateConfig(updates RegisterConfig) RegisterConfig {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Merge updates
	if updates.Mail.RequestTimeout > 0 {
		s.config.Mail.RequestTimeout = updates.Mail.RequestTimeout
	}
	if updates.Mail.WaitTimeout > 0 {
		s.config.Mail.WaitTimeout = updates.Mail.WaitTimeout
	}
	if updates.Mail.WaitInterval > 0 {
		s.config.Mail.WaitInterval = updates.Mail.WaitInterval
	}
	if updates.Mail.UserAgent != "" {
		s.config.Mail.UserAgent = updates.Mail.UserAgent
	}
	if updates.Mail.Proxy != "" {
		s.config.Mail.Proxy = updates.Mail.Proxy
	}
	if len(updates.Mail.Providers) > 0 {
		s.config.Mail.Providers = updates.Mail.Providers
	}
	if updates.Proxy != "" {
		s.config.Proxy = updates.Proxy
	}
	if updates.Total > 0 {
		s.config.Total = updates.Total
	}
	if updates.Threads > 0 {
		s.config.Threads = updates.Threads
		s.config.Stats.Threads = updates.Threads
	}
	if updates.Mode != "" {
		s.config.Mode = updates.Mode
	}
	if updates.TargetQuota > 0 {
		s.config.TargetQuota = updates.TargetQuota
	}
	if updates.TargetAvailable > 0 {
		s.config.TargetAvailable = updates.TargetAvailable
	}
	if updates.CheckInterval > 0 {
		s.config.CheckInterval = updates.CheckInterval
	}

	return s.config
}

// LoadConfig loads configuration from JSON
func (s *RegisterService) LoadConfig(data []byte) error {
	if len(data) == 0 {
		return nil
	}

	var cfg RegisterConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Normalize config
	if cfg.Total <= 0 {
		cfg.Total = 10
	}
	if cfg.Threads <= 0 {
		cfg.Threads = 3
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeTotal
	}
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 5
	}

	s.config = cfg
	s.config.Enabled = false // Don't auto-start

	return nil
}

// ExportConfig exports configuration to JSON
func (s *RegisterService) ExportConfig() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Don't export logs and stats
	export := s.config
	export.Logs = nil
	export.Stats = RegisterStats{Threads: s.config.Threads}
	export.Enabled = false

	return json.Marshal(export)
}

// Start starts the registration process
func (s *RegisterService) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.config.Enabled {
		return nil // Already running
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.config.Enabled = true
	s.startTime = time.Now()
	s.config.Stats = RegisterStats{
		JobID:     uuid.New().String(),
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Threads:   s.config.Threads,
	}
	s.config.Logs = []RegisterLog{}

	s.appendLogLocked(fmt.Sprintf("注册任务启动，模式=%s，线程数=%d", s.config.Mode, s.config.Threads), "yellow")

	go s.run(ctx)

	return nil
}

// Stop stops the registration process
func (s *RegisterService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.config.Enabled = false
	s.config.Stats.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	s.appendLogLocked("已请求停止注册任务，正在等待当前运行任务结束", "yellow")
}

// Reset resets the statistics
func (s *RegisterService) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.config.Logs = []RegisterLog{}
	s.config.Stats = RegisterStats{
		Threads: s.config.Threads,
	}
	s.startTime = time.Time{}
}

// Subscribe subscribes to config updates
func (s *RegisterService) Subscribe() chan RegisterConfig {
	ch := make(chan RegisterConfig, 10)
	s.subscribersMu.Lock()
	s.subscribers[ch] = struct{}{}
	s.subscribersMu.Unlock()
	return ch
}

// Unsubscribe unsubscribes from config updates
func (s *RegisterService) Unsubscribe(ch chan RegisterConfig) {
	s.subscribersMu.Lock()
	delete(s.subscribers, ch)
	s.subscribersMu.Unlock()
	close(ch)
}

func (s *RegisterService) notifySubscribers() {
	s.subscribersMu.RLock()
	defer s.subscribersMu.RUnlock()

	cfg := s.GetConfig()
	for ch := range s.subscribers {
		select {
		case ch <- cfg:
		default:
		}
	}
}

func (s *RegisterService) run(ctx context.Context) {
	threads := s.config.Threads
	if threads <= 0 {
		threads = 3
	}

	sem := make(chan struct{}, threads)
	var wg sync.WaitGroup

	submitted := 0

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			s.finish()
			return
		default:
		}

		// Check if target reached
		if s.targetReached(ctx, submitted) {
			if s.config.Mode != ModeTotal {
				time.Sleep(time.Duration(s.config.CheckInterval) * time.Second)
				continue
			}
			wg.Wait()
			s.finish()
			return
		}

		// Submit new registration task
		select {
		case sem <- struct{}{}:
			submitted++
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				defer func() { <-sem }()

				result, err := s.registerOne(ctx, index)
				if err != nil {
					s.recordFailure(err, index)
					return
				}
				s.recordSuccess(result, index)
			}(submitted)
		case <-ctx.Done():
			wg.Wait()
			s.finish()
			return
		}
	}
}

func (s *RegisterService) registerOne(ctx context.Context, index int) (*RegistrationResult, error) {
	registrar := NewPlatformRegistrar(s.config.Proxy, &s.config.Mail)
	defer registrar.Close()

	log := func(text, level string) {
		s.appendLog(text, level)
	}

	return registrar.Register(ctx, index, log)
}

func (s *RegisterService) targetReached(ctx context.Context, submitted int) bool {
	switch s.config.Mode {
	case ModeQuota:
		metrics := s.getPoolMetrics()
		s.updateMetrics(metrics)
		reached := metrics["quota"] >= s.config.TargetQuota
		if reached {
			s.appendLog(fmt.Sprintf("检查号池：当前额度=%d，目标额度=%d，跳过注册", metrics["quota"], s.config.TargetQuota), "yellow")
		} else {
			s.appendLog(fmt.Sprintf("检查号池：当前额度=%d，目标额度=%d，继续注册", metrics["quota"], s.config.TargetQuota), "yellow")
		}
		return reached
	case ModeAvailable:
		metrics := s.getPoolMetrics()
		s.updateMetrics(metrics)
		reached := metrics["available"] >= s.config.TargetAvailable
		if reached {
			s.appendLog(fmt.Sprintf("检查号池：当前账号=%d，目标账号=%d，跳过注册", metrics["available"], s.config.TargetAvailable), "yellow")
		} else {
			s.appendLog(fmt.Sprintf("检查号池：当前账号=%d，目标账号=%d，继续注册", metrics["available"], s.config.TargetAvailable), "yellow")
		}
		return reached
	default:
		return submitted >= s.config.Total
	}
}

func (s *RegisterService) getPoolMetrics() map[string]int {
	// This is a simplified implementation
	// In a real implementation, you would query the database
	return map[string]int{
		"quota":    0,
		"available": 0,
	}
}

func (s *RegisterService) updateMetrics(metrics map[string]int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.config.Stats.CurrentQuota = metrics["quota"]
	s.config.Stats.CurrentAvailable = metrics["available"]
	s.config.Stats.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	// Update elapsed time
	if !s.startTime.IsZero() {
		elapsed := time.Since(s.startTime).Seconds()
		s.config.Stats.ElapsedSeconds = elapsed

		if s.config.Stats.Success > 0 {
			s.config.Stats.AvgSeconds = elapsed / float64(s.config.Stats.Success)
		}

		total := s.config.Stats.Success + s.config.Stats.Fail
		if total > 0 {
			s.config.Stats.SuccessRate = float64(s.config.Stats.Success) * 100 / float64(total)
		}
	}
}

func (s *RegisterService) recordSuccess(result *RegistrationResult, index int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.config.Stats.Success++
	s.config.Stats.Done++
	s.appendLogLocked(fmt.Sprintf("%s 注册成功", result.Email), "green")

	// Save to account pool
	if err := s.saveToAccountPool(result); err != nil {
		s.appendLogLocked(fmt.Sprintf("保存账号 %s 失败: %v", result.Email, err), "red")
	}

	s.notifySubscribers()
}

func (s *RegisterService) recordFailure(err error, index int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.config.Stats.Fail++
	s.config.Stats.Done++
	s.appendLogLocked(fmt.Sprintf("任务%d 注册失败: %v", index, err), "red")

	s.notifySubscribers()
}

func (s *RegisterService) finish() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.config.Enabled = false
	s.config.Stats.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	s.appendLogLocked(fmt.Sprintf("注册任务结束，成功%d，失败%d", s.config.Stats.Success, s.config.Stats.Fail), "yellow")

	s.notifySubscribers()
}

func (s *RegisterService) saveToAccountPool(result *RegistrationResult) error {
	// Encrypt tokens
	var rtEnc, atEnc, stEnc []byte
	var err error

	if result.RefreshToken != "" {
		rtEnc, err = s.aes.Encrypt([]byte(result.RefreshToken))
		if err != nil {
			return fmt.Errorf("encrypt refresh token: %w", err)
		}
	}

	if result.AccessToken != "" {
		atEnc, err = s.aes.Encrypt([]byte(result.AccessToken))
		if err != nil {
			return fmt.Errorf("encrypt access token: %w", err)
		}
	}

	if result.SessionToken != "" {
		stEnc, err = s.aes.Encrypt([]byte(result.SessionToken))
		if err != nil {
			return fmt.Errorf("encrypt session token: %w", err)
		}
	}

	// Create OAuth metadata
	meta := map[string]any{
		"source":     "register",
		"email":      result.Email,
		"password":   result.Password,
		"created_at": result.CreatedAt,
	}
	metaBytes, _ := json.Marshal(meta)
	metaStr := string(metaBytes)

	// Set access token expiration
	now := time.Now().UTC()

	account := &model.Account{
		Provider:           model.ProviderGPT,
		Name:               result.Email,
		AuthType:           model.AuthTypeOAuth,
		CredentialEnc:      rtEnc,
		RefreshTokenEnc:    rtEnc,
		AccessTokenEnc:     atEnc,
		SessionTokenEnc:    stEnc,
		OAuthMeta:          &metaStr,
		AccessTokenExpiresAt: &now,
		Status:             model.AccountStatusEnabled,
		Weight:             10,
	}

	if err := s.accountRepo.Create(context.Background(), account); err != nil {
		return fmt.Errorf("create account: %w", err)
	}

	// Reload pool
	s.pool.Reload(model.ProviderGPT)

	return nil
}

func (s *RegisterService) appendLog(text, level string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appendLogLocked(text, level)
}

func (s *RegisterService) appendLogLocked(text, level string) {
	log := RegisterLog{
		Time:  time.Now().UTC().Format(time.RFC3339),
		Text:  text,
		Level: level,
	}

	s.config.Logs = append(s.config.Logs, log)

	// Keep only last 300 logs
	if len(s.config.Logs) > 300 {
		s.config.Logs = s.config.Logs[len(s.config.Logs)-300:]
	}

	s.notifySubscribers()
}
