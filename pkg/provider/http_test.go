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
