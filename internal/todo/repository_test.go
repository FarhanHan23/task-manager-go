package todo

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/FarhanHan23/task-manager-go/internal/platform/database"
)

// Test ini memakai SQLite SUNGGUHAN (file sementara) untuk memastikan query SQL benar.
func TestSQLiteRepository(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	repo := NewSQLiteRepository(db)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	td := Todo{Title: "Uji SQLite", CreatedAt: now, UpdatedAt: now}
	if err := repo.Create(ctx, &td); err != nil {
		t.Fatal(err)
	}
	if td.ID != 1 {
		t.Fatalf("ID = %d, mau 1", td.ID)
	}

	got, err := repo.GetByID(ctx, td.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != td { // Todo hanya berisi field comparable, jadi bisa pakai ==
		t.Errorf("GetByID = %+v, mau %+v", got, td)
	}

	td.Completed = true
	if err := repo.Update(ctx, td); err != nil {
		t.Fatal(err)
	}
	done := true
	list, err := repo.List(ctx, ListFilter{Completed: &done, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || !list[0].Completed {
		t.Errorf("List(completed=true) = %+v", list)
	}

	if err := repo.Delete(ctx, td.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetByID(ctx, td.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("setelah Delete, err = %v, mau ErrNotFound", err)
	}
	if err := repo.Delete(ctx, td.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete kedua, err = %v, mau ErrNotFound", err)
	}
}
