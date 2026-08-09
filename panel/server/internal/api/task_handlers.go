package api

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/azabash/hapanel/panel/internal/store"
)

const maxTaskImageBytes = 8 << 20 // 8 MiB decoded

var allowedTaskImageMimes = map[string]string{
	"image/png":  "image.png",
	"image/jpeg": "image.jpg",
	"image/webp": "image.webp",
	"image/gif":  "image.gif",
}

func publicTask(t store.Task) map[string]any {
	images := make([]map[string]any, 0, len(t.Images))
	for _, img := range t.Images {
		images = append(images, publicTaskImage(t.ID, img))
	}
	m := map[string]any{
		"id":               t.ID,
		"remna_panel_id":   t.RemnaPanelID,
		"remna_panel_name": t.RemnaPanelName,
		"description":      t.Description,
		"status":           string(t.Status),
		"created_at":       t.CreatedAt.Format(time.RFC3339),
		"updated_at":       t.UpdatedAt.Format(time.RFC3339),
		"images":           images,
	}
	return m
}

func publicTaskImage(taskID string, img store.TaskImage) map[string]any {
	return map[string]any{
		"id":   img.ID,
		"mime": img.Mime,
		"url":  "/api/tasks/" + taskID + "/images/" + img.ID,
	}
}

func (s *Server) taskImagePath(taskID, imageID string) string {
	return filepath.Join(s.imagesDir, taskID, imageID)
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.store.ListTasks()
	if err != nil {
		s.logger.Error("list tasks", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	out := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, publicTask(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": out})
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RemnaPanelID string `json:"remna_panel_id"`
		Description  string `json:"description"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	body.RemnaPanelID = strings.TrimSpace(body.RemnaPanelID)
	body.Description = strings.TrimSpace(body.Description)
	if body.RemnaPanelID == "" {
		writeErr(w, http.StatusBadRequest, "выберите панель")
		return
	}
	if body.Description == "" {
		writeErr(w, http.StatusBadRequest, "описание обязательно")
		return
	}
	p, err := s.store.GetRemnaPanel(body.RemnaPanelID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if p == nil {
		writeErr(w, http.StatusBadRequest, "remna-панель не найдена")
		return
	}
	t, err := s.store.CreateTask(body.RemnaPanelID, body.Description)
	if err != nil {
		s.logger.Error("create task", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	writeJSON(w, http.StatusCreated, publicTask(*t))
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t, err := s.store.GetTask(id)
	if err != nil {
		s.logger.Error("get task", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if t == nil {
		writeErr(w, http.StatusNotFound, "задача не найдена")
		return
	}
	writeJSON(w, http.StatusOK, publicTask(*t))
}

func (s *Server) handleUpdateTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Status       *string `json:"status"`
		Description  *string `json:"description"`
		RemnaPanelID *string `json:"remna_panel_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	if body.Status == nil && body.Description == nil && body.RemnaPanelID == nil {
		writeErr(w, http.StatusBadRequest, "укажите status, description или remna_panel_id")
		return
	}
	fields := store.TaskUpdate{}
	if body.Status != nil {
		st := strings.TrimSpace(*body.Status)
		if !store.ValidTaskStatus(st) {
			writeErr(w, http.StatusBadRequest, "status должен быть todo, doing или done")
			return
		}
		fields.Status = &st
	}
	if body.Description != nil {
		d := strings.TrimSpace(*body.Description)
		fields.Description = &d
	}
	if body.RemnaPanelID != nil {
		rp := strings.TrimSpace(*body.RemnaPanelID)
		if rp != "" {
			p, err := s.store.GetRemnaPanel(rp)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
				return
			}
			if p == nil {
				writeErr(w, http.StatusBadRequest, "remna-панель не найдена")
				return
			}
		}
		fields.RemnaPanelID = &rp
	}
	t, err := s.store.UpdateTask(id, fields)
	if err != nil {
		s.logger.Error("update task", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if t == nil {
		writeErr(w, http.StatusNotFound, "задача не найдена")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "task": publicTask(*t)})
}

func (s *Server) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	imageIDs, err := s.store.DeleteTask(id)
	if err != nil {
		s.logger.Error("delete task", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if imageIDs == nil {
		writeErr(w, http.StatusNotFound, "задача не найдена")
		return
	}
	for _, imageID := range imageIDs {
		_ = os.Remove(s.taskImagePath(id, imageID))
	}
	_ = os.Remove(filepath.Join(s.imagesDir, id))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleUploadTaskImage(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	t, err := s.store.GetTask(taskID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if t == nil {
		writeErr(w, http.StatusNotFound, "задача не найдена")
		return
	}
	if s.imagesDir == "" {
		writeErr(w, http.StatusInternalServerError, "каталог изображений не настроен")
		return
	}

	// Base64 of 8 MiB ≈ 11 MiB; allow some JSON overhead.
	var body struct {
		Mime       string `json:"mime"`
		DataBase64 string `json:"data_base64"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 14<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	mime := strings.TrimSpace(strings.ToLower(body.Mime))
	filename, ok := allowedTaskImageMimes[mime]
	if !ok {
		writeErr(w, http.StatusBadRequest, "допустимы image/png, image/jpeg, image/webp, image/gif")
		return
	}
	raw := strings.TrimSpace(body.DataBase64)
	if raw == "" {
		writeErr(w, http.StatusBadRequest, "укажите data_base64")
		return
	}
	if i := strings.Index(raw, ","); i >= 0 && strings.Contains(raw[:i], "base64") {
		raw = raw[i+1:]
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(raw)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "некорректный base64")
			return
		}
	}
	if len(data) == 0 {
		writeErr(w, http.StatusBadRequest, "пустое изображение")
		return
	}
	if len(data) > maxTaskImageBytes {
		writeErr(w, http.StatusBadRequest, "изображение больше 8 МБ")
		return
	}

	img, err := s.store.AddTaskImage(taskID, mime, filename)
	if err != nil {
		s.logger.Error("add task image", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if img == nil {
		writeErr(w, http.StatusNotFound, "задача не найдена")
		return
	}

	dir := filepath.Join(s.imagesDir, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		_, _ = s.store.DeleteTaskImage(taskID, img.ID)
		s.logger.Error("mkdir task images", "err", err)
		writeErr(w, http.StatusInternalServerError, "не удалось сохранить файл")
		return
	}
	path := s.taskImagePath(taskID, img.ID)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		_, _ = s.store.DeleteTaskImage(taskID, img.ID)
		s.logger.Error("write task image", "err", err)
		writeErr(w, http.StatusInternalServerError, "не удалось сохранить файл")
		return
	}
	writeJSON(w, http.StatusCreated, publicTaskImage(taskID, *img))
}

func (s *Server) handleGetTaskImage(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	imageID := r.PathValue("imageId")
	img, err := s.store.GetTaskImage(taskID, imageID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if img == nil {
		writeErr(w, http.StatusNotFound, "изображение не найдено")
		return
	}
	path := s.taskImagePath(taskID, imageID)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, "файл не найден")
			return
		}
		s.logger.Error("open task image", "err", err)
		writeErr(w, http.StatusInternalServerError, "не удалось прочитать файл")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", img.Mime)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, img.Filename, img.CreatedAt, f)
}

func (s *Server) handleDeleteTaskImage(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	imageID := r.PathValue("imageId")
	ok, err := s.store.DeleteTaskImage(taskID, imageID)
	if err != nil {
		s.logger.Error("delete task image", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "изображение не найдено")
		return
	}
	_ = os.Remove(s.taskImagePath(taskID, imageID))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
