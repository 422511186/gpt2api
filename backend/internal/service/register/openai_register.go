package register

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/publicsuffix"
)

const (
	authBase              = "https://auth.openai.com"
	platformBase          = "https://platform.openai.com"
	platformOAuthClientID = "app_2SKx67EdpoN0G6j64rFvigXD"
	platformOAuthRedirect = "https://platform.openai.com/auth/callback"
	platformOAuthAudience = "https://api.openai.com/v1"
	platformAuth0Client   = "eyJuYW1lIjoiYXV0aDAtc3BhLWpzIiwidmVyc2lvbiI6IjEuMjEuMCJ9"
)

var (
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36"
	defaultSecCHUA   = `"Google Chrome";v="145", "Not?A_Brand";v="8", "Chromium";v="145"`
)

// RegistrationResult contains the result of a successful registration
type RegistrationResult struct {
	Email        string `json:"email"`
	Password     string `json:"password"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	SessionToken string `json:"session_token,omitempty"`
	CreatedAt    string `json:"created_at"`
}

// PlatformRegistrar handles the OpenAI registration flow
type PlatformRegistrar struct {
	client    *http.Client
	utlsConn  *utls.UConn
	deviceID  string
	proxyURL  string
	mailCfg   *MailConfig
	userAgent string
	secCHUA   string

	// PKCE parameters
	codeVerifier  string
	codeChallenge string

	// State
	codeVerifierMap map[string]string
}

// NewPlatformRegistrar creates a new registrar
func NewPlatformRegistrar(proxyURL string, mailCfg *MailConfig) *PlatformRegistrar {
	userAgent := defaultUserAgent
	secCHUA := defaultSecCHUA

	if mailCfg != nil && mailCfg.UserAgent != "" {
		userAgent = mailCfg.UserAgent
	}

	return &PlatformRegistrar{
		deviceID:        uuid.New().String(),
		proxyURL:        proxyURL,
		mailCfg:         mailCfg,
		userAgent:       userAgent,
		secCHUA:         secCHUA,
		codeVerifierMap: make(map[string]string),
	}
}

// Register performs the complete registration flow
func (r *PlatformRegistrar) Register(ctx context.Context, index int, log func(string, string)) (*RegistrationResult, error) {
	if log == nil {
		log = func(string, string) {}
	}

	// Initialize HTTP client
	if err := r.initClient(); err != nil {
		return nil, fmt.Errorf("init client: %w", err)
	}
	defer r.Close()

	// 1. Create mailbox
	log(fmt.Sprintf("[任务%d] 开始创建邮箱", index), "info")
	mailbox, err := r.createMailbox(ctx)
	if err != nil {
		return nil, fmt.Errorf("create mailbox: %w", err)
	}
	email := mailbox.Address
	log(fmt.Sprintf("[任务%d] 邮箱创建完成: %s", index, email), "info")

	password := generateRandomPassword()

	// 2. Platform authorize
	log(fmt.Sprintf("[任务%d] 开始 platform authorize", index), "info")
	if err := r.platformAuthorize(ctx, email); err != nil {
		return nil, fmt.Errorf("platform authorize: %w", err)
	}
	log(fmt.Sprintf("[任务%d] platform authorize 完成", index), "info")

	// 3. Register user
	log(fmt.Sprintf("[任务%d] 开始提交注册密码", index), "info")
	if err := r.registerUser(ctx, email, password); err != nil {
		return nil, fmt.Errorf("register user: %w", err)
	}
	log(fmt.Sprintf("[任务%d] 提交注册密码完成", index), "info")

	// 4. Send OTP
	log(fmt.Sprintf("[任务%d] 开始发送验证码", index), "info")
	if err := r.sendOTP(ctx); err != nil {
		return nil, fmt.Errorf("send otp: %w", err)
	}
	log(fmt.Sprintf("[任务%d] 发送验证码完成", index), "info")

	// 5. Wait for verification code
	log(fmt.Sprintf("[任务%d] 开始等待注册验证码", index), "info")
	timeout := 30 * time.Second
	if r.mailCfg != nil && r.mailCfg.WaitTimeout > 0 {
		timeout = time.Duration(r.mailCfg.WaitTimeout) * time.Second
	}

	provider := r.getMailProvider()
	if provider == nil {
		return nil, fmt.Errorf("no mail provider available")
	}

	code, err := provider.WaitForCode(ctx, mailbox, timeout)
	if err != nil {
		return nil, fmt.Errorf("wait for code: %w", err)
	}
	if code == "" {
		return nil, fmt.Errorf("wait for verification code timeout")
	}
	log(fmt.Sprintf("[任务%d] 收到注册验证码: %s", index, code), "info")

	// 6. Validate OTP
	log(fmt.Sprintf("[任务%d] 开始校验验证码 %s", index, code), "info")
	if err := r.validateOTP(ctx, code); err != nil {
		return nil, fmt.Errorf("validate otp: %w", err)
	}
	log(fmt.Sprintf("[任务%d] 验证码校验完成", index), "info")

	// 7. Create account profile
	firstName, lastName := generateRandomName()
	name := fmt.Sprintf("%s %s", firstName, lastName)
	birthdate := generateRandomBirthdate()
	log(fmt.Sprintf("[任务%d] 开始创建账号资料", index), "info")
	if err := r.createAccount(ctx, name, birthdate); err != nil {
		return nil, fmt.Errorf("create account: %w", err)
	}
	log(fmt.Sprintf("[任务%d] 创建账号资料完成", index), "info")

	// 8. Login and exchange tokens
	log(fmt.Sprintf("[任务%d] 开始独立登录换 token", index), "info")
	tokens, err := r.loginAndExchangeTokens(ctx, email, password, mailbox)
	if err != nil {
		return nil, fmt.Errorf("exchange tokens: %w", err)
	}
	log(fmt.Sprintf("[任务%d] token 换取完成", index), "info")

	return &RegistrationResult{
		Email:        email,
		Password:     password,
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		IDToken:      tokens.IDToken,
		SessionToken: tokens.SessionToken,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (r *PlatformRegistrar) initClient() error {
	jar, err := cookiejar.New(&cookiejar.Options{
		PublicSuffixList: publicsuffix.List,
	})
	if err != nil {
		return err
	}

	r.client = &http.Client{
		Jar:     jar,
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
			DisableCompression: false,
		},
	}

	// Set cookies
	r.client.Jar.SetCookies(&url.URL{Scheme: "https", Host: "auth.openai.com"}, []*http.Cookie{
		{Name: "oai-did", Value: r.deviceID},
	})

	return nil
}

func (r *PlatformRegistrar) Close() {
	if r.client != nil {
		r.client.CloseIdleConnections()
	}
	if r.utlsConn != nil {
		r.utlsConn.Close()
	}
}

func (r *PlatformRegistrar) createMailbox(ctx context.Context) (*Mailbox, error) {
	provider := r.getMailProvider()
	if provider == nil {
		return nil, fmt.Errorf("no mail provider configured")
	}
	return provider.CreateMailbox(ctx, "")
}

func (r *PlatformRegistrar) getMailProvider() MailProvider {
	if r.mailCfg == nil || len(r.mailCfg.Providers) == 0 {
		return nil
	}

	// Find first enabled provider
	for _, cfg := range r.mailCfg.Providers {
		if !cfg.Enable {
			continue
		}
		switch cfg.Type {
		case "tempmail_lol":
			return NewTempMailLolProvider(cfg, *r.mailCfg)
		// Add more providers here as they are implemented
		}
	}

	return nil
}

func (r *PlatformRegistrar) platformAuthorize(ctx context.Context, email string) error {
	codeVerifier, codeChallenge := generatePKCE()
	r.codeVerifier = codeVerifier
	r.codeChallenge = codeChallenge

	params := url.Values{
		"issuer":             {authBase},
		"client_id":          {platformOAuthClientID},
		"audience":           {platformOAuthAudience},
		"redirect_uri":       {platformOAuthRedirect},
		"device_id":          {r.deviceID},
		"screen_hint":        {"login_or_signup"},
		"max_age":            {"0"},
		"login_hint":         {email},
		"scope":              {"openid profile email offline_access"},
		"response_type":      {"code"},
		"response_mode":      {"query"},
		"state":              {uuid.New().String()},
		"nonce":              {uuid.New().String()},
		"code_challenge":     {codeChallenge},
		"code_challenge_method": {"S256"},
		"auth0Client":        {platformAuth0Client},
	}

	reqURL := fmt.Sprintf("%s/api/accounts/authorize?%s", authBase, params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return err
	}

	r.setNavigateHeaders(req, platformBase+"/")

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("authorize failed: HTTP %d, body: %s", resp.StatusCode, string(body)[:min(300, len(body))])
	}

	return nil
}

func (r *PlatformRegistrar) registerUser(ctx context.Context, email, password string) error {
	body := map[string]string{
		"username": email,
		"password": password,
	}
	bodyBytes, _ := json.Marshal(body)

	reqURL := fmt.Sprintf("%s/api/accounts/user/register", authBase)
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}

	r.setJSONHeaders(req, authBase+"/create-account/password")

	// Add sentinel token
	sentinel := NewSentinelTokenGenerator(r.deviceID, r.userAgent)
	sentinelToken, err := sentinel.BuildSentinelToken(r, "username_password_create")
	if err != nil {
		// Continue without sentinel token
		sentinelToken = ""
	}
	if sentinelToken != "" {
		req.Header.Set("openai-sentinel-token", sentinelToken)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		var errData struct {
			Message string `json:"message"`
		}
		json.Unmarshal(respBody, &errData)
		if errData.Message == "Failed to create account. Please try again." {
			return fmt.Errorf("registration failed: email domain likely blocked")
		}
		return fmt.Errorf("register user failed: HTTP %d, body: %s", resp.StatusCode, string(respBody)[:min(500, len(respBody))])
	}

	return nil
}

func (r *PlatformRegistrar) sendOTP(ctx context.Context) error {
	reqURL := fmt.Sprintf("%s/api/accounts/email-otp/send", authBase)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return err
	}

	r.setNavigateHeaders(req, authBase+"/create-account/password")

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send otp failed: HTTP %d, body: %s", resp.StatusCode, string(body)[:min(300, len(body))])
	}

	return nil
}

func (r *PlatformRegistrar) validateOTP(ctx context.Context, code string) error {
	body := map[string]string{"code": code}
	bodyBytes, _ := json.Marshal(body)

	reqURL := fmt.Sprintf("%s/api/accounts/email-otp/validate", authBase)
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}

	r.setJSONHeaders(req, authBase+"/email-verification")
	req.Header.Set("oai-device-id", r.deviceID)
	r.addTraceHeaders(req)

	// Try with sentinel token first
	sentinel := NewSentinelTokenGenerator(r.deviceID, r.userAgent)
	sentinelToken, _ := sentinel.BuildSentinelToken(r, "authorize_continue")
	if sentinelToken != "" {
		req.Header.Set("openai-sentinel-token", sentinelToken)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	// Retry with new sentinel token
	req2, _ := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(bodyBytes))
	r.setJSONHeaders(req2, authBase+"/email-verification")
	req2.Header.Set("oai-device-id", r.deviceID)
	r.addTraceHeaders(req2)

	sentinelToken2, err := sentinel.BuildSentinelToken(r, "authorize_continue")
	if err != nil {
		return fmt.Errorf("validate otp failed: %w", err)
	}
	req2.Header.Set("openai-sentinel-token", sentinelToken2)

	resp2, err := r.client.Do(req2)
	if err != nil {
		return err
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		body2, _ := io.ReadAll(resp2.Body)
		return fmt.Errorf("validate otp failed: HTTP %d, body: %s", resp2.StatusCode, string(body2)[:min(300, len(body2))])
	}

	return nil
}

func (r *PlatformRegistrar) createAccount(ctx context.Context, name, birthdate string) error {
	body := map[string]string{
		"name":      name,
		"birthdate": birthdate,
	}
	bodyBytes, _ := json.Marshal(body)

	reqURL := fmt.Sprintf("%s/api/accounts/create_account", authBase)
	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}

	r.setJSONHeaders(req, authBase+"/about-you")

	sentinel := NewSentinelTokenGenerator(r.deviceID, r.userAgent)
	sentinelToken, _ := sentinel.BuildSentinelToken(r, "oauth_create_account")
	if sentinelToken != "" {
		req.Header.Set("openai-sentinel-token", sentinelToken)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create account failed: HTTP %d, body: %s", resp.StatusCode, string(body)[:min(500, len(body))])
	}

	return nil
}

type tokenResult struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	SessionToken string `json:"session_token,omitempty"`
}

func (r *PlatformRegistrar) loginAndExchangeTokens(ctx context.Context, email, password string, mailbox *Mailbox) (*tokenResult, error) {
	// Create a new session for login
	loginDeviceID := uuid.New().String()
	codeVerifier, codeChallenge := generatePKCE()

	// Create new client for login
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	loginClient := &http.Client{
		Jar:     jar,
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	loginClient.Jar.SetCookies(&url.URL{Scheme: "https", Host: "auth.openai.com"}, []*http.Cookie{
		{Name: "oai-did", Value: loginDeviceID},
	})
	defer loginClient.CloseIdleConnections()

	// Step 1: Authorize
	params := url.Values{
		"issuer":                 {authBase},
		"client_id":              {platformOAuthClientID},
		"audience":               {platformOAuthAudience},
		"redirect_uri":           {platformOAuthRedirect},
		"device_id":              {loginDeviceID},
		"screen_hint":            {"login_or_signup"},
		"max_age":                {"0"},
		"login_hint":             {email},
		"scope":                  {"openid profile email offline_access"},
		"response_type":          {"code"},
		"response_mode":          {"query"},
		"state":                  {uuid.New().String()},
		"nonce":                  {uuid.New().String()},
		"code_challenge":         {codeChallenge},
		"code_challenge_method":  {"S256"},
		"auth0Client":            {platformAuth0Client},
	}

	reqURL := fmt.Sprintf("%s/api/accounts/authorize?%s", authBase, params.Encode())
	req, _ := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	r.setNavigateHeadersWithDevice(req, platformBase+"/", loginDeviceID)

	resp, err := loginClient.Do(req)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()

	// Step 2: Submit email
	emailBody := map[string]map[string]string{
		"username": {"kind": "email", "value": email},
	}
	emailBytes, _ := json.Marshal(emailBody)

	req, _ = http.NewRequestWithContext(ctx, "POST", authBase+"/api/accounts/authorize/continue", bytes.NewReader(emailBytes))
	r.setJSONHeadersWithDevice(req, authBase+"/log-in?usernameKind=email", loginDeviceID)

	sentinel := NewSentinelTokenGenerator(loginDeviceID, r.userAgent)
	sentinelToken, _ := sentinel.BuildSentinelToken(r, "authorize_continue")
	if sentinelToken != "" {
		req.Header.Set("openai-sentinel-token", sentinelToken)
	}

	resp, err = loginClient.Do(req)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()

	// Step 3: Submit password
	pwBody := map[string]string{"password": password}
	pwBytes, _ := json.Marshal(pwBody)

	req, _ = http.NewRequestWithContext(ctx, "POST", authBase+"/api/accounts/password/verify", bytes.NewReader(pwBytes))
	r.setJSONHeadersWithDevice(req, authBase+"/log-in/password", loginDeviceID)

	sentinelToken, _ = sentinel.BuildSentinelToken(r, "password_verify")
	if sentinelToken != "" {
		req.Header.Set("openai-sentinel-token", sentinelToken)
	}

	resp, err = loginClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pwResp struct {
		ContinueURL string `json:"continue_url"`
		Page        struct {
			Type string `json:"type"`
		} `json:"page"`
	}
	respBody, _ := io.ReadAll(resp.Body)
	json.Unmarshal(respBody, &pwResp)

	continueURL := pwResp.ContinueURL

	// Check if email OTP is required
	if pwResp.Page.Type == "email_otp_verification" || strings.Contains(continueURL, "email-verification") {
		// Wait for login OTP
		provider := r.getMailProvider()
		if provider == nil {
			return nil, fmt.Errorf("no mail provider for login OTP")
		}

		timeout := 30 * time.Second
		if r.mailCfg != nil && r.mailCfg.WaitTimeout > 0 {
			timeout = time.Duration(r.mailCfg.WaitTimeout) * time.Second
		}

		loginCode, err := provider.WaitForCode(ctx, mailbox, timeout)
		if err != nil {
			return nil, fmt.Errorf("wait for login code: %w", err)
		}
		if loginCode == "" {
			return nil, fmt.Errorf("login OTP timeout")
		}

		// Validate login OTP
		otpBody := map[string]string{"code": loginCode}
		otpBytes, _ := json.Marshal(otpBody)

		req, _ = http.NewRequestWithContext(ctx, "POST", authBase+"/api/accounts/email-otp/validate", bytes.NewReader(otpBytes))
		r.setJSONHeadersWithDevice(req, authBase+"/email-verification", loginDeviceID)

		resp, err = loginClient.Do(req)
		if err != nil {
			return nil, err
		}
		resp.Body.Close()
	}

	if continueURL == "" {
		continueURL = authBase + "/sign-in-with-chatgpt/codex/consent"
	}

	// Step 4: Exchange tokens
	return r.exchangeTokens(ctx, loginClient, loginDeviceID, codeVerifier, continueURL)
}

func (r *PlatformRegistrar) exchangeTokens(ctx context.Context, client *http.Client, deviceID, codeVerifier, consentURL string) (*tokenResult, error) {
	// Navigate consent URL to get OAuth code
	req, _ := http.NewRequestWithContext(ctx, "GET", consentURL, nil)
	r.setNavigateHeadersWithDevice(req, consentURL, deviceID)

	// Follow redirects manually to capture the code
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	resp, err := client.Do(req)
	if err != nil && !strings.Contains(err.Error(), "stopped after") {
		return nil, err
	}
	if resp != nil {
		defer resp.Body.Close()
	}

	// Extract code from Location header or final URL
	var code string
	for i := 0; i < 10; i++ {
		location := resp.Header.Get("Location")
		if location == "" {
			break
		}

		if strings.Contains(location, "code=") {
			u, err := url.Parse(location)
			if err == nil {
				code = u.Query().Get("code")
				if code != "" {
					break
				}
			}
		}

		if !strings.HasPrefix(location, "http") {
			location = authBase + location
		}

		req, _ = http.NewRequestWithContext(ctx, "GET", location, nil)
		r.setNavigateHeadersWithDevice(req, consentURL, deviceID)
		resp, err = client.Do(req)
		if err != nil {
			break
		}
		resp.Body.Close()
	}

	if code == "" {
		return nil, fmt.Errorf("failed to extract OAuth code")
	}

	// Exchange code for tokens
	tokenReq := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {platformOAuthRedirect},
		"client_id":     {platformOAuthClientID},
		"code_verifier": {codeVerifier},
	}

	req, _ = http.NewRequestWithContext(ctx, "POST", authBase+"/oauth/token", strings.NewReader(tokenReq.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err = client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token exchange failed: HTTP %d, body: %s", resp.StatusCode, string(body)[:min(300, len(body))])
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	// Extract session token from cookies
	var sessionToken string
	for _, cookie := range client.Jar.Cookies(&url.URL{Scheme: "https", Host: "chatgpt.com"}) {
		if cookie.Name == "__Secure-next-auth.session-token" {
			sessionToken = cookie.Value
			break
		}
	}

	return &tokenResult{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		IDToken:      tokenResp.IDToken,
		SessionToken: sessionToken,
	}, nil
}

// HTTPSession implementation for sentinel
func (r *PlatformRegistrar) Post(url string, body []byte, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

func (r *PlatformRegistrar) setNavigateHeaders(req *http.Request, referer string) {
	r.setNavigateHeadersWithDevice(req, referer, r.deviceID)
}

func (r *PlatformRegistrar) setNavigateHeadersWithDevice(req *http.Request, referer, deviceID string) {
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("User-Agent", r.userAgent)
	req.Header.Set("sec-ch-ua", r.secCHUA)
	req.Header.Set("sec-ch-ua-arch", `"x86_64"`)
	req.Header.Set("sec-ch-ua-bitness", `"64"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"Windows"`)
	req.Header.Set("sec-fetch-dest", "document")
	req.Header.Set("sec-fetch-mode", "navigate")
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("sec-fetch-user", "?1")
	req.Header.Set("upgrade-insecure-requests", "1")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
}

func (r *PlatformRegistrar) setJSONHeaders(req *http.Request, referer string) {
	r.setJSONHeadersWithDevice(req, referer, r.deviceID)
}

func (r *PlatformRegistrar) setJSONHeadersWithDevice(req *http.Request, referer, deviceID string) {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", authBase)
	req.Header.Set("Priority", "u=1, i")
	req.Header.Set("User-Agent", r.userAgent)
	req.Header.Set("sec-ch-ua", r.secCHUA)
	req.Header.Set("sec-ch-ua-arch", `"x86_64"`)
	req.Header.Set("sec-ch-ua-bitness", `"64"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"Windows"`)
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-origin")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	req.Header.Set("oai-device-id", deviceID)
	r.addTraceHeaders(req)
}

func (r *PlatformRegistrar) addTraceHeaders(req *http.Request) {
	traceIDBig, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	parentIDBig, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	traceID := fmt.Sprintf("%016x", traceIDBig.Int64())
	parentID := fmt.Sprintf("%016x", parentIDBig.Int64())
	req.Header.Set("traceparent", fmt.Sprintf("00-%s-%s-01", uuid.New().String(), parentID))
	req.Header.Set("tracestate", "dd=s:1;o:rum")
	req.Header.Set("x-datadog-origin", "rum")
	req.Header.Set("x-datadog-parent-id", parentID)
	req.Header.Set("x-datadog-sampling-priority", "1")
	req.Header.Set("x-datadog-trace-id", traceID)
}

// Helper functions

func generatePKCE() (string, string) {
	verifier := make([]byte, 64)
	rand.Read(verifier)
	codeVerifier := base64.RawURLEncoding.EncodeToString(verifier)

	hash := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(hash[:])

	return codeVerifier, codeChallenge
}

func generateRandomPassword() string {
	const (
		upper    = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
		lower    = "abcdefghijklmnopqrstuvwxyz"
		digits   = "0123456789"
		special  = "!@#$%"
		allChars = upper + lower + digits + special
	)

	b := make([]byte, 0, 16)

	// Ensure at least one of each required type
	b = append(b, upper[randIntn(len(upper))])
	b = append(b, lower[randIntn(len(lower))])
	b = append(b, digits[randIntn(len(digits))])
	b = append(b, special[randIntn(len(special))])

	// Fill the rest
	for i := 4; i < 16; i++ {
		b = append(b, allChars[randIntn(len(allChars))])
	}

	// Shuffle
	for i := len(b) - 1; i > 0; i-- {
		jBig, _ := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		j := int(jBig.Int64())
		b[i], b[j] = b[j], b[i]
	}

	return string(b)
}

func generateRandomName() (string, string) {
	firstNames := []string{"James", "Robert", "John", "Michael", "David", "Mary", "Emma", "Olivia"}
	lastNames := []string{"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller"}

	return firstNames[randIntn(len(firstNames))], lastNames[randIntn(len(lastNames))]
}

func generateRandomBirthdate() string {
	year := 1996 + randIntn(10) // 1996-2005
	month := 1 + randIntn(12)
	day := 1 + randIntn(28)
	return fmt.Sprintf("%04d-%02d-%02d", year, month, day)
}

func randIntn(n int) int {
	if n <= 0 {
		return 0
	}
	i, _ := rand.Int(rand.Reader, big.NewInt(int64(n)))
	return int(i.Int64())
}