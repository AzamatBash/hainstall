package store

import (
	"path/filepath"
	"testing"
)

func TestProxyProfilesCRUDAndAssign(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	p, err := st.CreateProxyProfile("main", ProxyEngineHAProxy, "frontend fe\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Revision != 1 || p.Engine != ProxyEngineHAProxy {
		t.Fatalf("got %+v", p)
	}

	p2, err := st.UpdateProxyProfile(p.ID, "main", ProxyEngineHAProxy, "frontend fe\n  bind :443\n", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if p2.Revision != 2 {
		t.Fatalf("revision=%d want 2", p2.Revision)
	}

	created, err := st.CreateNode("n1", "https://127.0.0.1:1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	n, err := st.GetNode(created.ID)
	if err != nil || n == nil {
		t.Fatal(err)
	}
	if n.ProxyEngine != ProxyEngineHAProxy {
		t.Fatalf("default engine=%q", n.ProxyEngine)
	}

	n2, err := st.AssignNodeProxyProfile(n.ID, p.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if n2.ProxyProfileID != p.ID || n2.ProxyDesiredRev != 2 || n2.ProxyApplyStatus != ProxyApplyPending {
		t.Fatalf("assign %+v", n2)
	}

	if _, err := st.SetNodeProxyEngine(n.ID, ProxyEngineEnvoy); err == nil {
		t.Fatal("expected engine mismatch error")
	}

	ok, err := st.DeleteProxyProfile(p.ID)
	if err != nil || !ok {
		t.Fatalf("delete ok=%v err=%v", ok, err)
	}
	n3, err := st.GetNode(n.ID)
	if err != nil || n3 == nil {
		t.Fatal(err)
	}
	if n3.ProxyProfileID != "" || n3.ProxyDesiredRev != 0 {
		t.Fatalf("node not detached %+v", n3)
	}
}
