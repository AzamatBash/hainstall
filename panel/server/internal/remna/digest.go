package remna

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// DigestUsers is the users block from GET /api/system/stats/digest.
type DigestUsers struct {
	CreatedCount int
	ExpiredCount int
}

type digestBody struct {
	Response struct {
		Users struct {
			CreatedCount float64 `json:"createdCount"`
			ExpiredCount float64 `json:"expiredCount"`
		} `json:"users"`
	} `json:"response"`
}

// FetchDigest loads created/expired counts for half-open range [start, end).
func (c *Client) FetchDigest(ctx context.Context, baseURL, apiKey string, start, end time.Time) (DigestUsers, error) {
	if !start.Before(end) {
		return DigestUsers{}, fmt.Errorf("digest range: start must be before end")
	}
	q := url.Values{}
	q.Set("start", start.UTC().Format(time.RFC3339))
	q.Set("end", end.UTC().Format(time.RFC3339))
	var out digestBody
	if err := c.getJSON(ctx, baseURL, apiKey, "/api/system/stats/digest?"+q.Encode(), &out); err != nil {
		return DigestUsers{}, err
	}
	return DigestUsers{
		CreatedCount: int(out.Response.Users.CreatedCount),
		ExpiredCount: int(out.Response.Users.ExpiredCount),
	}, nil
}

// FetchActiveUsers returns current ACTIVE status count from GET /api/system/stats.
func (c *Client) FetchActiveUsers(ctx context.Context, baseURL, apiKey, tz string) (int, error) {
	q := ""
	if tz != "" {
		q = "?tz=" + url.QueryEscape(tz)
	}
	var stats statsBody
	if err := c.getJSON(ctx, baseURL, apiKey, "/api/system/stats"+q, &stats); err != nil {
		return 0, err
	}
	return int(stats.Response.Users.StatusCounts["ACTIVE"]), nil
}
