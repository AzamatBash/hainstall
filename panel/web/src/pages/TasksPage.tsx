import { FormEvent, useCallback, useEffect, useMemo, useRef, useState, type ClipboardEvent, type DragEvent } from 'react'
import {
  api,
  RemnaPanel,
  setToken,
  Task,
  TaskStatus,
  translateError,
} from '../api'
import { withBase } from '../basePath'
import BrandNav from '../components/BrandNav'

const COLUMNS: { status: TaskStatus; label: string }[] = [
  { status: 'todo', label: 'Новая' },
  { status: 'doing', label: 'В работе' },
  { status: 'done', label: 'Готово' },
]

interface PendingImage {
  id: string
  mime: string
  dataBase64: string
  previewUrl: string
}

function fileToBase64(file: Blob): Promise<{ mime: string; dataBase64: string }> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      const result = String(reader.result || '')
      const comma = result.indexOf(',')
      const dataBase64 = comma >= 0 ? result.slice(comma + 1) : result
      resolve({ mime: file.type || 'application/octet-stream', dataBase64 })
    }
    reader.onerror = () => reject(reader.error || new Error('read failed'))
    reader.readAsDataURL(file)
  })
}

function previewText(description: string, max = 120): string {
  const t = description.replace(/\s+/g, ' ').trim()
  if (t.length <= max) return t
  return t.slice(0, max - 1) + '…'
}

