package store

import (
	"path/filepath"
	"testing"
)

func TestAPIKeyCreateValidateRevoke(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "keys.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	created, err := st.CreateAPIKey("collector")
	if err != nil {
		t.Fatal(err)
	}
	if created.Key == "" || created.Prefix == "" || created.Name != "collector" {
		t.Fatalf("created=%+v", created)
	}
	if !st.ValidAPIKey(created.Key) {
		t.Fatal("fresh key should be valid")
	}
	if st.ValidAPIKey("hp_deadbeef") {
		t.Fatal("random key must not validate")
	}

	ok, err := st.RevokeAPIKey(created.ID)
	if err != nil || !ok {
		t.Fatalf("revoke ok=%v err=%v", ok, err)
	}
	if st.ValidAPIKey(created.Key) {
		t.Fatal("revoked key must not validate")
	}

	list, err := st.ListAPIKeys()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].RevokedAt == nil {
		t.Fatalf("list=%+v", list)
	}
}
