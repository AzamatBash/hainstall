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

func TestSumTrafficRates(t *testing.T) {
	nodes := []remna.Node{
		{System: &remna.NodeSystem{Stats: remna.NodeSystemStats{
			Interface: &remna.NetworkIface{RxBytesPerSec: 100, TxBytesPerSec: 400},
		}}},
		{System: &remna.NodeSystem{Stats: remna.NodeSystemStats{
			Interface: &remna.NetworkIface{RxBytesPerSec: 50, TxBytesPerSec: 200},
		}}},
		{System: nil},
		{},
	}
	down, up := SumTrafficRates(nodes)
	// down=TX, up=RX
	if down != 600 || up != 150 {
		t.Fatalf("SumTrafficRates = %v/%v, want 600/150", down, up)
	}
	down, up = SumTrafficRates(nil)
	if down != 0 || up != 0 {
		t.Fatalf("empty = %v/%v, want 0/0", down, up)
	}
}