export default function TasksPage() {
  const [tasks, setTasks] = useState<Task[]>([])
  const [panels, setPanels] = useState<RemnaPanel[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [viewTask, setViewTask] = useState<Task | null>(null)
  const [viewLoading, setViewLoading] = useState(false)
  const [dragOverStatus, setDragOverStatus] = useState<TaskStatus | null>(null)
  const draggingId = useRef<string | null>(null)

  const load = useCallback(async () => {
    setError('')
    try {
      const [tasksRes, panelsRes] = await Promise.all([
        api<{ tasks?: Task[] } | Task[]>('/api/tasks'),
        api<{ panels?: RemnaPanel[] }>('/api/remna-panels'),
      ])
      const list = Array.isArray(tasksRes)
        ? tasksRes
        : Array.isArray(tasksRes.tasks)
          ? tasksRes.tasks
          : []
      setTasks(list)
      setPanels(Array.isArray(panelsRes.panels) ? panelsRes.panels : [])
    } catch (err) {
      setError(err instanceof Error ? err.message : translateError(String(err)))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const byStatus = useMemo(() => {
    const map: Record<TaskStatus, Task[]> = { todo: [], doing: [], done: [] }
    for (const t of tasks) {
      const s = (COLUMNS.some((c) => c.status === t.status) ? t.status : 'todo') as TaskStatus
      map[s].push(t)
    }
    for (const s of Object.keys(map) as TaskStatus[]) {
      map[s].sort((a, b) => String(b.updated_at).localeCompare(String(a.updated_at)))
    }
    return map
  }, [tasks])

  async function logout() {
    try {
      await api('/api/auth/logout', { method: 'POST' })
    } catch {
      /* ignore */
    }
    setToken(null)
    window.location.href = withBase('/login')
  }

  async function openTask(id: string) {
    setViewLoading(true)
    setError('')
    try {
      const res = await api<Task | { task: Task }>(`/api/tasks/${encodeURIComponent(id)}`)
      const task = 'task' in res && res.task ? res.task : (res as Task)
      setViewTask(task)
    } catch (err) {
      setError(err instanceof Error ? err.message : translateError(String(err)))
    } finally {
      setViewLoading(false)
    }
  }

  async function patchStatus(id: string, status: TaskStatus) {
    setError('')
    const prev = tasks
    setTasks((list) =>
      list.map((t) => (t.id === id ? { ...t, status, updated_at: new Date().toISOString() } : t)),
    )
    if (viewTask?.id === id) {
      setViewTask((t) => (t ? { ...t, status } : t))
    }
    try {
      await api(`/api/tasks/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        body: JSON.stringify({ status }),
      })
    } catch (err) {
      setTasks(prev)
      setError(err instanceof Error ? err.message : translateError(String(err)))
      void load()
    }
  }

  async function deleteTask(id: string) {
    if (!window.confirm('Удалить задачу?')) return
    setError('')
    try {
      await api(`/api/tasks/${encodeURIComponent(id)}`, { method: 'DELETE' })
      setTasks((list) => list.filter((t) => t.id !== id))
      if (viewTask?.id === id) setViewTask(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : translateError(String(err)))
    }
  }

  function onDragStart(e: DragEvent, id: string) {
    draggingId.current = id
    e.dataTransfer.setData('text/plain', id)
    e.dataTransfer.effectAllowed = 'move'
  }

  function onDragEnd() {
    draggingId.current = null
    setDragOverStatus(null)
  }

  function onColumnDragOver(e: DragEvent, status: TaskStatus) {
    e.preventDefault()
    e.dataTransfer.dropEffect = 'move'
    setDragOverStatus(status)
  }

  function onColumnDrop(e: DragEvent, status: TaskStatus) {
    e.preventDefault()
    setDragOverStatus(null)
    const id = e.dataTransfer.getData('text/plain') || draggingId.current
    draggingId.current = null
    if (!id) return
    const task = tasks.find((t) => t.id === id)
    if (!task || task.status === status) return
    void patchStatus(id, status)
  }

  return (
    <div className="shell">
      <header className="topbar">
        <BrandNav active="tasks" />
        <div className="row">
          <button
            className="btn btn-sm btn-primary"
            type="button"
            onClick={() => setShowCreate(true)}
          >
            Новая задача
          </button>
          <button className="btn btn-sm btn-ghost" type="button" onClick={() => void logout()}>
            Выйти
          </button>
        </div>
      </header>

      {error && <p className="error">{error}</p>}
      {loading && <p className="muted">Загрузка…</p>}

      {!loading && (
        <div className="kanban">
          {COLUMNS.map((col) => (
            <section
              key={col.status}
              className={`kanban-col${dragOverStatus === col.status ? ' drag-over' : ''}`}
              onDragOver={(e) => onColumnDragOver(e, col.status)}
              onDragLeave={() => setDragOverStatus((s) => (s === col.status ? null : s))}
              onDrop={(e) => onColumnDrop(e, col.status)}
            >
              <header className="kanban-col-head">
                <h2>{col.label}</h2>
                <span className="muted">{byStatus[col.status].length}</span>
              </header>
              <div className="kanban-col-body">
                {byStatus[col.status].map((task) => (
                  <article
                    key={task.id}
                    className="kanban-card"
                    draggable
                    onDragStart={(e) => onDragStart(e, task.id)}
                    onDragEnd={onDragEnd}
                    onClick={() => void openTask(task.id)}
                    role="button"
                    tabIndex={0}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault()
                        void openTask(task.id)
                      }
                    }}
                  >
                    <p className="kanban-card-desc">{previewText(task.description)}</p>
                    <div className="kanban-card-meta">
                      {task.remna_panel_name ? (
                        <span className="kanban-panel-badge">{task.remna_panel_name}</span>
                      ) : null}
                      {(task.images?.length ?? 0) > 0 ? (
                        <span className="muted kanban-img-count">
                          {task.images!.length} фото
                        </span>
                      ) : null}
                    </div>
                  </article>
                ))}
                {byStatus[col.status].length === 0 && (
                  <p className="muted kanban-empty">Пусто</p>
                )}
              </div>
            </section>
          ))}
        </div>
      )}

      {showCreate && (
        <CreateTaskModal
          panels={panels}
          onClose={() => setShowCreate(false)}
          onCreated={(task) => {
            setTasks((list) => [task, ...list])
            setShowCreate(false)
          }}
          onError={(msg) => setError(msg)}
        />
      )}

      {(viewTask || viewLoading) && (
        <ViewTaskModal
          task={viewTask}
          panels={panels}
          loading={viewLoading}
          onClose={() => setViewTask(null)}
          onStatus={(status) => {
            if (viewTask) void patchStatus(viewTask.id, status)
          }}
          onDelete={() => {
            if (viewTask) void deleteTask(viewTask.id)
          }}
          onSaved={(task) => {
            setViewTask(task)
            setTasks((list) => list.map((t) => (t.id === task.id ? { ...t, ...task } : t)))
          }}
          onError={(msg) => setError(msg)}
        />
      )}
    </div>
  )
}

function CreateTaskModal({
  panels,
  onClose,
  onCreated,
  onError,
}: {
  panels: RemnaPanel[]
  onClose: () => void
  onCreated: (task: Task) => void
  onError: (msg: string) => void
}) {
  const [panelId, setPanelId] = useState(panels[0]?.id || '')
  const [description, setDescription] = useState('')
  const [pending, setPending] = useState<PendingImage[]>([])
  const [busy, setBusy] = useState(false)
  const [localError, setLocalError] = useState('')
  const pendingRef = useRef(pending)
  pendingRef.current = pending

  useEffect(() => {
    return () => {
      for (const img of pendingRef.current) URL.revokeObjectURL(img.previewUrl)
    }
  }, [])

  async function addImageFiles(files: File[]) {
    const images = files.filter((f) => f.type.startsWith('image/'))
    if (!images.length) return
    const next: PendingImage[] = []
    for (const file of images) {
      try {
        const { mime, dataBase64 } = await fileToBase64(file)
        next.push({
          id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
          mime,
          dataBase64,
          previewUrl: URL.createObjectURL(file),
        })
      } catch {
        /* skip */
      }
    }
    if (next.length) setPending((list) => [...list, ...next])
  }

  function onPaste(e: ClipboardEvent) {
    const items = e.clipboardData?.items
    if (!items) return
    const files: File[] = []
    for (const item of Array.from(items)) {
      if (item.kind === 'file' && item.type.startsWith('image/')) {
        const f = item.getAsFile()
        if (f) files.push(f)
      }
    }
    if (files.length) {
      e.preventDefault()
      void addImageFiles(files)
    }
  }

  function removePending(id: string) {
    setPending((list) => {
      const found = list.find((i) => i.id === id)
      if (found) URL.revokeObjectURL(found.previewUrl)
      return list.filter((i) => i.id !== id)
    })
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    if (!panelId.trim()) {
      setLocalError('Выберите панель Remnawave')
      return
    }
    if (!description.trim()) {
      setLocalError('Введите описание')
      return
    }
    setBusy(true)
    setLocalError('')
    try {
      const res = await api<Task | { task: Task }>('/api/tasks', {
        method: 'POST',
        body: JSON.stringify({
          remna_panel_id: panelId.trim(),
          description: description.trim(),
          status: 'todo',
        }),
      })
      let task = 'task' in res && res.task ? res.task : (res as Task)
      if (pending.length) {
        const uploaded = []
        for (const img of pending) {
          const imgRes = await api<
            | { id: string; mime: string; url: string }
            | { image: { id: string; mime: string; url: string } }
          >(`/api/tasks/${encodeURIComponent(task.id)}/images`, {
            method: 'POST',
            body: JSON.stringify({ mime: img.mime, data_base64: img.dataBase64 }),
          })
          const image =
            'image' in imgRes && imgRes.image
              ? imgRes.image
              : (imgRes as { id: string; mime: string; url: string })
          uploaded.push(image)
        }
        task = { ...task, images: [...(task.images || []), ...uploaded] }
      }
      const panelName = panels.find((p) => p.id === panelId)?.name
      if (panelName && !task.remna_panel_name) {
        task = { ...task, remna_panel_name: panelName }
      }
      onCreated(task)
    } catch (err) {
      const msg = err instanceof Error ? err.message : translateError(String(err))
      setLocalError(msg)
      onError(msg)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <form
        className="modal stack"
        onClick={(e) => e.stopPropagation()}
        onSubmit={(e) => void onSubmit(e)}
        onPaste={onPaste}
      >
        <h2 style={{ margin: 0 }}>Новая задача</h2>
        <div className="field">
          <label htmlFor="task-panel">Панель Remnawave</label>
          <select
            id="task-panel"
            value={panelId}
            onChange={(e) => setPanelId(e.target.value)}
            required
          >
            <option value="" disabled>
              Выберите панель
            </option>
            {panels.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="task-desc">Описание</label>
          <textarea
            id="task-desc"
            rows={5}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Текст задачи… Можно вставить скриншот (Ctrl+V)"
            required
          />
        </div>
        {pending.length > 0 && (
          <div className="task-images">
            {pending.map((img) => (
              <div key={img.id} className="task-image-thumb">
                <img src={img.previewUrl} alt="" />
                <button
                  type="button"
                  className="task-image-remove"
                  aria-label="Убрать"
                  onClick={() => removePending(img.id)}
                >
                  ×
                </button>
              </div>
            ))}
          </div>
        )}
        <p className="muted" style={{ margin: 0, fontSize: '0.8rem' }}>
          Ctrl+V — вставить изображение из буфера
        </p>
        {localError && <p className="error">{localError}</p>}
        <div className="modal-actions">
          <button className="btn btn-ghost" type="button" onClick={onClose} disabled={busy}>
            Отмена
          </button>
          <button className="btn btn-primary" type="submit" disabled={busy || !panels.length}>
            {busy ? 'Создание…' : 'Создать'}
          </button>
        </div>
      </form>
    </div>
  )
}

function ViewTaskModal({
  task,
  panels,
  loading,
  onClose,
  onStatus,
  onDelete,
  onSaved,
  onError,
}: {
  task: Task | null
  panels: RemnaPanel[]
  loading: boolean
  onClose: () => void
  onStatus: (status: TaskStatus) => void
  onDelete: () => void
  onSaved: (task: Task) => void
  onError: (msg: string) => void
}) {
  const [editing, setEditing] = useState(false)
  const [panelId, setPanelId] = useState('')
  const [description, setDescription] = useState('')
  const [images, setImages] = useState<Task['images']>([])
  const [pending, setPending] = useState<PendingImage[]>([])
  const [busy, setBusy] = useState(false)
  const [localError, setLocalError] = useState('')
  const [lightbox, setLightbox] = useState<string | null>(null)
  const pendingRef = useRef(pending)
  pendingRef.current = pending

  useEffect(() => {
    if (!task) return
    setEditing(false)
    setPanelId(task.remna_panel_id || '')
    setDescription(task.description || '')
    setImages(task.images || [])
    setPending([])
    setLocalError('')
    setLightbox(null)
  }, [task?.id, task?.updated_at])

  useEffect(() => {
    return () => {
      for (const img of pendingRef.current) URL.revokeObjectURL(img.previewUrl)
    }
  }, [])

  async function addImageFiles(files: File[]) {
    const list = files.filter((f) => f.type.startsWith('image/'))
    if (!list.length) return
    const next: PendingImage[] = []
    for (const file of list) {
      try {
        const { mime, dataBase64 } = await fileToBase64(file)
        next.push({
          id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
          mime,
          dataBase64,
          previewUrl: URL.createObjectURL(file),
        })
      } catch {
        /* skip */
      }
    }
    if (next.length) setPending((cur) => [...cur, ...next])
  }

  function onPaste(e: ClipboardEvent) {
    if (!editing) return
    const items = e.clipboardData?.items
    if (!items) return
    const files: File[] = []
    for (const item of Array.from(items)) {
      if (item.kind === 'file' && item.type.startsWith('image/')) {
        const f = item.getAsFile()
        if (f) files.push(f)
      }
    }
    if (files.length) {
      e.preventDefault()
      void addImageFiles(files)
    }
  }

  function removePending(id: string) {
    setPending((list) => {
      const found = list.find((i) => i.id === id)
      if (found) URL.revokeObjectURL(found.previewUrl)
      return list.filter((i) => i.id !== id)
    })
  }

  async function removeExistingImage(imageId: string) {
    if (!task) return
    setBusy(true)
    setLocalError('')
    try {
      await api(`/api/tasks/${encodeURIComponent(task.id)}/images/${encodeURIComponent(imageId)}`, {
        method: 'DELETE',
      })
      const nextImages = (images || []).filter((i) => i.id !== imageId)
      setImages(nextImages)
      onSaved({ ...task, images: nextImages })
    } catch (err) {
      setLocalError(err instanceof Error ? err.message : translateError(String(err)))
    } finally {
      setBusy(false)
    }
  }

  async function onSave(e: FormEvent) {
    e.preventDefault()
    if (!task) return
    if (!panelId.trim()) {
      setLocalError('Выберите панель Remnawave')
      return
    }
    if (!description.trim()) {
      setLocalError('Введите описание')
      return
    }
    setBusy(true)
    setLocalError('')
    try {
      const patched = await api<Task>(`/api/tasks/${encodeURIComponent(task.id)}`, {
        method: 'PATCH',
        body: JSON.stringify({
          remna_panel_id: panelId.trim(),
          description: description.trim(),
        }),
      })
      let nextImages = [...(images || [])]
      for (const img of pending) {
        const imgRes = await api<{ id: string; mime: string; url: string }>(
          `/api/tasks/${encodeURIComponent(task.id)}/images`,
          {
            method: 'POST',
            body: JSON.stringify({ mime: img.mime, data_base64: img.dataBase64 }),
          },
        )
        nextImages.push(imgRes)
      }
      for (const img of pending) URL.revokeObjectURL(img.previewUrl)
      setPending([])
      const panelName = panels.find((p) => p.id === panelId)?.name || patched.remna_panel_name
      const saved: Task = {
        ...patched,
        remna_panel_name: panelName,
        images: nextImages,
      }
      setImages(nextImages)
      setEditing(false)
      onSaved(saved)
    } catch (err) {
      const msg = err instanceof Error ? err.message : translateError(String(err))
      setLocalError(msg)
      onError(msg)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div
        className="modal modal-wide stack"
        onClick={(e) => e.stopPropagation()}
        onPaste={onPaste}
      >
        {loading || !task ? (
          <p className="muted">Загрузка…</p>
        ) : editing ? (
          <form className="stack" onSubmit={(e) => void onSave(e)}>
            <div className="row" style={{ justifyContent: 'space-between' }}>
              <h2 style={{ margin: 0 }}>Редактирование</h2>
              {task.remna_panel_name ? (
                <span className="kanban-panel-badge">{task.remna_panel_name}</span>
              ) : null}
            </div>
            <div className="field">
              <label htmlFor="edit-task-panel">Панель Remnawave</label>
              <select
                id="edit-task-panel"
                value={panelId}
                onChange={(e) => setPanelId(e.target.value)}
                required
              >
                <option value="" disabled>
                  Выберите панель
                </option>
                {panels.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </div>
            <div className="field">
              <label htmlFor="edit-task-desc">Описание</label>
              <textarea
                id="edit-task-desc"
                rows={5}
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Текст задачи… Ctrl+V — скриншот"
                required
              />
            </div>
            {(images?.length || pending.length) > 0 && (
              <div className="task-images">
                {(images || []).map((img) => (
                  <div key={img.id} className="task-image-thumb">
                    <button
                      type="button"
                      className="task-image-open"
                      onClick={() => setLightbox(withBase(img.url))}
                      aria-label="Открыть"
                    >
                      <img src={withBase(img.url)} alt="" />
                    </button>
                    <button
                      type="button"
                      className="task-image-remove"
                      aria-label="Убрать"
                      onClick={() => void removeExistingImage(img.id)}
                      disabled={busy}
                    >
                      ×
                    </button>
                  </div>
                ))}
                {pending.map((img) => (
                  <div key={img.id} className="task-image-thumb">
                    <img src={img.previewUrl} alt="" />
                    <button
                      type="button"
                      className="task-image-remove"
                      aria-label="Убрать"
                      onClick={() => removePending(img.id)}
                    >
                      ×
                    </button>
                  </div>
                ))}
              </div>
            )}
            <p className="muted" style={{ margin: 0, fontSize: '0.8rem' }}>
              Ctrl+V — вставить изображение
            </p>
            {localError && <p className="error">{localError}</p>}
            <div className="modal-actions">
              <button
                className="btn btn-ghost"
                type="button"
                disabled={busy}
                onClick={() => {
                  setEditing(false)
                  setPanelId(task.remna_panel_id || '')
                  setDescription(task.description || '')
                  setImages(task.images || [])
                  for (const img of pending) URL.revokeObjectURL(img.previewUrl)
                  setPending([])
                  setLocalError('')
                }}
              >
                Отмена
              </button>
              <button className="btn btn-primary" type="submit" disabled={busy}>
                {busy ? 'Сохранение…' : 'Сохранить'}
              </button>
            </div>
          </form>
        ) : (
          <>
            <div className="row" style={{ justifyContent: 'space-between' }}>
              <h2 style={{ margin: 0 }}>Задача</h2>
              {task.remna_panel_name ? (
                <span className="kanban-panel-badge">{task.remna_panel_name}</span>
              ) : null}
            </div>
            <p className="task-view-desc">{task.description}</p>
            {(images?.length ?? 0) > 0 && (
              <div className="task-images task-images-view">
                {images!.map((img) => (
                  <button
                    key={img.id}
                    type="button"
                    className="task-image-thumb task-image-open"
                    onClick={() => setLightbox(withBase(img.url))}
                    aria-label="Открыть изображение"
                  >
                    <img src={withBase(img.url)} alt="" />
                  </button>
                ))}
              </div>
            )}
            <div className="row" style={{ flexWrap: 'wrap' }}>
              {COLUMNS.map((col) => (
                <button
                  key={col.status}
                  type="button"
                  className={`btn btn-sm${task.status === col.status ? ' btn-primary' : ''}`}
                  disabled={task.status === col.status}
                  onClick={() => onStatus(col.status)}
                >
                  {col.label}
                </button>
              ))}
            </div>
            <div className="modal-actions">
              <button className="btn btn-danger btn-sm" type="button" onClick={onDelete}>
                Удалить
              </button>
              <button className="btn" type="button" onClick={() => setEditing(true)}>
                Изменить
              </button>
              <button className="btn btn-ghost" type="button" onClick={onClose}>
                Закрыть
              </button>
            </div>
          </>
        )}

        {lightbox && (
          <div
            className="task-lightbox"
            onClick={() => setLightbox(null)}
            role="presentation"
          >
            <img
              src={lightbox}
              alt=""
              onClick={(e) => e.stopPropagation()}
            />
            <button
              type="button"
              className="btn btn-sm task-lightbox-close"
              onClick={() => setLightbox(null)}
            >
              Закрыть
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
