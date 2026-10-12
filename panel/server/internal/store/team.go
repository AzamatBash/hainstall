package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const TeamDayOff = "off"

var teamColors = []string{
	"#14b8a6",
	"#38bdf8",
	"#a78bfa",
	"#f59e0b",
	"#fb7185",
	"#84cc16",
	"#e879f9",
	"#22d3ee",
}

type TeamMember struct {
	ID        string
	Name      string
	Color     string
	CreatedAt time.Time
}

type TeamDay struct {
	MemberID  string
	Day       string
	Status    string
	Note      string
	UpdatedAt time.Time
}

func ValidTeamDayStatus(s string) bool {
	return s == TeamDayOff
}

func (s *Store) ListTeamMembers() ([]TeamMember, error) {
	rows, err := s.db.Query(`SELECT id, name, color, created_at FROM team_members ORDER BY created_at ASC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TeamMember
	for rows.Next() {
		var m TeamMember
		var created string
		if err := rows.Scan(&m.ID, &m.Name, &m.Color, &created); err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) CreateTeamMember(name string) (*TeamMember, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("укажите имя")
	}
	if len([]rune(name)) > 40 {
		return nil, fmt.Errorf("имя длиннее 40 символов")
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM team_members`).Scan(&n); err != nil {
		return nil, err
	}
	m := TeamMember{
		ID:        uuid.NewString(),
		Name:      name,
		Color:     teamColors[n%len(teamColors)],
		CreatedAt: time.Now().UTC(),
	}
	_, err := s.db.Exec(`INSERT INTO team_members (id, name, color, created_at) VALUES (?, ?, ?, ?)`,
		m.ID, m.Name, m.Color, m.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) RenameTeamMember(id, name string) (*TeamMember, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("укажите имя")
	}
	if len([]rune(name)) > 40 {
		return nil, fmt.Errorf("имя длиннее 40 символов")
	}
	res, err := s.db.Exec(`UPDATE team_members SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, sql.ErrNoRows
	}
	var m TeamMember
	var created string
	err = s.db.QueryRow(`SELECT id, name, color, created_at FROM team_members WHERE id = ?`, id).
		Scan(&m.ID, &m.Name, &m.Color, &created)
	if err != nil {
		return nil, err
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return &m, nil
}

func (s *Store) DeleteTeamMember(id string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM team_days WHERE member_id = ?`, id); err != nil {
		return false, err
	}
	res, err := tx.Exec(`DELETE FROM team_members WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *Store) ListTeamDays() ([]TeamDay, error) {
	rows, err := s.db.Query(`SELECT member_id, day, status, note, updated_at FROM team_days ORDER BY day ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TeamDay
	for rows.Next() {
		var w TeamDay
		var updated string
		if err := rows.Scan(&w.MemberID, &w.Day, &w.Status, &w.Note, &updated); err != nil {
			return nil, err
		}
		w.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) SetTeamDay(memberID, day, status, note string) error {
	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM team_members WHERE id = ?`, memberID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return fmt.Errorf("человек не найден")
	}
	if strings.TrimSpace(status) == "" {
		_, err := s.db.Exec(`DELETE FROM team_days WHERE member_id = ? AND day = ?`, memberID, day)
		return err
	}
	if !ValidTeamDayStatus(status) {
		return fmt.Errorf("статус: off")
	}
	note = strings.TrimSpace(note)
	if len([]rune(note)) > 200 {
		return fmt.Errorf("заметка длиннее 200 символов")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`
INSERT INTO team_days (member_id, day, status, note, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(member_id, day) DO UPDATE SET
  status = excluded.status,
  note = excluded.note,
  updated_at = excluded.updated_at
`, memberID, day, status, note, now)
	return err
}
