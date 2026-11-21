package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- doPost tests ---

func TestDoPostSuccess(t *testing.T) {
	type resp struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing Content-Type")
		}
		if r.Header.Get("Authorization") != "Bearer tok123" {
			t.Error("missing auth header")
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(resp{ID: 42, Name: "test"})
	}))
	defer srv.Close()

	var result resp
	headers := map[string]string{"Authorization": "Bearer tok123"}
	_, err := doPost(context.Background(), srv.URL, headers, strings.NewReader(`{"name":"test"}`), &result)
	if err != nil {
		t.Fatalf("doPost: %v", err)
	}
	if result.ID != 42 || result.Name != "test" {
		t.Errorf("result = %+v", result)
	}
}

func TestDoPostNilTarget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	_, err := doPost(context.Background(), srv.URL, nil, strings.NewReader(`{}`), nil)
	if err != nil {
		t.Fatalf("doPost with nil target: %v", err)
	}
}

func TestDoPost401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := doPost(context.Background(), srv.URL, nil, strings.NewReader(`{}`), nil)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 error, got %v", err)
	}
}

func TestDoPost403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := doPost(context.Background(), srv.URL, nil, strings.NewReader(`{}`), nil)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("expected 403 error, got %v", err)
	}
}

func TestDoPost422(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"message":"validation failed"}`))
	}))
	defer srv.Close()

	_, err := doPost(context.Background(), srv.URL, nil, strings.NewReader(`{}`), nil)
	if err == nil || !strings.Contains(err.Error(), "422") {
		t.Errorf("expected 422 error, got %v", err)
	}
}

// --- doDelete tests ---

func TestDoDeleteSuccess200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.Header.Get("Authorization") != "token abc" {
			t.Error("missing auth header")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := doDelete(context.Background(), srv.URL, map[string]string{"Authorization": "token abc"})
	if err != nil {
		t.Fatalf("doDelete: %v", err)
	}
}

func TestDoDeleteSuccess204(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	err := doDelete(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatalf("doDelete: %v", err)
	}
}

func TestDoDelete401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	err := doDelete(context.Background(), srv.URL, nil)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 error, got %v", err)
	}
}

func TestDoDelete404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	err := doDelete(context.Background(), srv.URL, nil)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 error, got %v", err)
	}
}

// doerFunc answers requests without a network.
type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

// Every request goes through the package's Doer: with a fake in place no
// connection is made, and the previous Doer comes back when the test ends.
func TestRequestsGoThroughTheDoer(t *testing.T) {
	before := httpDoer
	var methods []string
	t.Run("with a fake", func(t *testing.T) {
		SetDoerForTest(t, doerFunc(func(req *http.Request) (*http.Response, error) {
			methods = append(methods, req.Method)
			rec := httptest.NewRecorder()
			rec.WriteHeader(http.StatusOK)
			_, _ = rec.WriteString(`{"ok":true}`)
			return rec.Result(), nil
		}))
		// Port 1 refuses every connection: only the fake can answer.
		const unreachable = "http://127.0.0.1:1/api"
		var out map[string]any
		if _, err := doGet(context.Background(), unreachable, nil, &out); err != nil || out["ok"] != true {
			t.Errorf("doGet = %v, %v", out, err)
		}
		if _, err := doPost(context.Background(), unreachable, nil, strings.NewReader(`{}`), nil); err != nil {
			t.Errorf("doPost: %v", err)
		}
		if err := doDelete(context.Background(), unreachable, nil); err != nil {
			t.Errorf("doDelete: %v", err)
		}
	})
	if strings.Join(methods, " ") != "GET POST DELETE" {
		t.Errorf("the fake saw %v, want GET POST DELETE", methods)
	}
	if httpDoer != before {
		t.Errorf("the Doer was not restored after the test")
	}
	if httpDoer == Doer(http.DefaultClient) {
		t.Errorf("the package must own its client, not share http.DefaultClient")
	}
}
