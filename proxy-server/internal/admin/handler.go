// Package admin serves the read-only observability API (/admin/*) consumed by
// the local admin UI. It binds the same localhost address as the proxy and is
// never exposed to the LAN. An optional bearer token guards every route; it is
// strongly recommended whenever store_raw_text is enabled (the audit DB then
// contains secrets). The API only reads the audit store — it never mutates
// state or forwards anything upstream.
package admin

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"promptgate/internal/storage"
)

// Reader is the read-only view of the audit store the admin API depends on.
// *storage.Store satisfies it.
type Reader interface {
	Query(storage.EventFilter) (storage.EventPage, error)
	Stats(from, to string) (storage.Stats, error)
	Get(id string) (storage.EventRow, bool, error)
}

// Meta is read-only proxy metadata surfaced to the admin UI header/status bar.
type Meta struct {
	StoreRawText  bool   `json:"storeRawText"`
	RetentionDays int    `json:"retentionDays"`
	Model         string `json:"model"`
	Backend       string `json:"backend"`
	ListenAddr    string `json:"listenAddr"`
	StartedAt     string `json:"startedAt"`
}

// Handler serves the /admin/* routes.
type Handler struct {
	reader    Reader
	meta      Meta
	authToken string
	log       *slog.Logger
}

// New builds an admin Handler. authToken="" disables token auth (localhost-only).
func New(r Reader, meta Meta, authToken string, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{reader: r, meta: meta, authToken: authToken, log: log}
}

// Register wires the admin routes onto mux. Method-scoped patterns (Go 1.22+)
// make non-GET requests return 405 automatically.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/stats", h.auth(h.stats))
	mux.HandleFunc("GET /admin/events", h.auth(h.events))
	mux.HandleFunc("GET /admin/events/{id}", h.auth(h.event))
	mux.HandleFunc("GET /admin/meta", h.auth(h.metaInfo))
}

func (h *Handler) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.authToken != "" {
			got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if subtle.ConstantTimeCompare([]byte(got), []byte(h.authToken)) != 1 {
				writeErr(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
				return
			}
		}
		next(w, r)
	}
}

func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, to := q.Get("from"), q.Get("to")
	if !validTime(from) || !validTime(to) {
		writeErr(w, http.StatusBadRequest, "bad_request", "from/to must be RFC3339")
		return
	}
	s, err := h.reader.Stats(from, to)
	if err != nil {
		h.fail(w, "stats", err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := storage.EventFilter{
		From:     q.Get("from"),
		To:       q.Get("to"),
		Decision: q.Get("decision"),
		Source:   q.Get("source"),
		Q:        q.Get("q"),
		Limit:    atoiDefault(q.Get("limit"), 50),
		Offset:   atoiDefault(q.Get("offset"), 0),
	}
	if !validTime(f.From) || !validTime(f.To) {
		writeErr(w, http.StatusBadRequest, "bad_request", "from/to must be RFC3339")
		return
	}
	page, err := h.reader.Query(f)
	if err != nil {
		h.fail(w, "events", err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *Handler) event(w http.ResponseWriter, r *http.Request) {
	row, ok, err := h.reader.Get(r.PathValue("id"))
	if err != nil {
		h.fail(w, "event", err)
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "not_found", "event not found")
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (h *Handler) metaInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.meta)
}

func (h *Handler) fail(w http.ResponseWriter, route string, err error) {
	h.log.Warn("admin query failed", "route", route, "err", err.Error())
	writeErr(w, http.StatusInternalServerError, "internal_error", "query failed")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": code, "message": msg})
}

func validTime(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse(time.RFC3339, s)
	return err == nil
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
