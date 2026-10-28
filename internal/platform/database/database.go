// Package database mengurus koneksi & migrasi SQLite.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"time"

	_ "modernc.org/sqlite" // driver "sqlite" (pure Go, tanpa cgo)
)

// Open membuka database SQLite di path tertentu dan memastikan bisa dihubungi.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	// PRAGMA diset lewat DSN supaya berlaku di SETIAP koneksi dalam pool.
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")  // pembaca tidak memblokir penulis
	q.Add("_pragma", "busy_timeout(5000)") // tunggu 5 detik jika DB terkunci
	dsn := "file:" + path + "?" + q.Encode()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	// SQLite hanya mengizinkan satu penulis dalam satu waktu.
	// Pool kecil sudah cukup dan mengurangi error "database is locked".
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}

// migrations: tiap elemen = satu versi skema. Hanya boleh DITAMBAH, jangan diubah.
var migrations = []string{
	// v1
	`CREATE TABLE todos (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		title       TEXT    NOT NULL,
		description TEXT    NOT NULL DEFAULT '',
		completed   INTEGER NOT NULL DEFAULT 0,
		created_at  TEXT    NOT NULL,
		updated_at  TEXT    NOT NULL
	)`,
	// v2
	`CREATE INDEX idx_todos_completed ON todos (completed)`,
}

// Migrate menjalankan migrasi yang belum pernah diterapkan.
// Mengembalikan jumlah migrasi yang baru dijalankan.
func Migrate(ctx context.Context, db *sql.DB) (int, error) {
	if _, err := db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`); err != nil {
		return 0, fmt.Errorf("buat tabel schema_migrations: %w", err)
	}

	var current int
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return 0, fmt.Errorf("baca versi skema: %w", err)
	}

	applied := 0
	for i, stmt := range migrations {
		version := i + 1
		if version <= current {
			continue
		}
		if err := applyMigration(ctx, db, version, stmt); err != nil {
			return applied, err
		}
		applied++
	}
	return applied, nil
}

func applyMigration(ctx context.Context, db *sql.DB, version int, stmt string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // tidak berefek jika Commit sudah sukses

	if _, err := tx.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("migrasi v%d: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		version, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("catat migrasi v%d: %w", version, err)
	}
	return tx.Commit()
}
