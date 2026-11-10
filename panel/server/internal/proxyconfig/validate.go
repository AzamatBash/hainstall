package proxyconfig

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxConfigBytes     = 1 << 20 // 1 MiB
	haproxyImage       = "haproxy:3.0-alpine"
	dockerValidateWait = 45 * time.Second
)

// Result is the outcome of config validation.
type Result struct {
	OK      bool   `json:"ok"`
	Engine  string `json:"engine"`
	Method  string `json:"method"` // structural | haproxy-c | envoy-structural
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// Validate checks a proxy profile body for the given engine.
func Validate(engine, body string) Result {
	engine = strings.ToLower(strings.TrimSpace(engine))
	if engine == "" {
		engine = "haproxy"
	}
	body = strings.ReplaceAll(body, "\r\n", "\n")
	if strings.TrimSpace(body) == "" {
		return Result{OK: false, Engine: engine, Method: "structural", Message: "конфиг пустой"}
	}
	if len(body) > maxConfigBytes {
		return Result{OK: false, Engine: engine, Method: "structural", Message: "конфиг слишком большой (>1 MiB)"}
	}
	if strings.ContainsRune(body, 0) {
		return Result{OK: false, Engine: engine, Method: "structural", Message: "в конфиге есть нулевые байты"}
	}

	switch engine {
	case "envoy":
		return validateEnvoyStructural(body)
	default:
		if err := validateHAProxyStructural(body); err != nil {
			return Result{OK: false, Engine: "haproxy", Method: "structural", Message: err.Error()}
		}
		if r := validateHAProxyDocker(body); r.Method == "haproxy-c" {
			return r
		}
		return Result{
			OK:      true,
			Engine:  "haproxy",
			Method:  "structural",
			Message: "структура OK (haproxy -c недоступен на панели — полный check будет на агенте)",
		}
	}
}

func validateHAProxyStructural(body string) error {
	var hasGlobal, hasDefaults, hasFrontend, hasBackend, hasBind, hasModeTCP bool
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		low := strings.ToLower(t)
		switch {
		case low == "global":
			hasGlobal = true
		case low == "defaults":
			hasDefaults = true
		case strings.HasPrefix(low, "frontend "):
			hasFrontend = true
		case strings.HasPrefix(low, "backend "):
			hasBackend = true
		case strings.HasPrefix(low, "bind ") || strings.HasPrefix(low, "bind\t"):
			hasBind = true
		case strings.HasPrefix(low, "mode ") && strings.Contains(low, "tcp"):
			hasModeTCP = true
		}
	}
	if !hasGlobal {
		return fmt.Errorf("нет секции global")
	}
	if !hasDefaults {
		return fmt.Errorf("нет секции defaults")
	}
	if !hasFrontend {
		return fmt.Errorf("нет frontend (нужен хотя бы entrance_*)")
	}
	if !hasBackend {
		return fmt.Errorf("нет backend (нужен хотя бы app)")
	}
	if !hasModeTCP {
		return fmt.Errorf("ожидается mode tcp для VPN passthrough")
	}
	if !hasBind {
		return fmt.Errorf("нет bind в frontend")
	}
	return nil
}

func validateEnvoyStructural(body string) Result {
	lower := strings.ToLower(body)
	if !strings.Contains(lower, "static_resources") && !strings.Contains(lower, "listeners") {
		return Result{OK: false, Engine: "envoy", Method: "envoy-structural", Message: "нужны static_resources / listeners"}
	}
	if !strings.Contains(lower, "clusters") {
		return Result{OK: false, Engine: "envoy", Method: "envoy-structural", Message: "нужна секция clusters"}
	}
	return Result{OK: true, Engine: "envoy", Method: "envoy-structural", Message: "базовая структура YAML OK"}
}

func validateHAProxyDocker(body string) Result {
	docker, err := exec.LookPath("docker")
	if err != nil {
		return Result{Method: "structural"}
	}
	dir, err := os.MkdirTemp("", "hapanel-hap-validate-*")
	if err != nil {
		return Result{Method: "structural"}
	}
	defer func() { _ = os.RemoveAll(dir) }()
	_ = os.Chmod(dir, 0o755)

	cfgPath := filepath.Join(dir, "haproxy.cfg")
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		return Result{Method: "structural"}
	}
	_ = os.Chmod(cfgPath, 0o644)

	cmd := exec.Command(docker, "run", "--rm",
		"--user", "0:0",
		"-v", dir+":/cfg:ro",
		haproxyImage,
		"haproxy", "-c", "-f", "/cfg/haproxy.cfg",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()

	select {
	case err := <-done:
		out := strings.TrimSpace(stdout.String() + "\n" + stderr.String())
		if err != nil {
			return Result{
				OK:      false,
				Engine:  "haproxy",
				Method:  "haproxy-c",
				Message: "haproxy -c: конфиг невалиден",
				Detail:  truncate(out, 2000),
			}
		}
		return Result{
			OK:      true,
			Engine:  "haproxy",
			Method:  "haproxy-c",
			Message: "haproxy -c: OK",
			Detail:  truncate(out, 500),
		}
	case <-time.After(dockerValidateWait):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return Result{Method: "structural"}
	}
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
