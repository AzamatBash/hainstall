package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteMatchesSanitizedName(t *testing.T) {
	dir := t.TempDir()
	st := New(filepath.Join(dir, "state.json"))
	if err := st.Upsert(Server{
		Backend: "app",
		Name:    "DE Selectel OUT",
		Address: "1.2.3.4",
		Port:    443,
		Weight:  100,
	}); err != nil {
		t.Fatal(err)
	}
	ok, err := st.Delete("app", "DE_Selectel_OUT")
	if err != nil || !ok {
		t.Fatalf("delete sanitized: ok=%v err=%v", ok, err)
	}
	list, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty store, got %+v", list)
	}
}

func TestMigrateSanitizeNames(t *testing.T) {
	dir := t.TempDir()
	st := New(filepath.Join(dir, "state.json"))
	if err := st.Upsert(Server{
		Backend: "app", Name: "DE Selectel OUT", Address: "1.1.1.1", Port: 443, Weight: 100,
	}); err != nil {
		t.Fatal(err)
	}
	n, err := st.MigrateSanitizeNames()
	if err != nil || n != 1 {
		t.Fatalf("migrate: n=%d err=%v", n, err)
	}
	list, _ := st.List()
	if list[0].Name != "DE_Selectel_OUT" {
		t.Fatalf("got name %q", list[0].Name)
	}
}

func TestHydrateEntrancesFromListenPorts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte(`{"servers":[],"listen_ports":[443,8443]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st := New(path)
	ents, err := st.Entrances()
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 2 || ents[0].Port != 443 || ents[0].Backend != "app" || ents[1].Port != 8443 {
		t.Fatalf("got %+v", ents)
	}
}

func TestSetEntrances(t *testing.T) {
	dir := t.TempDir()
	st := New(filepath.Join(dir, "state.json"))
	ents, err := st.SetEntrances([]Entrance{
		{Port: 8443, Backend: "app"},
		{Port: 443, Backend: "edge"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 2 || ents[0].Port != 443 || ents[0].Backend != "edge" {
		t.Fatalf("sorted/normalized: %+v", ents)
	}
	loaded, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.ListenPorts) != 2 || loaded.ListenPorts[0] != 443 {
		t.Fatalf("listen_ports derived: %+v", loaded.ListenPorts)
	}
}

