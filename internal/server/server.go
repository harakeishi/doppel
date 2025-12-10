package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/example/doppel/internal/storage"
)

// Server wraps HTTP handlers for the shadow server.
type Server struct {
	store *storage.Store
	mux   *http.ServeMux
}

// NewServer constructs handlers backed by the given database.
func NewServer(db *sql.DB) (http.Handler, error) {
	store, err := storage.NewStore(db)
	if err != nil {
		return nil, err
	}

	s := &Server{store: store, mux: http.NewServeMux()}
	s.routes()
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/record", s.handleRecord)
	s.mux.HandleFunc("/replay", s.handleReplay)
	s.mux.HandleFunc("/healthz", s.handleHealth)
}

type recordRequest struct {
	TraceID    string          `json:"trace_id"`
	Sequence   int             `json:"sequence"`
	SQL        string          `json:"sql"`
	Bindings   json.RawMessage `json:"bindings"`
	Result     json.RawMessage `json:"result"`
	DurationMS int             `json:"duration_ms"`
}

type recordResponse struct {
	TraceID    string          `json:"trace_id"`
	Sequence   int             `json:"sequence"`
	SQL        string          `json:"sql"`
	Bindings   json.RawMessage `json:"bindings"`
	Result     json.RawMessage `json:"result"`
	DurationMS int             `json:"duration_ms"`
}

func (s *Server) handleRecord(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var payload recordRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	if payload.TraceID == "" || payload.Sequence == 0 {
		http.Error(w, "trace_id and sequence are required", http.StatusBadRequest)
		return
	}

	bindings := toOptionalString(payload.Bindings)
	result := toOptionalString(payload.Result)

	record := storage.QueryRecord{
		TraceID:    payload.TraceID,
		Sequence:   payload.Sequence,
		SQL:        payload.SQL,
		Bindings:   bindings,
		Result:     result,
		DurationMS: payload.DurationMS,
	}

	if err := s.store.InsertQuery(r.Context(), record); err != nil {
		if errors.Is(err, storage.ErrConflict) {
			http.Error(w, "duplicate trace sequence", http.StatusConflict)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(recordResponse{
		TraceID:    payload.TraceID,
		Sequence:   payload.Sequence,
		SQL:        payload.SQL,
		Bindings:   payload.Bindings,
		Result:     payload.Result,
		DurationMS: payload.DurationMS,
	})
}

func (s *Server) handleReplay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	traceID := r.URL.Query().Get("trace_id")
	seqStr := r.URL.Query().Get("sequence")
	sequence, err := strconv.Atoi(seqStr)
	if traceID == "" || err != nil {
		http.Error(w, "trace_id and sequence are required", http.StatusBadRequest)
		return
	}

	rec, err := s.store.GetQuery(r.Context(), traceID, sequence)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	response := recordResponse{
		TraceID:    rec.TraceID,
		Sequence:   rec.Sequence,
		SQL:        rec.SQL,
		DurationMS: rec.DurationMS,
	}
	if rec.Bindings != nil {
		response.Bindings = json.RawMessage(*rec.Bindings)
	}
	if rec.Result != nil {
		response.Result = json.RawMessage(*rec.Result)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func toOptionalString(raw json.RawMessage) *string {
	if raw == nil {
		return nil
	}
	s := string(raw)
	return &s
}
