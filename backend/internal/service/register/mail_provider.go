package register

import (
	"context"
	"regexp"
	"time"
)

// Mailbox represents a created temporary mailbox
type Mailbox struct {
	Provider    string         `json:"provider"`
	ProviderRef string         `json:"provider_ref"`
	Address     string         `json:"address"`
	Token       string         `json:"token,omitempty"`
	EmailID     string         `json:"email_id,omitempty"`
	Extra       map[string]any `json:"extra,omitempty"`

	// Internal state for tracking seen verification codes
	seenCodeRefs map[string]bool
}

// Message represents an email message
type Message struct {
	Provider    string     `json:"provider"`
	Mailbox     string     `json:"mailbox"`
	MessageID   string     `json:"message_id"`
	Subject     string     `json:"subject"`
	Sender      string     `json:"sender"`
	TextContent string     `json:"text_content"`
	HTMLContent string     `json:"html_content"`
	ReceivedAt  *time.Time `json:"received_at,omitempty"`
	Raw         any        `json:"raw,omitempty"`
}

// MailProvider is the interface for email providers
type MailProvider interface {
	// Name returns the provider name
	Name() string

	// CreateMailbox creates a new temporary mailbox
	CreateMailbox(ctx context.Context, username string) (*Mailbox, error)

	// FetchLatestMessage fetches the latest message from the mailbox
	FetchLatestMessage(ctx context.Context, mailbox *Mailbox) (*Message, error)

	// WaitForCode waits for a verification code to arrive
	WaitForCode(ctx context.Context, mailbox *Mailbox, timeout time.Duration) (string, error)

	// Close releases resources
	Close() error
}

// MailProviderConfig is the configuration for a mail provider
type MailProviderConfig struct {
	Type            string   `json:"type"`
	Enable          bool     `json:"enable"`
	APIBase         string   `json:"api_base,omitempty"`
	APIKey          string   `json:"api_key,omitempty"`
	AdminPassword   string   `json:"admin_password,omitempty"`
	Domain          []string `json:"domain,omitempty"`
	DefaultDomain   string   `json:"default_domain,omitempty"`
	Subdomain       string   `json:"subdomain,omitempty"`
	Wildcard        bool     `json:"wildcard,omitempty"`
	RandomSubdomain bool     `json:"random_subdomain,omitempty"`
	DDGToken        string   `json:"ddg_token,omitempty"`
	CFInboxJWT      string   `json:"cf_inbox_jwt,omitempty"`
	CFAPIBase       string   `json:"cf_api_base,omitempty"`
	CFAPIKey        string   `json:"cf_api_key,omitempty"`
	CFAuthMode      string   `json:"cf_auth_mode,omitempty"`
	CFDomain        []string `json:"cf_domain,omitempty"`
	CFCreatePath    string   `json:"cf_create_path,omitempty"`
	CFMessagesPath  string   `json:"cf_messages_path,omitempty"`
	ExpiryTime      int      `json:"expiry_time,omitempty"`
}

// MailConfig is the global mail configuration
type MailConfig struct {
	RequestTimeout float64              `json:"request_timeout"`
	WaitTimeout    float64              `json:"wait_timeout"`
	WaitInterval   float64              `json:"wait_interval"`
	UserAgent      string               `json:"user_agent"`
	Proxy          string               `json:"proxy"`
	Providers      []MailProviderConfig `json:"providers"`
}

// DefaultMailConfig returns the default mail configuration
func DefaultMailConfig() MailConfig {
	return MailConfig{
		RequestTimeout: 30,
		WaitTimeout:    30,
		WaitInterval:   2,
		UserAgent:      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36",
		Providers:      []MailProviderConfig{},
	}
}

// ExtractCode extracts a 6-digit verification code from message content
func ExtractCode(message *Message) string {
	if message == nil {
		return ""
	}

	content := message.Subject + "\n" + message.TextContent + "\n" + message.HTMLContent
	if content == "" {
		return ""
	}

	// Try to find code in OpenAI email format (background-color: #F3F3F3)
	bgColorPattern := regexp.MustCompile(`background-color:\s*#F3F3F3[^>]*>[\s\S]*?(\d{6})[\s\S]*?</p>`)
	if match := bgColorPattern.FindStringSubmatch(content); match != nil {
		return match[1]
	}

	// Try common patterns
	codePatterns := []string{
		`(?:Verification code|code is|代码为|验证码)[:\s]*(\d{6})`,
		`>\s*(\d{6})\s*<`,
		`(?<![#&])\b(\d{6})\b`,
	}

	for _, pattern := range codePatterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindAllStringSubmatch(content, -1)
		for _, match := range matches {
			if len(match) > 1 {
				code := match[1]
				if len(match) > 2 && match[2] != "" {
					code = match[2]
				}
				// Skip common false positives
				if code != "177010" {
					return code
				}
			}
		}
	}

	return ""
}

// BaseMailProvider provides common functionality for mail providers
type BaseMailProvider struct {
	conf MailConfig
}

// WaitForCode implements the default wait for code logic
func (p *BaseMailProvider) WaitForCode(
	ctx context.Context,
	mailbox *Mailbox,
	fetchMessage func(ctx context.Context, mailbox *Mailbox) (*Message, error),
	timeout time.Duration,
) (string, error) {
	if mailbox.seenCodeRefs == nil {
		mailbox.seenCodeRefs = make(map[string]bool)
	}

	deadline := time.Now().Add(timeout)
	waitInterval := time.Duration(p.conf.WaitInterval) * time.Second
	if waitInterval < 200*time.Millisecond {
		waitInterval = 2 * time.Second
	}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		message, err := fetchMessage(ctx, mailbox)
		if err != nil {
			// Log error but continue waiting - transient errors like rate limiting shouldn't fail immediately
			// Only fail on context cancellation or deadline exceeded
			time.Sleep(waitInterval)
			continue
		}

		if message != nil {
			ref := messageTrackingRef(message)
			if !mailbox.seenCodeRefs[ref] {
				code := ExtractCode(message)
				if code != "" {
					mailbox.seenCodeRefs[ref] = true
					return code, nil
				}
			}
		}

		time.Sleep(waitInterval)
	}

	return "", nil
}

func messageTrackingRef(message *Message) string {
	if message.MessageID != "" {
		return "id:" + message.Provider + ":" + message.Mailbox + ":" + message.MessageID
	}

	receivedStr := ""
	if message.ReceivedAt != nil {
		receivedStr = message.ReceivedAt.Format(time.RFC3339)
	}

	return "content:" + message.Provider + ":" + message.Mailbox + ":" + receivedStr
}
