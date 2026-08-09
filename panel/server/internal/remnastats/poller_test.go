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

func TestSumTrafficUsedBytes(t *testing.T) {
	a, b, neg := 1_000_000.0, 2_500_000.0, -10.0
	nodes := []remna.Node{
		{TrafficUsedBytes: &a},
		{TrafficUsedBytes: nil},
		{TrafficUsedBytes: &b},
		{TrafficUsedBytes: &neg},
		{},
	}
	if got := SumTrafficUsedBytes(nodes); got != 3_500_000 {
		t.Fatalf("SumTrafficUsedBytes = %v, want 3500000", got)
	}
	if got := SumTrafficUsedBytes(nil); got != 0 {
		t.Fatalf("empty = %v, want 0", got)
	}
}
