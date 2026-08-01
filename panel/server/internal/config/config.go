package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	ListenAddr         string
	PanelPassword      string
	JWTSecret          string
	DBPath             string
	StaticDir          string
	InsecureSkipVerify bool
	UTLSHello          string
	SessionTTL         time.Duration
}

func Load() Config {
	return Config{
		ListenAddr:         envOr("PANEL_LISTEN", ":3080"),
		PanelPassword:      envOr("PANEL_PASSWORD", "changeme"),
		JWTSecret:          envOr("PANEL_JWT_SECRET", "dev-jwt-secret-change-me"),
		DBPath:             envOr("PANEL_DB_PATH", "./data/panel.db"),
		StaticDir:          envOr("PANEL_STATIC_DIR", "./web/dist"),
		InsecureSkipVerify: envBool("HAPANEL_INSECURE_SKIP_VERIFY", true),
		// randomized works with HAProxy; chrome/firefox ClientHello often EOF.
		UTLSHello:  envOr("HAPANEL_UTLS_HELLO", "randomized"),
		SessionTTL: 24 * time.Hour,
	}
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
