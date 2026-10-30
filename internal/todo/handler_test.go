package todo

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FarhanHan23/task-manager-go/internal/platform/web"
)

// newTestServer merakit handler dengan repository in-memory & jam palsu.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	repo := NewMemoryRepository()
	svc := NewService(repo)
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	svc.now = func() time.Time { return fixed }

	logger := slog.New(slog.NewTextHandler(io.Discard, nil)) // log dibuang saat test
	mux := http.NewServeMux()
	NewHandler(svc, logger).RegisterRoutes(mux)

	srv := httptest.NewServer(web.Chain(mux, web.Recover(logger), web.Logging(logger)))
	t.Cleanup(srv.Close)
	return srv
}

// doRequest mengirim request dan mengembalikan status + body.
func doRequest(t *testing.T, method, url, body string) (int, []byte) {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, b
}

func decode[T any](t *testing.T, b []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("respons bukan JSON valid: %v\nbody: %s", err, b)
	}
	return v
}

func TestCreateTodo(t *testing.T) {
	srv := newTestServer(t)

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string // kode error; kosong jika sukses
	}{
		{"valid", `{"title":"Belajar Go","description":"modul 26"}`, http.StatusCreated, ""},
		{"title kosong", `{"title":"   "}`, http.StatusUnprocessableEntity, "validation_error"},
		{"title terlalu panjang", `{"title":"` + strings.Repeat("a", 101) + `"}`, http.StatusUnprocessableEntity, "validation_error"},
		{"JSON rusak", `{"title":`, http.StatusBadRequest, "bad_request"},
		{"field tak dikenal", `{"title":"x","prioritas":1}`, http.StatusBadRequest, "bad_request"},
		{"tipe salah", `{"title":123}`, http.StatusBadRequest, "bad_request"},
		{"body kosong", ``, http.StatusBadRequest, "bad_request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := doRequest(t, http.MethodPost, srv.URL+"/todos", tt.body)
			if status != tt.wantStatus {
				t.Fatalf("status = %d, mau %d; body: %s", status, tt.wantStatus, body)
			}
			if tt.wantCode != "" {
				e := decode[web.ErrorBody](t, body)
				if e.Error.Code != tt.wantCode {
					t.Errorf("code = %q, mau %q", e.Error.Code, tt.wantCode)
				}
				return
			}
			got := decode[Todo](t, body)
			if got.ID == 0 || got.Completed || got.CreatedAt.IsZero() {
				t.Errorf("todo tidak sesuai: %+v", got)
			}
		})
	}
}

func TestCreateTodo_TrimTitle(t *testing.T) {
	srv := newTestServer(t)
	_, body := doRequest(t, http.MethodPost, srv.URL+"/todos", `{"title":"  Rapi  "}`)
	if got := decode[Todo](t, body); got.Title != "Rapi" {
		t.Errorf("title = %q, mau %q", got.Title, "Rapi")
	}
}

func TestGetTodo(t *testing.T) {
	srv := newTestServer(t)
	doRequest(t, http.MethodPost, srv.URL+"/todos", `{"title":"Satu"}`)

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{"ada", "/todos/1", http.StatusOK},
		{"tidak ada", "/todos/99", http.StatusNotFound},
		{"id bukan angka", "/todos/abc", http.StatusBadRequest},
		{"id negatif", "/todos/-1", http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := doRequest(t, http.MethodGet, srv.URL+tt.path, "")
			if status != tt.wantStatus {
				t.Fatalf("status = %d, mau %d; body: %s", status, tt.wantStatus, body)
			}
		})
	}
}

func TestUpdateTodo(t *testing.T) {
	srv := newTestServer(t)
	doRequest(t, http.MethodPost, srv.URL+"/todos", `{"title":"Lama","description":"desc"}`)

	status, body := doRequest(t, http.MethodPatch, srv.URL+"/todos/1", `{"completed":true}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d; body: %s", status, body)
	}
	got := decode[Todo](t, body)
	if !got.Completed || got.Title != "Lama" || got.Description != "desc" {
		t.Errorf("hanya completed yang boleh berubah, dapat: %+v", got)
	}

	// Body tanpa field apa pun -> 422
	status, _ = doRequest(t, http.MethodPatch, srv.URL+"/todos/1", `{}`)
	if status != http.StatusUnprocessableEntity {
		t.Errorf("PATCH kosong: status = %d, mau 422", status)
	}

	// Todo yang tidak ada -> 404
	status, _ = doRequest(t, http.MethodPatch, srv.URL+"/todos/42", `{"title":"x"}`)
	if status != http.StatusNotFound {
		t.Errorf("PATCH id 42: status = %d, mau 404", status)
	}
}

func TestDeleteTodo(t *testing.T) {
	srv := newTestServer(t)
	doRequest(t, http.MethodPost, srv.URL+"/todos", `{"title":"Hapus aku"}`)

	status, _ := doRequest(t, http.MethodDelete, srv.URL+"/todos/1", "")
	if status != http.StatusNoContent {
		t.Fatalf("DELETE pertama: status = %d, mau 204", status)
	}
	status, _ = doRequest(t, http.MethodDelete, srv.URL+"/todos/1", "")
	if status != http.StatusNotFound {
		t.Fatalf("DELETE kedua: status = %d, mau 404", status)
	}
}

func TestListTodos(t *testing.T) {
	srv := newTestServer(t)
	for _, title := range []string{"A", "B", "C", "D"} {
		doRequest(t, http.MethodPost, srv.URL+"/todos", `{"title":"`+title+`"}`)
	}
	doRequest(t, http.MethodPatch, srv.URL+"/todos/2", `{"completed":true}`)

	type listResp struct {
		Data   []Todo `json:"data"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
	}

	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantTitles []string
	}{
		{"semua", "", http.StatusOK, []string{"A", "B", "C", "D"}},
		{"selesai saja", "?completed=true", http.StatusOK, []string{"B"}},
		{"belum selesai", "?completed=false", http.StatusOK, []string{"A", "C", "D"}},
		{"paginasi", "?limit=2&offset=1", http.StatusOK, []string{"B", "C"}},
		{"offset kelewatan", "?offset=10", http.StatusOK, []string{}},
		{"completed tidak valid", "?completed=mungkin", http.StatusBadRequest, nil},
		{"limit bukan angka", "?limit=banyak", http.StatusBadRequest, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := doRequest(t, http.MethodGet, srv.URL+"/todos"+tt.query, "")
			if status != tt.wantStatus {
				t.Fatalf("status = %d, mau %d; body: %s", status, tt.wantStatus, body)
			}
			if tt.wantTitles == nil {
				return
			}
			got := decode[listResp](t, body)
			titles := []string{}
			for _, td := range got.Data {
				titles = append(titles, td.Title)
			}
			if strings.Join(titles, ",") != strings.Join(tt.wantTitles, ",") {
				t.Errorf("titles = %v, mau %v", titles, tt.wantTitles)
			}
		})
	}
}

func TestMethodNotAllowed(t *testing.T) {
	srv := newTestServer(t)
	// ServeMux Go 1.22+ otomatis membalas 405 untuk method yang tidak terdaftar.
	status, _ := doRequest(t, http.MethodPut, srv.URL+"/todos/1", `{}`)
	if status != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, mau 405", status)
	}
}

func TestRecoverMiddleware(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	panicky := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("ups!")
	})
	h := web.Recover(logger)(panicky)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, mau 500", rec.Code)
	}
	e := decode[web.ErrorBody](t, rec.Body.Bytes())
	if e.Error.Code != "internal_error" {
		t.Errorf("code = %q, mau internal_error", e.Error.Code)
	}
}
