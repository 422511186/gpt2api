package register

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	maxSentinelAttempts   = 500000
	sentinelErrorPrefix   = "wQ8Lk5FbGpA2NcR9dShT6gYjU7VxZ4D"
	sentinelAPIURL        = "https://sentinel.openai.com/backend-api/sentinel/req"
	sentinelFrameURL      = "https://sentinel.openai.com/backend-api/sentinel/frame.html"
)

// SentinelTokenGenerator generates OpenAI sentinel tokens (PoW)
type SentinelTokenGenerator struct {
	deviceID  string
	userAgent string
	sid       string
}

// NewSentinelTokenGenerator creates a new sentinel token generator
func NewSentinelTokenGenerator(deviceID, userAgent string) *SentinelTokenGenerator {
	return &SentinelTokenGenerator{
		deviceID:  deviceID,
		userAgent: userAgent,
		sid:       uuid.New().String(),
	}
}

// GenerateRequirementsToken generates a requirements token
func (g *SentinelTokenGenerator) GenerateRequirementsToken() string {
	config := g.getConfig()
	config[3] = 1
	config[9] = randInt(5, 50)
	return "gAAAAAC" + g.b64Encode(config)
}

// GenerateToken generates a PoW token by finding a nonce that satisfies the difficulty
func (g *SentinelTokenGenerator) GenerateToken(seed, difficulty string) string {
	start := time.Now()
	config := g.getConfig()
	difficultyStr := strings.TrimSpace(difficulty)
	if difficultyStr == "" {
		difficultyStr = "0"
	}

	for i := 0; i < maxSentinelAttempts; i++ {
		config[3] = i
		config[9] = int(time.Since(start).Milliseconds())
		payload := g.b64Encode(config)

		hash := g.fnv1a32(seed + payload)
		hashStr := fmt.Sprintf("%08x", hash)

		// Check if hash prefix satisfies difficulty
		if len(hashStr) >= len(difficultyStr) && hashStr[:len(difficultyStr)] <= difficultyStr {
			return "gAAAAAB" + payload + "~S"
		}
	}

	// Fallback: return error token
	return "gAAAAAB" + sentinelErrorPrefix + g.b64Encode([]interface{}{nil})
}

// BuildSentinelToken builds a complete sentinel token by calling the sentinel API
func (g *SentinelTokenGenerator) BuildSentinelToken(session HTTPSession, flow string) (string, error) {
	reqToken := g.GenerateRequirementsToken()

	body := map[string]string{
		"p":   reqToken,
		"id":  g.deviceID,
		"flow": flow,
	}
	bodyBytes, _ := json.Marshal(body)

	headers := map[string]string{
		"Content-Type": "text/plain;charset=UTF-8",
		"Referer":      sentinelFrameURL,
		"Origin":       "https://sentinel.openai.com",
		"User-Agent":   g.userAgent,
	}

	resp, err := session.Post(sentinelAPIURL, bodyBytes, headers)
	if err != nil {
		return "", fmt.Errorf("sentinel request failed: %w", err)
	}

	var data struct {
		Token       string `json:"token"`
		ProofOfWork struct {
			Required  bool   `json:"required"`
			Seed      string `json:"seed"`
			Difficulty string `json:"difficulty"`
		} `json:"proofofwork"`
	}

	if err := json.Unmarshal(resp, &data); err != nil {
		return "", fmt.Errorf("parse sentinel response: %w", err)
	}

	if data.Token == "" {
		return "", fmt.Errorf("sentinel response missing token")
	}

	// Generate PoW token if required
	var pToken string
	if data.ProofOfWork.Required && data.ProofOfWork.Seed != "" {
		pToken = g.GenerateToken(data.ProofOfWork.Seed, data.ProofOfWork.Difficulty)
	} else {
		pToken = g.GenerateRequirementsToken()
	}

	result := map[string]string{
		"p":   pToken,
		"t":   "",
		"c":   data.Token,
		"id":  g.deviceID,
		"flow": flow,
	}

	resultBytes, _ := json.Marshal(result)
	return string(resultBytes), nil
}

func (g *SentinelTokenGenerator) getConfig() []interface{} {
	perfNow := rand.Float64() * 50000

	return []interface{}{
		"1920x1080",
		time.Now().UTC().Format("Mon Jan 02 2006 15:04:05 GMT+0000 (Coordinated Universal Time)"),
		4294705152,
		rand.Float64(),
		g.userAgent,
		"https://sentinel.openai.com/sentinel/20260124ceb8/sdk.js",
		nil,
		nil,
		"en-US",
		rand.Float64(),
		randomChoice([]string{"vendorSub-undefined", "plugins-undefined", "mimeTypes-undefined", "hardwareConcurrency-undefined"}),
		randomChoice([]string{"location", "implementation", "URL", "documentURI", "compatMode"}),
		randomChoice([]string{"Object", "Function", "Array", "Number", "parseFloat", "undefined"}),
		perfNow,
		g.sid,
		"",
		randomChoice([]int{4, 8, 12, 16}),
		time.Now().UnixMilli() - int64(perfNow),
	}
}

func (g *SentinelTokenGenerator) b64Encode(data []interface{}) string {
	jsonBytes, _ := json.Marshal(data)
	return base64.StdEncoding.EncodeToString(jsonBytes)
}

// fnv1a32 computes FNV-1a 32-bit hash
func (g *SentinelTokenGenerator) fnv1a32(text string) uint32 {
	h := uint32(2166136261)
	for _, ch := range text {
		h ^= uint32(ch)
		h *= 16777619
	}

	// Final mix
	h ^= h >> 16
	h *= 2246822507
	h ^= h >> 13
	h *= 3266489909
	h ^= h >> 16

	return h
}

func randInt(min, max int) int {
	return min + rand.Intn(max - min + 1)
}

func randomChoice[T any](items []T) T {
	if len(items) == 0 {
		var zero T
		return zero
	}
	return items[rand.Intn(len(items))]
}

// HTTPSession is an interface for HTTP operations needed by sentinel
type HTTPSession interface {
	Post(url string, body []byte, headers map[string]string) ([]byte, error)
}