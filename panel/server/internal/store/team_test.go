package store

import (
	"path/filepath"
	"testing"
)

func TestTeamDays(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "team.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	a, err := st.CreateTeamMember("Аня")
	if err != nil {
		t.Fatal(err)
	}
	day := "2026-10-31"
	next := "2026-11-01"
	if err := st.SetTeamDay(a.ID, day, TeamDayOff, ""); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTeamDay(a.ID, next, TeamDayOff, ""); err != nil {
		t.Fatal(err)
	}
	marks, err := st.ListTeamDays()
	if err != nil {
		t.Fatal(err)
	}
	if len(marks) != 2 || marks[0].Day != day || marks[1].Day != next {
		t.Fatalf("marks=%+v", marks)
	}
	if err := st.SetTeamDay(a.ID, day, "", ""); err != nil {
		t.Fatal(err)
	}
	marks, err = st.ListTeamDays()
	if err != nil {
		t.Fatal(err)
	}
	if len(marks) != 1 || marks[0].Day != next {
		t.Fatalf("expected only %s, got %+v", next, marks)
	}
}
