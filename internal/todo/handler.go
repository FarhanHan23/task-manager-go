package todo

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/FarhanHan23/task-manager-go/internal/platform/web"
)

// Handler menerjemahkan HTTP <-> Service.
type Handler struct {
	svc    *Service
	logger *slog.Logger
}

func NewHandler(svc *Service, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

// RegisterRoutes mendaftarkan semua endpoint todo ke mux (pola Go 1.22+).
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /todos", h.list)
	mux.HandleFunc("POST /todos", h.create)
	mux.HandleFunc("GET /todos/{id}", h.get)
	mux.HandleFunc("PATCH /todos/{id}", h.update)
	mux.HandleFunc("DELETE /todos/{id}", h.delete)
}

// listResponse membungkus data list agar mudah ditambah metadata nanti.
type listResponse struct {
	Data   []Todo `json:"data"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var f ListFilter

	if v := q.Get("completed"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			web.WriteError(w, http.StatusBadRequest, "bad_request",
				"parameter completed harus true atau false", nil)
			return
		}
		f.Completed = &b
	}
	var err error
	if f.Limit, err = intParam(q.Get("limit")); err != nil {
		web.WriteError(w, http.StatusBadRequest, "bad_request", "parameter limit harus angka", nil)
		return
	}
	if f.Offset, err = intParam(q.Get("offset")); err != nil {
		web.WriteError(w, http.StatusBadRequest, "bad_request", "parameter offset harus angka", nil)
		return
	}

	f.Normalize() // agar limit/offset di respons = yang benar-benar dipakai
	todos, err := h.svc.List(r.Context(), f)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	web.WriteJSON(w, http.StatusOK, listResponse{Data: todos, Limit: f.Limit, Offset: f.Offset})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if err := web.DecodeJSON(w, r, &in); err != nil {
		web.WriteError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	t, err := h.svc.Create(r.Context(), in)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	w.Header().Set("Location", "/todos/"+strconv.FormatInt(t.ID, 10))
	web.WriteJSON(w, http.StatusCreated, t)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	t, err := h.svc.Get(r.Context(), id)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	web.WriteJSON(w, http.StatusOK, t)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var in UpdateInput
	if err := web.DecodeJSON(w, r, &in); err != nil {
		web.WriteError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	t, err := h.svc.Update(r.Context(), id, in)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	web.WriteJSON(w, http.StatusOK, t)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		h.handleError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleError memetakan error domain -> status HTTP. Satu tempat, konsisten.
func (h *Handler) handleError(w http.ResponseWriter, r *http.Request, err error) {
	var vErr *ValidationError
	switch {
	case errors.As(err, &vErr):
		web.WriteError(w, http.StatusUnprocessableEntity, "validation_error",
			"input tidak valid", vErr.Fields)
	case errors.Is(err, ErrNotFound):
		web.WriteError(w, http.StatusNotFound, "not_found", err.Error(), nil)
	default:
		// Detail error internal hanya masuk log, TIDAK dikirim ke client.
		h.logger.Error("kesalahan internal", "err", err, "method", r.Method, "path", r.URL.Path)
		web.WriteError(w, http.StatusInternalServerError, "internal_error",
			"terjadi kesalahan pada server", nil)
	}
}

func parseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		web.WriteError(w, http.StatusBadRequest, "bad_request", "id harus bilangan bulat positif", nil)
		return 0, false
	}
	return id, true
}

func intParam(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.Atoi(s)
}
