package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr         string
	PanelPassword      string
	JWTSecret          string
	SecretsKey         string // PANEL_SECRETS_KEY; empty → caller falls back to JWTSecret
	DBPath             string
	StaticDir          string
	BasePath           string // e.g. "/a1b2c3d4e5f6" — empty = root
	InsecureSkipVerify bool
	UTLSHello          string
	SessionTTL         time.Duration
	GeminiAPIKey       string
	GroqAPIKey         string
	LLMProvider        string // gemini|groq
	PanelIP            string // public IP of panel for UFW on nodes
	LLMHTTPProxy       string // socks5://user:pass@host:port for LLM APIs
}

func Load() Config {
	return Config{
		ListenAddr:         envOr("PANEL_LISTEN", ":3080"),
		PanelPassword:      envOr("PANEL_PASSWORD", "changeme"),
		JWTSecret:          envOr("PANEL_JWT_SECRET", "dev-jwt-secret-change-me"),
		SecretsKey:         os.Getenv("PANEL_SECRETS_KEY"),
		DBPath:             envOr("PANEL_DB_PATH", "./data/panel.db"),
		StaticDir:          envOr("PANEL_STATIC_DIR", "./web/dist"),
		BasePath:           normalizeBasePath(os.Getenv("PANEL_BASE_PATH")),
		InsecureSkipVerify: envBool("HAPANEL_INSECURE_SKIP_VERIFY", true),
		// randomized works with HAProxy; chrome/firefox ClientHello often EOF.
		UTLSHello:    envOr("HAPANEL_UTLS_HELLO", "randomized"),
		SessionTTL:   24 * time.Hour,
		GeminiAPIKey: strings.TrimSpace(os.Getenv("GEMINI_API_KEY")),
		GroqAPIKey:   strings.TrimSpace(os.Getenv("GROQ_API_KEY")),
		LLMProvider:  strings.ToLower(strings.TrimSpace(envOr("LLM_PROVIDER", "gemini"))),
		PanelIP:      strings.TrimSpace(os.Getenv("PANEL_IP")),
		LLMHTTPProxy: strings.TrimSpace(os.Getenv("LLM_HTTP_PROXY")),
	}
}

func normalizeBasePath(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || v == "/" {
		return ""
	}
	if !strings.HasPrefix(v, "/") {
		v = "/" + v
	}
	return strings.TrimRight(v, "/")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
