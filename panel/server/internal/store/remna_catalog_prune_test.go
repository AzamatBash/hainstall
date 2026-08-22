package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPruneRemnaNodeCatalog(t *testing.T) {
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
	for _, uuid := range []string{"keep-a", "drop-b", "drop-c"} {
		if err := st.UpsertRemnaNodeCatalogSync(RemnaNodeSyncInput{
			PanelID: panel.ID, RemnaUUID: uuid, Name: uuid, Address: "1.1.1.1",
			ProtocolDerived: "vless", UsersOnline: 1, NodeOK: true, At: now,
		}); err != nil {
			t.Fatal(err)
		}
		if err := st.AppendRemnaNodeOnlineSample(panel.ID, uuid, now, 1, true); err != nil {
			t.Fatal(err)
		}
		if err := st.AppendRemnaNodeTrafficSample(panel.ID, uuid, now, 10, 20); err != nil {
			t.Fatal(err)
		}
	}

	n, err := st.PruneRemnaNodeCatalog(panel.ID, []string{"keep-a"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("pruned=%d want 2", n)
	}

	list, err := st.ListRemnaNodeCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].RemnaUUID != "keep-a" {
		t.Fatalf("catalog=%+v", list)
	}
	online, err := st.ListRemnaNodeOnlineSamples(panel.ID, "drop-b", now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(online) != 0 {
		t.Fatalf("online leftovers for drop-b: %d", len(online))
	}
	traffic, err := st.ListRemnaNodeTrafficSamples(panel.ID, "drop-c", now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(traffic) != 0 {
		t.Fatalf("traffic leftovers for drop-c: %d", len(traffic))
	}
}

func TestDeleteRemnaPanelClearsCatalog(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	panel, err := st.CreateRemnaPanel("gone", "https://remna.example", []byte("enc"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := st.UpsertRemnaNodeCatalogSync(RemnaNodeSyncInput{
		PanelID: panel.ID, RemnaUUID: "n1", Name: "n1", Address: "2.2.2.2",
		ProtocolDerived: "vless", At: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendRemnaOnlineSample(panel.ID, now, 5); err != nil {
		t.Fatal(err)
	}

	ok, err := st.DeleteRemnaPanel(panel.ID)
	if err != nil || !ok {
		t.Fatalf("delete panel: ok=%v err=%v", ok, err)
	}
	list, err := st.ListRemnaNodeCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("catalog leftovers: %+v", list)
	}
	samples, err := st.ListRemnaOnlineSamples(panel.ID, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 0 {
		t.Fatalf("panel online leftovers: %d", len(samples))
	}
}

func TestDeleteNodeClearsRemnaLinks(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	n, err := st.CreateNode("hap", "http://127.0.0.1:47893", "tok")
	if err != nil {
		t.Fatal(err)
	}
	panel, err := st.CreateRemnaPanel("rp", "https://remna.example", []byte("enc"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertBackendRemnaLink(n.ID, "app", "srv1", panel.ID, "1.2.3.4"); err != nil {
		t.Fatal(err)
	}
	ok, err := st.DeleteNode(n.ID)
	if err != nil || !ok {
		t.Fatalf("delete node: ok=%v err=%v", ok, err)
	}
	links, err := st.GetBackendRemnaLinks(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 0 {
		t.Fatalf("remna links leftovers: %+v", links)
	}
}
