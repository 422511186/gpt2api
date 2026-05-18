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
	"sync/atomic"
	"time"

	"github.com/go-resty/resty/v2"
)

const (
	tempMailAPIBase = "https://api.tempmail.lol/v2"
)

// TempMailLolProvider implements MailProvider for tempmail.lol
type TempMailLolProvider struct {
	base    BaseMailProvider
	apiKeys []string
	keyIdx  atomic.Int64
	domain  []string
	client  *resty.Client
}

// NewTempMailLolProvider creates a new TempMail.lol provider
func NewTempMailLolProvider(cfg MailProviderConfig, mailCfg MailConfig) *TempMailLolProvider {
	p := &TempMailLolProvider{
		base:   BaseMailProvider{conf: mailCfg},
		domain: cfg.Domain,
	}

	// Split API keys by newline, filter empty
	for _, k := range strings.Split(cfg.APIKey, "\n") {
		k = strings.TrimSpace(k)
		if k != "" {
			p.apiKeys = append(p.apiKeys, k)
		}
	}

	p.client = resty.New().
		SetTimeout(time.Duration(mailCfg.RequestTimeout) * time.Second).
		SetHeader("User-Agent", mailCfg.UserAgent).
		SetHeader("Accept", "application/json").
		SetHeader("Content-Type", "application/json")

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

// nextKey returns the next API key in round-robin order.
// Returns empty string if no keys configured.
func (p *TempMailLolProvider) nextKey() string {
	if len(p.apiKeys) == 0 {
		return ""
	}
	idx := p.keyIdx.Add(1) - 1
	return p.apiKeys[idx%int64(len(p.apiKeys))]
}

// req creates a resty request with the next rotated API key.
func (p *TempMailLolProvider) req(ctx context.Context) *resty.Request {
	r := p.client.R().SetContext(ctx)
	if key := p.nextKey(); key != "" {
		r.SetAuthToken(key)
	}
	return r
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

	resp, err := p.req(ctx).
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
	resp, err := p.req(ctx).
		SetQueryParam("token", mailbox.Token).
		Get(tempMailAPIBase + "/inbox")

	if err != nil {
		return nil, fmt.Errorf("fetch messages request failed: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("fetch messages failed: HTTP %d", resp.StatusCode())
	}

	type emailItem struct {
		ID        string `json:"id"`
		Subject   string `json:"subject"`
		From      string `json:"from"`
		FromAddr  string `json:"from_address"`
		Body      string `json:"body"`
		HTML      string `json:"html"`
		Text      string `json:"text"`
		CreatedAt int64  `json:"created_at"`
		Date      any    `json:"date"`
		Timestamp int64  `json:"timestamp"`
	}

	var data struct {
		Emails   []emailItem `json:"emails"`
		Messages []emailItem `json:"messages"`
	}

	if err := json.Unmarshal(resp.Body(), &data); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// Combine emails and messages
	items := append(data.Emails, data.Messages...)
	if len(items) == 0 {
		return nil, nil
	}

	// Return the LAST message (most recent by list order)
	// The API likely returns messages in chronological order
	item := &items[len(items)-1]

	textContent := item.Text
	if textContent == "" {
		textContent = item.Body
	}

	// Parse timestamp - prefer created_at, then timestamp, then use current time as fallback
	var receivedAt time.Time
	if item.CreatedAt > 0 {
		receivedAt = time.Unix(item.CreatedAt, 0)
	} else if item.Timestamp > 0 {
		receivedAt = time.Unix(item.Timestamp, 0)
	} else {
		// If no timestamp, use current time as the message just arrived
		receivedAt = time.Now()
	}

	return &Message{
		Provider:    p.Name(),
		Mailbox:     mailbox.Address,
		MessageID:   item.ID,
		Subject:     item.Subject,
		Sender:      item.FromAddr,
		TextContent: textContent,
		HTMLContent: item.HTML,
		ReceivedAt:  &receivedAt,
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
