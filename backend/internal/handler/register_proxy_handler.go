// Package handler 注册机代理 Handler - 将请求转发到 Python registrar 服务。
package handler

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kleinai/backend/pkg/errcode"
	"github.com/kleinai/backend/pkg/response"
)

// RegisterProxyHandler 将注册机请求代理到 Python registrar 服务。
type RegisterProxyHandler struct {
	registrarURL string // Python registrar 服务地址，如 http://klein-registrar:8080
	client       *http.Client
}

// NewRegisterProxyHandler 创建代理 handler。
func NewRegisterProxyHandler(registrarURL string) *RegisterProxyHandler {
	if registrarURL == "" {
		registrarURL = "http://localhost:8080" // 默认本地开发地址
	}
	return &RegisterProxyHandler{
		registrarURL: strings.TrimSuffix(registrarURL, "/"),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// proxyRequest 代理请求到 Python registrar 服务。
func (h *RegisterProxyHandler) proxyRequest(c *gin.Context, method, path string) {
	targetURL := fmt.Sprintf("%s/admin/api/v1/register%s", h.registrarURL, path)

	// 读取请求体
	var bodyBytes []byte
	if c.Request.Body != nil {
		bodyBytes, _ = io.ReadAll(c.Request.Body)
		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	}

	// 创建代理请求
	req, err := http.NewRequest(method, targetURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		response.Fail(c, errcode.Internal.Wrap(err))
		return
	}

	// 复制 headers（除了 Host）
	for k, v := range c.Request.Header {
		if k != "Host" {
			req.Header[k] = v
		}
	}

	// 发送请求
	resp, err := h.client.Do(req)
	if err != nil {
		response.Fail(c, errcode.Internal.WithMsg(fmt.Sprintf("registrar 服务不可达: %v", err)))
		return
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, _ := io.ReadAll(resp.Body)

	// 复制响应 headers
	for k, v := range resp.Header {
		if k != "Content-Length" && k != "Transfer-Encoding" {
			c.Header(k, strings.Join(v, ","))
		}
	}

	// 返回响应
	c.Data(resp.StatusCode, "application/json; charset=utf-8", respBody)
}

// GetConfig GET /admin/api/v1/register
func (h *RegisterProxyHandler) GetConfig(c *gin.Context) {
	h.proxyRequest(c, "GET", "")
}

// UpdateConfig POST /admin/api/v1/register
func (h *RegisterProxyHandler) UpdateConfig(c *gin.Context) {
	h.proxyRequest(c, "POST", "")
}

// Start POST /admin/api/v1/register/start
func (h *RegisterProxyHandler) Start(c *gin.Context) {
	h.proxyRequest(c, "POST", "/start")
}

// Stop POST /admin/api/v1/register/stop
func (h *RegisterProxyHandler) Stop(c *gin.Context) {
	h.proxyRequest(c, "POST", "/stop")
}

// Reset POST /admin/api/v1/register/reset
func (h *RegisterProxyHandler) Reset(c *gin.Context) {
	h.proxyRequest(c, "POST", "/reset")
}

// Events GET /admin/api/v1/register/events (SSE)
func (h *RegisterProxyHandler) Events(c *gin.Context) {
	targetURL := fmt.Sprintf("%s/admin/api/v1/register/events", h.registrarURL)

	// 构建 query 参数（JWT token）
	if token := c.Query("token"); token != "" {
		targetURL = fmt.Sprintf("%s?token=%s", targetURL, token)
	}

	// 创建代理请求
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		response.Fail(c, errcode.Internal.Wrap(err))
		return
	}

	// 复制 Authorization header
	if auth := c.GetHeader("Authorization"); auth != "" {
		req.Header.Set("Authorization", auth)
	}

	// 使用 streaming client
	client := &http.Client{
		Timeout: 0, // 无超时，长连接
	}

	resp, err := client.Do(req)
	if err != nil {
		response.Fail(c, errcode.Internal.WithMsg(fmt.Sprintf("registrar SSE 服务不可达: %v", err)))
		return
	}
	defer resp.Body.Close()

	// 设置 SSE headers
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Transfer-Encoding", "chunked")

	// 流式传输
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		response.Fail(c, errcode.Internal.WithMsg("不支持 SSE streaming"))
		return
	}

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			break
		}

		c.Writer.Write(line)
		flusher.Flush()

		// 检查客户端是否断开
		if c.Request.Context().Done() != nil {
			select {
			case <-c.Request.Context().Done():
				return
			default:
			}
		}
	}
}