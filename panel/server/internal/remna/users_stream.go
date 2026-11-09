package remna

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// UserSpan is the minimal subscription window for reconstructing ACTIVE-by-date.
type UserSpan struct {
	Created  time.Time
	Expire   time.Time
	Revoked  *time.Time
	Disabled bool
}

// ActiveAt reports whether the user had a non-expired subscription at instant t.
// Currently DISABLED users are excluded (Remnawave has no disable history).
// EXPIRED/LIMITED are decided by dates, not by current status label.
func (u UserSpan) ActiveAt(t time.Time) bool {
	if u.Disabled {
		return false
	}
	if !u.Created.Before(t) {
		return false
	}
	if !u.Expire.After(t) {
		return false
	}
	if u.Revoked != nil && !u.Revoked.After(t) {
		return false
	}
	return true
}

type usersStreamBody struct {
	Response struct {
		Users []struct {
			Status       string  `json:"status"`
			CreatedAt    string  `json:"createdAt"`
			ExpireAt     string  `json:"expireAt"`
			SubRevokedAt *string `json:"subRevokedAt"`
		} `json:"users"`
		// Remnawave versions disagree: string id vs numeric cursor.
		NextCursor any  `json:"nextCursor"`
		HasMore    bool `json:"hasMore"`
	} `json:"response"`
}

func cursorQuery(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case json.Number:
		return t.String()
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

// FetchUserSpans streams all users and keeps subscription windows only.
func (c *Client) FetchUserSpans(ctx context.Context, baseURL, apiKey string) ([]UserSpan, error) {
	rc := c.withTimeout(90 * time.Second)
	var out []UserSpan
	cursor := ""
	for page := 0; page < 500; page++ { // hard cap ~500k users at size=1000
		q := url.Values{}
		q.Set("size", "1000")
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var body usersStreamBody
		if err := rc.getJSON(ctx, baseURL, apiKey, "/api/users/stream?"+q.Encode(), &body); err != nil {
			return nil, fmt.Errorf("users stream page %d: %w", page, err)
		}
		for _, u := range body.Response.Users {
			created, err1 := parseRemnaTime(u.CreatedAt)
			expire, err2 := parseRemnaTime(u.ExpireAt)
			if err1 != nil || err2 != nil {
				continue
			}
			span := UserSpan{
				Created:  created,
				Expire:   expire,
				Disabled: u.Status == "DISABLED",
			}
			if u.SubRevokedAt != nil && *u.SubRevokedAt != "" {
				if rev, err := parseRemnaTime(*u.SubRevokedAt); err == nil {
					span.Revoked = &rev
				}
			}
			out = append(out, span)
		}
		next := cursorQuery(body.Response.NextCursor)
		if !body.Response.HasMore || next == "" || next == cursor {
			return out, nil
		}
		cursor = next
	}
	return out, fmt.Errorf("users stream: exceeded page limit")
}

func parseRemnaTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

// CountActiveAt counts users active at instant t.
func CountActiveAt(spans []UserSpan, t time.Time) int {
	n := 0
	for i := range spans {
		if spans[i].ActiveAt(t) {
			n++
		}
	}
	return n
}

// CountCreatedIn counts users with createdAt in [start, end).
func CountCreatedIn(spans []UserSpan, start, end time.Time) int {
	n := 0
	for i := range spans {
		c := spans[i].Created
		if !c.Before(start) && c.Before(end) {
			n++
		}
	}
	return n
}

// CountExpiredIn counts users whose expireAt falls in [start, end).
// Disabled-only accounts are skipped (they never enter the active base).
func CountExpiredIn(spans []UserSpan, start, end time.Time) int {
	n := 0
	for i := range spans {
		u := &spans[i]
		if u.Disabled {
			continue
		}
		e := u.Expire
		if !e.Before(start) && e.Before(end) {
			n++
		}
	}
	return n
}
