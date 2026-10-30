// Package web berisi helper HTTP yang dipakai bersama: JSON & middleware.
package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// ErrorBody adalah format error JSON yang KONSISTEN untuk semua endpoint:
//
//	{"error": {"code": "not_found", "message": "...", "details": {...}}}
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
}

// WriteJSON menulis respons JSON dengan status code tertentu.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("gagal menulis respons JSON", "err", err)
	}
}

// WriteError menulis error dalam format ErrorBody.
func WriteError(w http.ResponseWriter, status int, code, msg string, details map[string]string) {
	WriteJSON(w, status, ErrorBody{Error: ErrorDetail{Code: code, Message: msg, Details: details}})
}

const maxBodyBytes = 1 << 20 // 1 MB

// DecodeJSON membaca body request ke dst dengan aturan ketat:
// ukuran dibatasi, field tak dikenal ditolak, dan hanya boleh satu objek JSON.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var (
			syntaxErr   *json.SyntaxError
			typeErr     *json.UnmarshalTypeError
			maxBytesErr *http.MaxBytesError
		)
		switch {
		case errors.As(err, &syntaxErr):
			return fmt.Errorf("JSON rusak di posisi %d", syntaxErr.Offset)
		case errors.Is(err, io.ErrUnexpectedEOF):
			return errors.New("JSON tidak lengkap")
		case errors.As(err, &typeErr):
			return fmt.Errorf("tipe salah untuk field %q", typeErr.Field)
		case strings.HasPrefix(err.Error(), "json: unknown field "):
			field := strings.TrimPrefix(err.Error(), "json: unknown field ")
			return fmt.Errorf("field tidak dikenal %s", field)
		case errors.Is(err, io.EOF):
			return errors.New("body tidak boleh kosong")
		case errors.As(err, &maxBytesErr):
			return fmt.Errorf("body maksimal %d byte", maxBytesErr.Limit)
		default:
			return err
		}
	}
	// Pastikan tidak ada data sisa setelah objek pertama.
	if dec.More() {
		return errors.New("body hanya boleh berisi satu objek JSON")
	}
	return nil
}

// ======================= Middleware =======================

// statusRecorder membungkus ResponseWriter untuk mencatat status code & ukuran.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Logging mencatat setiap request dengan slog.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"bytes", rec.bytes,
				"duration", time.Since(start).String(),
			)
		})
	}
}

// Recover menangkap panic di handler agar server tidak mati,
// lalu membalas 500 dengan format error standar.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					// http.ErrAbortHandler adalah panic "resmi" milik net/http: teruskan.
					if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
						panic(v)
					}
					logger.Error("panic ditangkap",
						"panic", fmt.Sprint(v),
						"stack", string(debug.Stack()),
					)
					w.Header().Set("Connection", "close")
					WriteError(w, http.StatusInternalServerError, "internal_error",
						"terjadi kesalahan pada server", nil)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Chain menyusun middleware: Chain(h, a, b) == a(b(h)).
// Middleware pertama di daftar adalah yang paling luar.
func Chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
