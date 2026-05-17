package register

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

const (
	tempMailAPIBase = "https://api.tempmail.lol/v2"
)

// TempMailLolProvider implements MailProvider for tempmail.lol
type TempMailLolProvider struct {
	base    BaseMailProvider
	apiKey  string
	domain  []string
	client  *resty.Client
}

// NewTempMailLolProvider creates a new TempMail.lol provider
func NewTempMailLolProvider(cfg MailProviderConfig, mailCfg MailConfig) *TempMailLolProvider {
	p := &TempMailLolProvider{
		base:   BaseMailProvider{conf: mailCfg},
		apiKey: cfg.APIKey,
		domain: cfg.Domain,
	}

	p.client = resty.New().
		SetTimeout(time.Duration(mailCfg.RequestTimeout) * time.Second).
		SetHeader("User-Agent", mailCfg.UserAgent).
		SetHeader("Accept", "application/json").
		SetHeader("Content-Type", "application/json")

	if p.apiKey != "" {
		p.client.SetAuthToken(p.apiKey)
	}

	// Configure proxy if set
	if mailCfg.Proxy != "" {
		proxyURL, err := url.Parse(mailCfg.Proxy)
		if err == nil {
			p.client.SetProxy(proxyURL.String())
		}
	}

	return p
}

func (p *TempMailLolProvider) Name() string {
	return "tempmail_lol"
}

func (p *TempMailLolProvider) CreateMailbox(ctx context.Context, username string) (*Mailbox, error) {
	payload := make(map[string]any)

	if len(p.domain) > 0 {
		domain, forceRandomPrefix := p.resolveDomain(p.domain)
		payload["domain"] = domain
		if forceRandomPrefix {
			payload["prefix"] = randomMailboxName()
		}
	}

	if username != "" && payload["prefix"] == nil {
		payload["prefix"] = username
	}

	resp, err := p.client.R().
		SetContext(ctx).
		SetBody(payload).
		Post(tempMailAPIBase + "/inbox/create")

	if err != nil {
		return nil, fmt.Errorf("create mailbox request failed: %w", err)
	}

	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusCreated {
		return nil, fmt.Errorf("create mailbox failed: HTTP %d, body: %s", resp.StatusCode(), string(resp.Body())[:min(300, len(resp.Body()))])
	}

	var data struct {
		Address string `json:"address"`
		Token   string `json:"token"`
	}

	if err := json.Unmarshal(resp.Body(), &data); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if data.Address == "" || data.Token == "" {
		return nil, fmt.Errorf("response missing address or token")
	}

	return &Mailbox{
		Provider:    p.Name(),
		ProviderRef: p.Name(),
		Address:     data.Address,
		Token:       data.Token,
	}, nil
}

func (p *TempMailLolProvider) FetchLatestMessage(ctx context.Context, mailbox *Mailbox) (*Message, error) {
	resp, err := p.client.R().
		SetContext(ctx).
		SetQueryParam("token", mailbox.Token).
		Get(tempMailAPIBase + "/inbox")

	if err != nil {
		return nil, fmt.Errorf("fetch messages request failed: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("fetch messages failed: HTTP %d", resp.StatusCode())
	}

	var data struct {
		Emails []struct {
			ID        string `json:"id"`
			Subject   string `json:"subject"`
			From      string `json:"from"`
			FromAddr  string `json:"from_address"`
			Body      string `json:"body"`
			HTML      string `json:"html"`
			Text      string `json:"text"`
			CreatedAt int64  `json:"created_at"`
			Date      string `json:"date"`
			Timestamp int64  `json:"timestamp"`
		} `json:"emails"`
		Messages []struct {
			ID        string `json:"id"`
			Subject   string `json:"subject"`
			From      string `json:"from"`
			FromAddr  string `json:"from_address"`
			Body      string `json:"body"`
			HTML      string `json:"html"`
			Text      string `json:"text"`
			CreatedAt int64  `json:"created_at"`
			Date      string `json:"date"`
			Timestamp int64  `json:"timestamp"`
		} `json:"messages"`
	}

	if err := json.Unmarshal(resp.Body(), &data); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// Combine emails and messages
	items := append(data.Emails, data.Messages...)
	if len(items) == 0 {
		return nil, nil
	}

	// Find the most recent message
	var latest *struct {
		ID        string `json:"id"`
		Subject   string `json:"subject"`
		From      string `json:"from"`
		FromAddr  string `json:"from_address"`
		Body      string `json:"body"`
		HTML      string `json:"html"`
		Text      string `json:"text"`
		CreatedAt int64  `json:"created_at"`
		Date      string `json:"date"`
		Timestamp int64  `json:"timestamp"`
	}

	var latestTime time.Time
	for i := range items {
		item := &items[i]
		var t time.Time
		if item.CreatedAt > 0 {
			t = time.Unix(item.CreatedAt, 0)
		} else if item.Timestamp > 0 {
			t = time.Unix(item.Timestamp, 0)
		}
		if t.After(latestTime) {
			latestTime = t
			latest = item
		}
	}

	if latest == nil {
		latest = &items[0]
	}

	textContent := latest.Text
	if textContent == "" {
		textContent = latest.Body
	}

	return &Message{
		Provider:    p.Name(),
		Mailbox:     mailbox.Address,
		MessageID:   latest.ID,
		Subject:     latest.Subject,
		Sender:      latest.FromAddr,
		TextContent: textContent,
		HTMLContent: latest.HTML,
		ReceivedAt:  &latestTime,
	}, nil
}

func (p *TempMailLolProvider) WaitForCode(ctx context.Context, mailbox *Mailbox, timeout time.Duration) (string, error) {
	return p.base.WaitForCode(ctx, mailbox, p.FetchLatestMessage, timeout)
}

func (p *TempMailLolProvider) Close() error {
	return nil
}

func (p *TempMailLolProvider) resolveDomain(domains []string) (string, bool) {
	if len(domains) == 0 {
		return "", false
	}

	// Pick a random domain
	idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(domains))))
	domain := domains[idx.Int64()]

	// Check if it's a wildcard domain (*.example.com)
	if strings.HasPrefix(domain, "*.") && len(domain) > 2 {
		return randomSubdomainLabel() + "." + domain[2:], true
	}

	return domain, false
}

func randomMailboxName() string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	const digits = "0123456789"

	b := make([]byte, 0, 12)

	// 5 random letters
	for i := 0; i < 5; i++ {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		b = append(b, letters[idx.Int64()])
	}

	// 1-3 random digits
	numDigits, _ := rand.Int(rand.Reader, big.NewInt(3))
	for i := 0; i < int(numDigits.Int64())+1; i++ {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(digits))))
		b = append(b, digits[idx.Int64()])
	}

	// 1-3 random letters
	numLetters, _ := rand.Int(rand.Reader, big.NewInt(3))
	for i := 0; i < int(numLetters.Int64())+1; i++ {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		b = append(b, letters[idx.Int64()])
	}

	return string(b)
}

func randomSubdomainLabel() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	length, _ := rand.Int(rand.Reader, big.NewInt(7)) // 4-10 chars
	length = length.Add(length, big.NewInt(4))

	b := make([]byte, length.Int64())
	for i := range b {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		b[i] = chars[idx.Int64()]
	}

	return string(b)
}

func randomTokenSuffix() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
