package opsagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type LLM struct {
	Provider   string // gemini|groq
	GeminiKey  string
	GroqKey    string
	HTTPClient *http.Client
}

type Recovery struct {
	Commands []string `json:"commands"`
	Note     string   `json:"note"`
}

func (l *LLM) Enabled() bool {
	if l == nil {
		return false
	}
	switch strings.ToLower(l.Provider) {
	case "groq":
		return strings.TrimSpace(l.GroqKey) != ""
	default:
		return strings.TrimSpace(l.GeminiKey) != "" || strings.TrimSpace(l.GroqKey) != ""
	}
}

func (l *LLM) client() *http.Client {
	if l.HTTPClient != nil {
		return l.HTTPClient
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (l *LLM) SuggestRecovery(ctx context.Context, step, errText, hostOS string) (Recovery, error) {
	if !l.Enabled() {
		return Recovery{}, fmt.Errorf("LLM API keys not configured")
	}
	prompt := fmt.Sprintf(`You are a Linux ops assistant installing hapanel hanode (HAProxy + azamatbash/hanode agent).
Failed step: %s
Host OS hint: %s
Error / command output (truncated):
%s

Return ONLY valid JSON (no markdown):
{"commands":["shell cmd 1","shell cmd 2"],"note":"short russian note"}
Rules:
- commands must be non-interactive bash for Ubuntu/Debian
- do NOT touch unrelated containers
- do NOT open management port 47893 to 0.0.0.0/0; only from panel IP if firewall
- prefer fixing docker, compose, ufw, fail2ban, disk, ports
- stopping nginx / remnanode / remnawave node containers is OK when they block 80/8443
- max 5 commands
`, step, hostOS, truncate(errText, 6000))

	provider := strings.ToLower(l.Provider)
	if provider == "" {
		provider = "gemini"
	}
	var raw string
	var err error
	switch provider {
	case "groq":
		raw, err = l.callGroq(ctx, prompt)
		if err != nil && l.GeminiKey != "" {
			raw, err = l.callGemini(ctx, prompt)
		}
	default:
		raw, err = l.callGemini(ctx, prompt)
		if err != nil && l.GroqKey != "" {
			raw, err = l.callGroq(ctx, prompt)
		}
	}
	if err != nil {
		return Recovery{}, err
	}
	return parseRecovery(raw)
}

func parseRecovery(raw string) (Recovery, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			raw = raw[i : j+1]
		}
	}
	var out Recovery
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return Recovery{}, fmt.Errorf("parse LLM JSON: %w (%s)", err, truncate(raw, 400))
	}
	clean := make([]string, 0, len(out.Commands))
	for _, c := range out.Commands {
		c = strings.TrimSpace(c)
		if c != "" {
			clean = append(clean, c)
		}
	}
	out.Commands = clean
	if len(out.Commands) == 0 {
		return Recovery{}, fmt.Errorf("LLM returned no commands")
	}
	if len(out.Commands) > 5 {
		out.Commands = out.Commands[:5]
	}
	return out, nil
}

func (l *LLM) callGemini(ctx context.Context, prompt string) (string, error) {
	key := strings.TrimSpace(l.GeminiKey)
	if key == "" {
		return "", fmt.Errorf("GEMINI_API_KEY empty")
	}
	// New free-tier keys often have limit:0 on gemini-2.0/2.5-flash; prefer *-latest / 3.x.
	models := []string{
		"gemini-flash-latest",
		"gemini-flash-lite-latest",
		"gemini-3-flash-preview",
		"gemini-3.5-flash",
		"gemini-3.5-flash-lite",
		"gemini-2.0-flash",
	}
	var lastErr error
	for _, model := range models {
		endpoint := "https://generativelanguage.googleapis.com/v1beta/models/" + model + ":generateContent"
		body := map[string]any{
			"contents": []map[string]any{
				{
					"role":  "user",
					"parts": []map[string]string{{"text": prompt}},
				},
			},
			"generationConfig": map[string]any{
				"temperature": 0.2,
			},
		}
		raw, err := l.postJSONGemini(ctx, endpoint, key, body)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", model, err)
			continue
		}
		var resp struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			lastErr = fmt.Errorf("%s decode: %w", model, err)
			continue
		}
		if resp.Error != nil && resp.Error.Message != "" {
			lastErr = fmt.Errorf("%s: %s", model, resp.Error.Message)
			continue
		}
		if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
			lastErr = fmt.Errorf("%s: empty response", model)
			continue
		}
		return resp.Candidates[0].Content.Parts[0].Text, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("gemini: no models tried")
	}
	return "", lastErr
}

func (l *LLM) postJSONGemini(ctx context.Context, endpoint, apiKey string, payload any) ([]byte, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", apiKey)
	res, err := l.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("llm http %d: %s", res.StatusCode, truncate(string(raw), 400))
	}
	return raw, nil
}

func (l *LLM) callGroq(ctx context.Context, prompt string) (string, error) {
	key := strings.TrimSpace(l.GroqKey)
	if key == "" {
		return "", fmt.Errorf("GROQ_API_KEY empty")
	}
	body := map[string]any{
		"model": "llama-3.3-70b-versatile",
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"temperature": 0.2,
	}
	raw, err := l.postJSON(ctx, "https://api.groq.com/openai/v1/chat/completions", "Bearer "+key, body)
	if err != nil {
		return "", err
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", err
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return "", fmt.Errorf("groq: %s", resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("groq: empty response")
	}
	return resp.Choices[0].Message.Content, nil
}

func (l *LLM) postJSON(ctx context.Context, url, auth string, payload any) ([]byte, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	res, err := l.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("llm http %d: %s", res.StatusCode, truncate(string(raw), 300))
	}
	return raw, nil
}
