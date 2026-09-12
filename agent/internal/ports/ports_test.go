package ports

import (
	"reflect"
	"testing"
)

func TestNormalize(t *testing.T) {
	got, err := Normalize([]int{8443, 443, 8443})
	if err != nil {
		t.Fatal(err)
	}
	want := []int{443, 8443}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestDiff(t *testing.T) {
	open, close := Diff([]int{8443}, []int{443})
	if !reflect.DeepEqual(open, []int{443}) || !reflect.DeepEqual(close, []int{8443}) {
		t.Fatalf("open=%v close=%v", open, close)
	}
}
