package store

import (
	"path/filepath"
	"testing"
)

func TestSeedOlcrtcDemo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "olcrtc.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	seeded, err := st.SeedOlcrtcDemo()
	if err != nil {
		t.Fatal(err)
	}
	if seeded {
		t.Fatal("seed should be no-op")
	}
	nodes, err := st.ListOlcrtcNodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 0 {
		t.Fatalf("nodes=%d want 0", len(nodes))
	}

	token := "tok-demo"
	n, err := st.CreateOlcrtcNode("local", "http://127.0.0.1:9201", "127.0.0.1", token, "DE", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if n.Token != token || !n.HasToken {
		t.Fatalf("token not stored: %+v", n)
	}
	got, err := st.GetOlcrtcNode(n.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.Token != token {
		t.Fatalf("token=%q", got.Token)
	}
	pub := OlcrtcNodePublic(*got)
	if pub.Token != "" || !pub.HasToken {
		t.Fatalf("public=%+v", pub)
	}
}
