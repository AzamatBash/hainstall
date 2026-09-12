package ports

import (
	"fmt"
	"sort"
)

// DefaultListen is used when no client ports are configured.
var DefaultListen = []int{8443}

// Normalize validates and deduplicates listen ports.
func Normalize(in []int) ([]int, error) {
	if len(in) == 0 {
		out := make([]int, len(DefaultListen))
		copy(out, DefaultListen)
		return out, nil
	}
	seen := make(map[int]struct{}, len(in))
	out := make([]int, 0, len(in))
	for _, p := range in {
		if p < 1 || p > 65535 {
			return nil, fmt.Errorf("invalid port %d (want 1–65535)", p)
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	sort.Ints(out)
	return out, nil
}

// Diff returns ports to open and close when moving from old → new.
func Diff(oldPorts, newPorts []int) (open, close []int) {
	oldSet := make(map[int]struct{}, len(oldPorts))
	for _, p := range oldPorts {
		oldSet[p] = struct{}{}
	}
	newSet := make(map[int]struct{}, len(newPorts))
	for _, p := range newPorts {
		newSet[p] = struct{}{}
	}
	for p := range newSet {
		if _, ok := oldSet[p]; !ok {
			open = append(open, p)
		}
	}
	for p := range oldSet {
		if _, ok := newSet[p]; !ok {
			close = append(close, p)
		}
	}
	sort.Ints(open)
	sort.Ints(close)
	return open, close
}
