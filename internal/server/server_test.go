package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	_ "modernc.org/sqlite"
)

type testRecordRequest struct {
	TraceID    string          `json:"trace_id"`
	Sequence   int             `json:"sequence"`
	SQL        string          `json:"sql"`
	Bindings   json.RawMessage `json:"bindings"`
	Result     json.RawMessage `json:"result"`
	DurationMS int             `json:"duration_ms"`
}

func mustRaw(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func setupTestServer(t *testing.T) http.Handler {
	t.Helper()
	db, err := sql.Open("sqlite", "file:memdb1?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	srv, err := NewServer(db)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	return srv
}

func TestRecordSuccess(t *testing.T) {
	handler := setupTestServer(t)

	body := testRecordRequest{
		TraceID:    "trace-1",
		Sequence:   1,
		SQL:        "SELECT 1",
		Bindings:   mustRaw(t, []interface{}{1}),
		Result:     mustRaw(t, map[string]interface{}{"columns": []string{"1"}, "rows": [][]int{{1}}}),
		DurationMS: 5,
	}

	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/record", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", w.Code)
	}
}

func TestRecordValidation(t *testing.T) {
	handler := setupTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/record", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRecordConflict(t *testing.T) {
	handler := setupTestServer(t)

	body := testRecordRequest{TraceID: "trace-1", Sequence: 1}
	payload, _ := json.Marshal(body)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/record", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if i == 0 && w.Code != http.StatusCreated {
			t.Fatalf("first request should be 201, got %d", w.Code)
		}
		if i == 1 && w.Code != http.StatusConflict {
			t.Fatalf("second request should be 409, got %d", w.Code)
		}
	}
}

func TestReplay(t *testing.T) {
	handler := setupTestServer(t)

	record := testRecordRequest{
		TraceID:  "trace-1",
		Sequence: 2,
		SQL:      "SELECT * FROM users WHERE id = ?",
		Bindings: mustRaw(t, []interface{}{123}),
		Result:   mustRaw(t, map[string]interface{}{"columns": []string{"id"}, "rows": [][]int{{123}}}),
	}
	payload, _ := json.Marshal(record)

	recReq := httptest.NewRequest(http.MethodPost, "/record", bytes.NewReader(payload))
	recReq.Header.Set("Content-Type", "application/json")
	recRes := httptest.NewRecorder()
	handler.ServeHTTP(recRes, recReq)
	if recRes.Code != http.StatusCreated {
		t.Fatalf("expected 201 on record, got %d", recRes.Code)
	}

	replayReq := httptest.NewRequest(http.MethodGet, "/replay?trace_id=trace-1&sequence=2", nil)
	replayRes := httptest.NewRecorder()
	handler.ServeHTTP(replayRes, replayReq)

	if replayRes.Code != http.StatusOK {
		t.Fatalf("expected 200 on replay, got %d", replayRes.Code)
	}

	var got testRecordRequest
	if err := json.Unmarshal(replayRes.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if diff := cmp.Diff(record, got, cmpopts.EquateEmpty()); diff != "" {
		t.Fatalf("unexpected replay body (-want +got):\n%s", diff)
	}
}

func TestReplayNotFound(t *testing.T) {
	handler := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/replay?trace_id=missing&sequence=1", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}
}

func TestHealth(t *testing.T) {
	handler := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
}
