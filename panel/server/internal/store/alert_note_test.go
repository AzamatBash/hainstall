package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAlertNoteSurvivesCatalogSync(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	panel, err := st.CreateRemnaPanel("p1", "https://remna.example", []byte("enc"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	in := RemnaNodeSyncInput{
		PanelID: panel.ID, RemnaUUID: "node-1", Name: "edge", Address: "1.1.1.1",
		ProtocolDerived: "vless", UsersOnline: 1, NodeOK: true, At: now,
	}
	if err := st.UpsertRemnaNodeCatalogSync(in); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRemnaNodeAlertNote(panel.ID, "node-1", "провайдер режет"); err != nil {
		t.Fatal(err)
	}
	in.Name = "edge-2"
	in.At = now.Add(time.Minute)
	if err := st.UpsertRemnaNodeCatalogSync(in); err != nil {
		t.Fatal(err)
	}
	notes, err := st.MapRemnaNodeAlertNotes()
	if err != nil {
		t.Fatal(err)
	}
	if notes[panel.ID+"\x00node-1"] != "провайдер режет" {
		t.Fatalf("note=%q", notes[panel.ID+"\x00node-1"])
	}
	if err := st.SetRemnaNodeAlertNote(panel.ID, "missing", "x"); err == nil {
		t.Fatal("expected missing node")
	}
}
