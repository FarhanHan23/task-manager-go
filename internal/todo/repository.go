package todo

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// Repository adalah kontrak penyimpanan todo.
// Service hanya bergantung pada interface ini, bukan pada SQLite.
type Repository interface {
	Create(ctx context.Context, t *Todo) error
	GetByID(ctx context.Context, id int64) (Todo, error)
	List(ctx context.Context, f ListFilter) ([]Todo, error)
	Update(ctx context.Context, t Todo) error
	Delete(ctx context.Context, id int64) error
}

// ======================= Implementasi SQLite =======================

// Waktu disimpan sebagai TEXT RFC3339 (UTC) agar portabel & mudah dibaca.
const timeLayout = time.RFC3339Nano

type SQLiteRepository struct {
	db *sql.DB
}

func NewSQLiteRepository(db *sql.DB) *SQLiteRepository {
	return &SQLiteRepository{db: db}
}

func (r *SQLiteRepository) Create(ctx context.Context, t *Todo) error {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO todos (title, description, completed, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		t.Title, t.Description, t.Completed,
		t.CreatedAt.UTC().Format(timeLayout), t.UpdatedAt.UTC().Format(timeLayout))
	if err != nil {
		return fmt.Errorf("insert todo: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("last insert id: %w", err)
	}
	t.ID = id
	return nil
}

// scanner adalah interface kecil yang dipenuhi *sql.Row dan *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanTodo(s scanner) (Todo, error) {
	var (
		t                Todo
		created, updated string
	)
	if err := s.Scan(&t.ID, &t.Title, &t.Description, &t.Completed, &created, &updated); err != nil {
		return Todo{}, err
	}
	var err error
	if t.CreatedAt, err = time.Parse(timeLayout, created); err != nil {
		return Todo{}, fmt.Errorf("parse created_at: %w", err)
	}
	if t.UpdatedAt, err = time.Parse(timeLayout, updated); err != nil {
		return Todo{}, fmt.Errorf("parse updated_at: %w", err)
	}
	return t, nil
}

const selectColumns = `SELECT id, title, description, completed, created_at, updated_at FROM todos`

func (r *SQLiteRepository) GetByID(ctx context.Context, id int64) (Todo, error) {
	row := r.db.QueryRowContext(ctx, selectColumns+` WHERE id = ?`, id)
	t, err := scanTodo(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Todo{}, ErrNotFound
	}
	if err != nil {
		return Todo{}, fmt.Errorf("get todo %d: %w", id, err)
	}
	return t, nil
}

func (r *SQLiteRepository) List(ctx context.Context, f ListFilter) ([]Todo, error) {
	var (
		sb   strings.Builder
		args []any
	)
	sb.WriteString(selectColumns)
	if f.Completed != nil {
		// Yang disambung hanya teks SQL tetap; NILAI tetap lewat placeholder.
		sb.WriteString(` WHERE completed = ?`)
		args = append(args, *f.Completed)
	}
	sb.WriteString(` ORDER BY id LIMIT ? OFFSET ?`)
	args = append(args, f.Limit, f.Offset)

	rows, err := r.db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list todos: %w", err)
	}
	defer rows.Close()

	todos := []Todo{} // bukan nil, agar JSON-nya [] bukan null
	for rows.Next() {
		t, err := scanTodo(rows)
		if err != nil {
			return nil, fmt.Errorf("scan todo: %w", err)
		}
		todos = append(todos, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterasi todos: %w", err)
	}
	return todos, nil
}

func (r *SQLiteRepository) Update(ctx context.Context, t Todo) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE todos SET title = ?, description = ?, completed = ?, updated_at = ?
		 WHERE id = ?`,
		t.Title, t.Description, t.Completed, t.UpdatedAt.UTC().Format(timeLayout), t.ID)
	if err != nil {
		return fmt.Errorf("update todo %d: %w", t.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLiteRepository) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM todos WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete todo %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ======================= Implementasi In-Memory =======================

// MemoryRepository menyimpan todo di map. Cocok untuk test & prototipe.
// Aman dipakai banyak goroutine berkat mutex.
type MemoryRepository struct {
	mu     sync.RWMutex
	nextID int64
	items  map[int64]Todo
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{items: make(map[int64]Todo)}
}

func (r *MemoryRepository) Create(_ context.Context, t *Todo) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	t.ID = r.nextID
	r.items[t.ID] = *t
	return nil
}

func (r *MemoryRepository) GetByID(_ context.Context, id int64) (Todo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.items[id]
	if !ok {
		return Todo{}, ErrNotFound
	}
	return t, nil
}

func (r *MemoryRepository) List(_ context.Context, f ListFilter) ([]Todo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	todos := []Todo{}
	for _, t := range r.items {
		if f.Completed != nil && t.Completed != *f.Completed {
			continue
		}
		todos = append(todos, t)
	}
	slices.SortFunc(todos, func(a, b Todo) int { return cmp.Compare(a.ID, b.ID) })

	start := min(f.Offset, len(todos))
	end := min(start+f.Limit, len(todos))
	return todos[start:end], nil
}

func (r *MemoryRepository) Update(_ context.Context, t Todo) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[t.ID]; !ok {
		return ErrNotFound
	}
	r.items[t.ID] = t
	return nil
}

func (r *MemoryRepository) Delete(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[id]; !ok {
		return ErrNotFound
	}
	delete(r.items, id)
	return nil
}

// Pastikan (saat compile) kedua tipe memenuhi interface Repository.
var (
	_ Repository = (*SQLiteRepository)(nil)
	_ Repository = (*MemoryRepository)(nil)
)
