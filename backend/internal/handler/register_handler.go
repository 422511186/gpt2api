package handler

import (
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kleinai/backend/internal/dto"
	"github.com/kleinai/backend/internal/service/register"
	"github.com/kleinai/backend/pkg/errcode"
	"github.com/kleinai/backend/pkg/response"
)

// RegisterHandler handles register API requests
type RegisterHandler struct {
	svc *register.RegisterService
}

// NewRegisterHandler creates a new register handler
func NewRegisterHandler(svc *register.RegisterService) *RegisterHandler {
	return &RegisterHandler{svc: svc}
}

// GetConfig GET /admin/api/v1/register
func (h *RegisterHandler) GetConfig(c *gin.Context) {
	config := h.svc.GetConfig()
	response.OK(c, gin.H{"register": config})
}

// UpdateConfig POST /admin/api/v1/register
func (h *RegisterHandler) UpdateConfig(c *gin.Context) {
	var req dto.RegisterConfigReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, errcode.InvalidParam.WithMsg(err.Error()))
		return
	}

	// Convert request to config
	updates := register.RegisterConfig{}

	if req.Mail != nil {
		updates.Mail = register.MailConfig{
			RequestTimeout: req.Mail.RequestTimeout,
			WaitTimeout:    req.Mail.WaitTimeout,
			WaitInterval:   req.Mail.WaitInterval,
			UserAgent:      req.Mail.UserAgent,
			Proxy:          req.Mail.Proxy,
		}
		for _, p := range req.Mail.Providers {
			updates.Mail.Providers = append(updates.Mail.Providers, register.MailProviderConfig{
				Type:            p.Type,
				Enable:          p.Enable,
				APIBase:         p.APIBase,
				APIKey:          p.APIKey,
				AdminPassword:   p.AdminPassword,
				Domain:          p.Domain,
				DefaultDomain:   p.DefaultDomain,
				Subdomain:       p.Subdomain,
				Wildcard:        p.Wildcard,
				RandomSubdomain: p.RandomSubdomain,
				DDGToken:        p.DDGToken,
				CFInboxJWT:      p.CFInboxJWT,
				CFAPIBase:       p.CFAPIBase,
				CFAPIKey:        p.CFAPIKey,
				CFAuthMode:      p.CFAuthMode,
				CFDomain:        p.CFDomain,
				CFCreatePath:    p.CFCreatePath,
				CFMessagesPath:  p.CFMessagesPath,
				ExpiryTime:      p.ExpiryTime,
			})
		}
	}

	updates.Proxy = req.Proxy
	updates.FlareSolverrURL = req.FlareSolverrURL
	updates.Total = req.Total
	updates.Threads = req.Threads
	if req.Mode != "" {
		updates.Mode = register.RegisterMode(req.Mode)
	}
	updates.TargetQuota = req.TargetQuota
	updates.TargetAvailable = req.TargetAvailable
	updates.CheckInterval = req.CheckInterval

	config := h.svc.UpdateConfig(updates)
	response.OK(c, gin.H{"register": config})
}

// Start POST /admin/api/v1/register/start
func (h *RegisterHandler) Start(c *gin.Context) {
	if err := h.svc.Start(); err != nil {
		response.Fail(c, errcode.Internal.Wrap(err))
		return
	}
	config := h.svc.GetConfig()
	response.OK(c, gin.H{"register": config})
}

// Stop POST /admin/api/v1/register/stop
func (h *RegisterHandler) Stop(c *gin.Context) {
	h.svc.Stop()
	config := h.svc.GetConfig()
	response.OK(c, gin.H{"register": config})
}

// Reset POST /admin/api/v1/register/reset
func (h *RegisterHandler) Reset(c *gin.Context) {
	h.svc.Reset()
	config := h.svc.GetConfig()
	response.OK(c, gin.H{"register": config})
}

// Events GET /admin/api/v1/register/events (SSE)
func (h *RegisterHandler) Events(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Transfer-Encoding", "chunked")

	ch := h.svc.Subscribe()
	defer h.svc.Unsubscribe(ch)

	// Send initial config
	config := h.svc.GetConfig()
	data, _ := jsonMarshal(config)
	c.Writer.Write([]byte("data: "))
	c.Writer.Write(data)
	c.Writer.Write([]byte("\n\n"))
	c.Writer.Flush()

	// Stream updates
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case cfg, ok := <-ch:
			if !ok {
				return
			}
			data, _ := jsonMarshal(cfg)
			c.Writer.Write([]byte("data: "))
			c.Writer.Write(data)
			c.Writer.Write([]byte("\n\n"))
			c.Writer.Flush()
		case <-ticker.C:
			// Send heartbeat
			c.Writer.Write([]byte(": heartbeat\n\n"))
			c.Writer.Flush()
		case <-c.Request.Context().Done():
			return
		case <-c.Done():
			return
		}
	}
}

func jsonMarshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}
