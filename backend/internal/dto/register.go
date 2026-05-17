package dto

// RegisterConfigReq is the request for updating register config
type RegisterConfigReq struct {
	Mail            *MailConfigReq           `json:"mail,omitempty"`
	Proxy           string                   `json:"proxy,omitempty"`
	Total           int                      `json:"total,omitempty"`
	Threads         int                      `json:"threads,omitempty"`
	Mode            string                   `json:"mode,omitempty"`
	TargetQuota     int                      `json:"target_quota,omitempty"`
	TargetAvailable int                      `json:"target_available,omitempty"`
	CheckInterval   int                      `json:"check_interval,omitempty"`
}

// MailConfigReq is the request for mail configuration
type MailConfigReq struct {
	RequestTimeout float64                   `json:"request_timeout,omitempty"`
	WaitTimeout    float64                   `json:"wait_timeout,omitempty"`
	WaitInterval   float64                   `json:"wait_interval,omitempty"`
	UserAgent      string                    `json:"user_agent,omitempty"`
	Proxy          string                    `json:"proxy,omitempty"`
	Providers      []MailProviderConfigReq   `json:"providers,omitempty"`
}

// MailProviderConfigReq is the request for mail provider configuration
type MailProviderConfigReq struct {
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