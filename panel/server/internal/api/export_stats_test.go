package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/azabash/hapanel/panel/internal/agent"
	"github.com/azabash/hapanel/panel/internal/auth"
	"github.com/azabash/hapanel/panel/internal/store"
)

func TestExportHanodes_APIKey(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	n, err := st.CreateNode("de-1", "http://127.0.0.1:9100", "tok")
	if err != nil {
		t.Fatal(err)
	}
	cpu := 12.5
	down := 1.5e6
	up := 2.0e5
	sess := 7
	if err := st.SaveSnapshot(n.ID, store.StatusOnline, store.NodeSnapshot{
		CPU:       &cpu,
		LoadAvg:   []float64{0.1, 0.2, 0.3},
		DownBps:   &down,
		UpBps:     &up,
		Sessions:  &sess,
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	hour := time.Now().UTC().Truncate(time.Hour)
	if err := st.AddTrafficHourlyDelta(n.ID, hour, 1000, 2000); err != nil {
		t.Fatal(err)
	}

	au := auth.New("admin-pass", "jwt-secret-for-tests", time.Hour)
	srv := New(st, au, agent.New(true), slog.New(slog.NewTextHandler(os.Stderr, nil)), Options{
		APIKey: "test-export-key",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/hanodes?hours=24", nil)
	req.Header.Set("X-API-Key", "test-export-key")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Count int `json:"count"`
		Nodes []struct {
			ID             string    `json:"id"`
			Name           string    `json:"name"`
			Status         string    `json:"status"`
			CPU            float64   `json:"cpu"`
			DownBps        float64   `json:"down_bps"`
			UpBps          float64   `json:"up_bps"`
			Sessions       int       `json:"sessions"`
			LoadAvg        []float64 `json:"load_avg"`
			PeriodRxBytes  int64     `json:"period_rx_bytes"`
			PeriodTxBytes  int64     `json:"period_tx_bytes"`
			URL            string    `json:"url"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Count != 1 || len(body.Nodes) != 1 {
		t.Fatalf("count=%d nodes=%d", body.Count, len(body.Nodes))
	}
	got := body.Nodes[0]
	if got.ID != n.ID || got.Name != "de-1" || got.Status != "online" {
		t.Fatalf("identity %+v", got)
	}
	if got.CPU != 12.5 || got.DownBps != 1.5e6 || got.UpBps != 2.0e5 || got.Sessions != 7 {
		t.Fatalf("metrics %+v", got)
	}
	if got.PeriodRxBytes != 1000 || got.PeriodTxBytes != 2000 {
		t.Fatalf("period rx=%d tx=%d", got.PeriodRxBytes, got.PeriodTxBytes)
	}
	if got.URL != "" {
		t.Fatal("must not leak node URL")
	}

	bad := httptest.NewRequest(http.MethodGet, "/api/v1/hanodes", nil)
	bad.Header.Set("X-API-Key", "wrong")
	badRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(badRec, bad)
	if badRec.Code != http.StatusUnauthorized {
		t.Fatalf("bad key status=%d", badRec.Code)
	}

	created, err := st.CreateAPIKey("ui-key")
	if err != nil {
		t.Fatal(err)
	}
	dbReq := httptest.NewRequest(http.MethodGet, "/api/v1/hanodes", nil)
	dbReq.Header.Set("Authorization", "Bearer "+created.Key)
	dbRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(dbRec, dbReq)
	if dbRec.Code != http.StatusOK {
		t.Fatalf("db key status=%d body=%s", dbRec.Code, dbRec.Body.String())
	}
}

func TestExportHanodeRow_omitsEmptyLive(t *testing.T) {
	row := exportHanodeRow(store.Node{ID: "x", Name: "n", Status: store.StatusUnknown})
	if _, ok := row["cpu"]; ok {
		t.Fatal("cpu should be absent")
	}
	if row["status"] != store.StatusUnknown {
		t.Fatalf("status=%v", row["status"])
	}
}
