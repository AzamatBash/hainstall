package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type TaskStatus string

const (
	TaskTodo  TaskStatus = "todo"
	TaskDoing TaskStatus = "doing"
	TaskDone  TaskStatus = "done"
)

func ValidTaskStatus(s string) bool {
	switch TaskStatus(s) {
	case TaskTodo, TaskDoing, TaskDone:
		return true
	default:
		return false
	}
}

type Task struct {
	ID             string      `json:"id"`
	RemnaPanelID   string      `json:"remna_panel_id"`
	RemnaPanelName string      `json:"remna_panel_name,omitempty"`
	Description    string      `json:"description"`
	Status         TaskStatus  `json:"status"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
	Images         []TaskImage `json:"images,omitempty"`
}

type TaskImage struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	Mime      string    `json:"mime"`
	Filename  string    `json:"filename"`
	CreatedAt time.Time `json:"created_at"`
}

type TaskUpdate struct {
	Status       *string
	Description  *string
	RemnaPanelID *string
}

func (s *Store) ListTasks() ([]Task, error) {
	rows, err := s.db.Query(`
SELECT t.id, t.remna_panel_id, COALESCE(p.name, ''), t.description, t.status, t.created_at, t.updated_at
FROM tasks t
LEFT JOIN remna_panels p ON p.id = t.remna_panel_id
ORDER BY t.updated_at DESC`)
	if err != nil {
		return nil, err
	}

	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Load images after closing the task rows — SQLite is single-conn (MaxOpenConns=1);
	// nested Query while rows are open deadlocks.
	for i := range out {
		imgs, err := s.ListTaskImages(out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Images = imgs
	}
	if out == nil {
		out = []Task{}
	}
	return out, nil
}

func (s *Store) GetTask(id string) (*Task, error) {
	row := s.db.QueryRow(`
SELECT t.id, t.remna_panel_id, COALESCE(p.name, ''), t.description, t.status, t.created_at, t.updated_at
FROM tasks t
LEFT JOIN remna_panels p ON p.id = t.remna_panel_id
WHERE t.id = ?`, id)
	t, err := scanTask(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	imgs, err := s.ListTaskImages(id)
	if err != nil {
		return nil, err
	}
	t.Images = imgs
	return &t, nil
}

func (s *Store) CreateTask(remnaPanelID, description string) (*Task, error) {
	now := time.Now().UTC()
	t := Task{
		ID:           uuid.NewString(),
		RemnaPanelID: remnaPanelID,
		Description:  description,
		Status:       TaskTodo,
		CreatedAt:    now,
		UpdatedAt:    now,
		Images:       []TaskImage{},
	}
	_, err := s.db.Exec(`
INSERT INTO tasks (id, remna_panel_id, description, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)`,
		t.ID, t.RemnaPanelID, t.Description, string(t.Status),
		t.CreatedAt.Format(time.RFC3339Nano), t.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	return s.GetTask(t.ID)
}

func (s *Store) UpdateTask(id string, fields TaskUpdate) (*Task, error) {
	existing, err := s.GetTask(id)
	if existing == nil || err != nil {
		return existing, err
	}
	status := string(existing.Status)
	description := existing.Description
	remnaPanelID := existing.RemnaPanelID
	if fields.Status != nil {
		if !ValidTaskStatus(*fields.Status) {
			return nil, fmt.Errorf("invalid task status %q", *fields.Status)
		}
		status = *fields.Status
	}
	if fields.Description != nil {
		description = *fields.Description
	}
	if fields.RemnaPanelID != nil {
		remnaPanelID = *fields.RemnaPanelID
	}
	now := time.Now().UTC()
	_, err = s.db.Exec(`
UPDATE tasks SET remna_panel_id = ?, description = ?, status = ?, updated_at = ? WHERE id = ?`,
		remnaPanelID, description, status, now.Format(time.RFC3339Nano), id)
	if err != nil {
		return nil, err
	}
	return s.GetTask(id)
}

// DeleteTask removes the task and its image rows. Returns image IDs for disk cleanup.
func (s *Store) DeleteTask(id string) (imageIDs []string, err error) {
	imgs, err := s.ListTaskImages(id)
	if err != nil {
		return nil, err
	}
	imageIDs = make([]string, 0, len(imgs))
	for _, img := range imgs {
		imageIDs = append(imageIDs, img.ID)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM task_images WHERE task_id = ?`, id); err != nil {
		return nil, err
	}
	res, err := tx.Exec(`DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return imageIDs, nil
}

func (s *Store) AddTaskImage(taskID, mime, filename string) (*TaskImage, error) {
	existing, err := s.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, nil
	}
	now := time.Now().UTC()
	img := TaskImage{
		ID:        uuid.NewString(),
		TaskID:    taskID,
		Mime:      mime,
		Filename:  filename,
		CreatedAt: now,
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
INSERT INTO task_images (id, task_id, mime, filename, created_at)
VALUES (?, ?, ?, ?, ?)`,
		img.ID, img.TaskID, img.Mime, img.Filename, img.CreatedAt.Format(time.RFC3339Nano)); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE tasks SET updated_at = ? WHERE id = ?`, now.Format(time.RFC3339Nano), taskID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &img, nil
}

func (s *Store) ListTaskImages(taskID string) ([]TaskImage, error) {
	rows, err := s.db.Query(`
SELECT id, task_id, mime, filename, created_at
FROM task_images WHERE task_id = ? ORDER BY created_at ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TaskImage
	for rows.Next() {
		img, err := scanTaskImage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, img)
	}
	if out == nil {
		out = []TaskImage{}
	}
	return out, rows.Err()
}

func (s *Store) GetTaskImage(taskID, imageID string) (*TaskImage, error) {
	row := s.db.QueryRow(`
SELECT id, task_id, mime, filename, created_at
FROM task_images WHERE task_id = ? AND id = ?`, taskID, imageID)
	img, err := scanTaskImage(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &img, nil
}

func (s *Store) DeleteTaskImage(taskID, imageID string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(`DELETE FROM task_images WHERE task_id = ? AND id = ?`, taskID, imageID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(`UPDATE tasks SET updated_at = ? WHERE id = ?`, now.Format(time.RFC3339Nano), taskID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func scanTask(r rowScanner) (Task, error) {
	var (
		t                        Task
		status, created, updated string
	)
	if err := r.Scan(&t.ID, &t.RemnaPanelID, &t.RemnaPanelName, &t.Description, &status, &created, &updated); err != nil {
		return Task{}, err
	}
	t.Status = TaskStatus(status)
	ct, err := parseStoreTime(created)
	if err != nil {
		return Task{}, err
	}
	ut, err := parseStoreTime(updated)
	if err != nil {
		return Task{}, err
	}
	t.CreatedAt = ct
	t.UpdatedAt = ut
	return t, nil
}

func scanTaskImage(r rowScanner) (TaskImage, error) {
	var (
		img       TaskImage
		createdAt string
	)
	if err := r.Scan(&img.ID, &img.TaskID, &img.Mime, &img.Filename, &createdAt); err != nil {
		return TaskImage{}, err
	}
	t, err := parseStoreTime(createdAt)
	if err != nil {
		return TaskImage{}, err
	}
	img.CreatedAt = t
	return img, nil
}

func parseStoreTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}, err
		}
	}
	return t, nil
}
