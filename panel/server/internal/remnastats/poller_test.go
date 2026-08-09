package remnastats

import (
	"testing"

	"github.com/azabash/hapanel/panel/internal/remna"
)

func TestSumUsersOnline(t *testing.T) {
	a, b := 3, 7
	nodes := []remna.Node{
		{UsersOnline: &a},
		{UsersOnline: nil},
		{UsersOnline: &b},
		{},
	}
	if got := SumUsersOnline(nodes); got != 10 {
		t.Fatalf("SumUsersOnline = %d, want 10", got)
	}
	if got := SumUsersOnline(nil); got != 0 {
		t.Fatalf("empty = %d, want 0", got)
	}
}
