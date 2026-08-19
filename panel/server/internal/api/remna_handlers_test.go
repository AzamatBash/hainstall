package api

import (
	"testing"

	"github.com/azabash/hapanel/panel/internal/store"
)

func TestActiveRemnaLinks_filtersStaleBackends(t *testing.T) {
	links := []store.BackendRemnaLink{
		{Backend: "app", ServerName: "VLES_ams-4", RemnaAddress: "72.56.18.247"},
		{Backend: "app", ServerName: "VLES_ams-6", RemnaAddress: "72.56.98.164"},
	}
	snap := &store.NodeSnapshot{
		Backends: []store.SnapshotBackend{
			{Backend: "app", Name: "VLES_ams-4", Address: "72.56.18.247", Port: 8443},
		},
	}
	got := activeRemnaLinks(links, snap)
	if len(got) != 1 {
		t.Fatalf("len=%d want 1", len(got))
	}
	if got[0].ServerName != "VLES_ams-4" {
		t.Fatalf("server=%q want VLES_ams-4", got[0].ServerName)
	}
}

func TestActiveRemnaLinks_noSnapshotKeepsAll(t *testing.T) {
	links := []store.BackendRemnaLink{
		{Backend: "app", ServerName: "a", RemnaAddress: "1.1.1.1"},
		{Backend: "app", ServerName: "b", RemnaAddress: "2.2.2.2"},
	}
	got := activeRemnaLinks(links, nil)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
}
