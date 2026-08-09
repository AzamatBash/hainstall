package remna

import (
	"context"
	"encoding/json"
	"strings"
)

// Inbound is a config-profile inbound from GET /api/config-profiles/inbounds.
type Inbound struct {
	UUID        string `json:"uuid"`
	ProfileUUID string `json:"profileUuid"`
	Tag         string `json:"tag"`
	Type        string `json:"type"`
	Network     string `json:"network"`
	Security    string `json:"security"`
	Port        int    `json:"port"`
}

// NodeConfigProfile holds the active profile + inbound refs attached to a node.
type NodeConfigProfile struct {
	ActiveConfigProfileUUID string          `json:"activeConfigProfileUuid"`
	ActiveInbounds          json.RawMessage `json:"activeInbounds"`
}

// ConfigProfileUUID returns the profile id from nested or flat node fields.
func (n *Node) ConfigProfileUUID() string {
	if n.ConfigProfile != nil {
		if u := strings.TrimSpace(n.ConfigProfile.ActiveConfigProfileUUID); u != "" {
			return u
		}
	}
	return strings.TrimSpace(n.ActiveConfigProfileUUID)
}

// ActiveInboundUUIDs returns inbound UUIDs attached to the node (best-effort).
func (n *Node) ActiveInboundUUIDs() []string {
	if n.ConfigProfile != nil {
		if ids := parseInboundUUIDList(n.ConfigProfile.ActiveInbounds); len(ids) > 0 {
			return ids
		}
	}
	if len(n.ConfigProfileInbounds) > 0 {
		return append([]string(nil), n.ConfigProfileInbounds...)
	}
	return nil
}

func parseInboundUUIDList(raw json.RawMessage) []string {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var asStrings []string
	if err := json.Unmarshal(raw, &asStrings); err == nil {
		out := make([]string, 0, len(asStrings))
		for _, s := range asStrings {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	var asObjects []struct {
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal(raw, &asObjects); err == nil {
		out := make([]string, 0, len(asObjects))
		for _, o := range asObjects {
			u := strings.TrimSpace(o.UUID)
			if u != "" {
				out = append(out, u)
			}
		}
		return out
	}
	return nil
}

type inboundsListResponse struct {
	Response json.RawMessage `json:"response"`
}

type inboundsWrapped struct {
	Total    int       `json:"total"`
	Inbounds []Inbound `json:"inbounds"`
}

// ListAllInbounds fetches every inbound across config profiles.
func (c *Client) ListAllInbounds(ctx context.Context, baseURL, apiKey string) ([]Inbound, error) {
	var out inboundsListResponse
	if err := c.getJSON(ctx, baseURL, apiKey, "/api/config-profiles/inbounds", &out); err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(string(out.Response))
	if raw == "" || raw == "null" {
		return []Inbound{}, nil
	}
	// Newer panels: { response: { total, inbounds: [...] } }
	var wrapped inboundsWrapped
	if err := json.Unmarshal(out.Response, &wrapped); err == nil && wrapped.Inbounds != nil {
		return wrapped.Inbounds, nil
	}
	// Older / alternate: { response: [...] }
	var list []Inbound
	if err := json.Unmarshal(out.Response, &list); err != nil {
		return nil, err
	}
	if list == nil {
		return []Inbound{}, nil
	}
	return list, nil
}

// InboundByUUID builds a lookup map.
func InboundByUUID(inbounds []Inbound) map[string]Inbound {
	m := make(map[string]Inbound, len(inbounds))
	for _, in := range inbounds {
		id := strings.TrimSpace(in.UUID)
		if id == "" {
			continue
		}
		m[id] = in
	}
	return m
}

// DeriveProtocolFromInbounds picks protocol from the primary (first) inbound.
// Multiple different types → "mixed"; empty → "unknown".
func DeriveProtocolFromInbounds(inboundUUIDs []string, byUUID map[string]Inbound) (protocol string, tags []string) {
	tags = make([]string, 0, len(inboundUUIDs))
	seenProto := make(map[string]struct{})
	var first string
	for _, id := range inboundUUIDs {
		in, ok := byUUID[strings.TrimSpace(id)]
		if !ok {
			continue
		}
		if t := strings.TrimSpace(in.Tag); t != "" {
			tags = append(tags, t)
		}
		p := protocolFromInbound(in)
		if p == "" || p == "unknown" {
			continue
		}
		if first == "" {
			first = p
		}
		seenProto[p] = struct{}{}
	}
	switch len(seenProto) {
	case 0:
		return "unknown", tags
	case 1:
		return first, tags
	default:
		return "mixed", tags
	}
}

func protocolFromInbound(in Inbound) string {
	typ := strings.ToLower(strings.TrimSpace(in.Type))
	sec := strings.ToLower(strings.TrimSpace(in.Security))
	switch {
	case typ == "vless" && (sec == "reality" || strings.Contains(sec, "reality")):
		return "vless_reality"
	case typ == "vless":
		return "vless"
	case typ == "hysteria2" || typ == "hysteria" || typ == "hy2":
		return "hysteria2"
	case typ == "shadowsocks" || typ == "ss":
		return "shadowsocks"
	case typ == "trojan":
		return "trojan"
	case typ == "wireguard" || typ == "wg":
		return "wireguard"
	case typ == "":
		return "unknown"
	default:
		return typ
	}
}

// EffectiveProtocol prefers override when set.
func EffectiveProtocol(override, derived string) string {
	o := strings.TrimSpace(override)
	if o != "" {
		return o
	}
	d := strings.TrimSpace(derived)
	if d == "" {
		return "unknown"
	}
	return d
}
