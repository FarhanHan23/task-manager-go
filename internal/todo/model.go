package todo

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Batas panjang field — dipakai oleh validasi.
const (
	MaxTitleLen       = 100
	MaxDescriptionLen = 1000
)

// ErrNotFound dikembalikan ketika todo dengan id tertentu tidak ada.
var ErrNotFound = errors.New("todo tidak ditemukan")

// Todo adalah entitas utama aplikasi kita.
type Todo struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Completed   bool      `json:"completed"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateInput adalah data yang dikirim client saat membuat todo.
type CreateInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// UpdateInput memakai pointer agar kita bisa membedakan
// "field tidak dikirim" (nil) dengan "field dikirim bernilai kosong".
type UpdateInput struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Completed   *bool   `json:"completed"`
}

// Nilai default & batas untuk paginasi.
const (
	DefaultLimit = 20
	MaxLimit     = 100
)

// ListFilter berisi parameter query untuk daftar todo.
type ListFilter struct {
	Completed *bool // nil = semua
	Limit     int
	Offset    int
}

// Normalize memberi nilai default & membatasi limit/offset ke rentang aman.
func (f *ListFilter) Normalize() {
	if f.Limit <= 0 {
		f.Limit = DefaultLimit
	}
	f.Limit = min(f.Limit, MaxLimit)
	f.Offset = max(f.Offset, 0)
}

// ValidationError menampung pesan error per field.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Fields))
	for _, k := range slices.Sorted(maps.Keys(e.Fields)) {
		parts = append(parts, fmt.Sprintf("%s: %s", k, e.Fields[k]))
	}
	return "input tidak valid: " + strings.Join(parts, "; ")
}

// validator kecil untuk mengumpulkan banyak error sekaligus.
type validator struct {
	fields map[string]string
}

func (v *validator) check(ok bool, field, msg string) {
	if ok {
		return
	}
	if v.fields == nil {
		v.fields = make(map[string]string)
	}
	if _, exists := v.fields[field]; !exists { // simpan error pertama saja
		v.fields[field] = msg
	}
}

func (v *validator) err() error {
	if len(v.fields) == 0 {
		return nil
	}
	return &ValidationError{Fields: v.fields}
}

// Normalize merapikan input (trim spasi) sebelum divalidasi.
func (in *CreateInput) Normalize() {
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
}

// Validate memeriksa aturan bisnis untuk CreateInput.
func (in CreateInput) Validate() error {
	var v validator
	v.check(in.Title != "", "title", "wajib diisi")
	v.check(utf8.RuneCountInString(in.Title) <= MaxTitleLen, "title",
		fmt.Sprintf("maksimal %d karakter", MaxTitleLen))
	v.check(utf8.RuneCountInString(in.Description) <= MaxDescriptionLen, "description",
		fmt.Sprintf("maksimal %d karakter", MaxDescriptionLen))
	return v.err()
}

// Normalize merapikan field yang dikirim.
func (in *UpdateInput) Normalize() {
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		in.Title = &t
	}
	if in.Description != nil {
		d := strings.TrimSpace(*in.Description)
		in.Description = &d
	}
}

// Validate memeriksa aturan bisnis untuk UpdateInput.
func (in UpdateInput) Validate() error {
	var v validator
	v.check(in.Title != nil || in.Description != nil || in.Completed != nil,
		"body", "minimal satu field harus dikirim")
	if in.Title != nil {
		v.check(*in.Title != "", "title", "tidak boleh kosong")
		v.check(utf8.RuneCountInString(*in.Title) <= MaxTitleLen, "title",
			fmt.Sprintf("maksimal %d karakter", MaxTitleLen))
	}
	if in.Description != nil {
		v.check(utf8.RuneCountInString(*in.Description) <= MaxDescriptionLen, "description",
			fmt.Sprintf("maksimal %d karakter", MaxDescriptionLen))
	}
	return v.err()
}

// Apply menerapkan perubahan ke todo yang sudah ada.
func (in UpdateInput) Apply(t *Todo) {
	if in.Title != nil {
		t.Title = *in.Title
	}
	if in.Description != nil {
		t.Description = *in.Description
	}
	if in.Completed != nil {
		t.Completed = *in.Completed
	}
}
