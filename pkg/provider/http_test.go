package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
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

func TestProviderRateAndScopeSignals(t *testing.T) {
	const bodyCanary = "provider-body-canary"
	for _, tc := range []struct {
		name    string
		status  int
		headers map[string]string
		body    string
		class   ProviderClass
		delay   time.Duration
	}{
		{"GitHub exhausted before scope", 403, map[string]string{"X-RateLimit-Remaining": "0", "X-Accepted-OAuth-Scopes": "repo", "Retry-After": "3"}, `{}`, ClassRateLimited, 3 * time.Second},
		{"GitHub scope absent", 403, map[string]string{"X-Accepted-OAuth-Scopes": "repo", "X-OAuth-Scopes": "gist"}, `{}`, ClassScopeDenied, 0},
		{"GitHub scope granted", 403, map[string]string{"X-Accepted-OAuth-Scopes": "repo", "X-OAuth-Scopes": "repo"}, `{}`, ClassForbidden, 0},
		{"GitLab scope", 403, nil, `{"error":"insufficient_scope","message":"` + bodyCanary + `"}`, ClassScopeDenied, 0},
		{"nested rate code", 403, map[string]string{"Retry-After": "4"}, `{"error":{"code":"rate_limited","message":"` + bodyCanary + `"}}`, ClassRateLimited, 4 * time.Second},
		{"rate limit from status", 429, map[string]string{"Retry-After": "7"}, `{}`, ClassRateLimited, 7 * time.Second},
		{"ordinary forbidden", 403, nil, `{"message":"` + bodyCanary + `"}`, ClassForbidden, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for name, value := range tc.headers {
					w.Header().Set(name, value)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()

			var response any
			_, err := doGet(t.Context(), server.URL+"/private-path?key=private-query", nil, &response)
			got, ok := errors.AsType[*ProviderError](err)
			if !ok || got.Status != tc.status || got.Class != tc.class || got.RetryAfter != tc.delay {
				t.Fatalf("error = %#v; want HTTP %d, %q, delay %v", got, tc.status, tc.class, tc.delay)
			}
			for _, secret := range []string{bodyCanary, "private-path", "private-query"} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("error includes %q", secret)
				}
			}
		})
	}
}

func TestRetryAfterTimestamps(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	serverTime := now.Add(4 * time.Second)
	for _, headers := range []http.Header{
		{"Retry-After": {serverTime.Format(http.TimeFormat)}},
		{"X-Ratelimit-Reset": {strconv.FormatInt(serverTime.Unix(), 10)}},
	} {
		if got := retryAfter(headers, now); got != 4*time.Second {
			t.Errorf("delay = %v; want 4s", got)
		}
	}
}

func TestProviderDeadlineIncludesResponseBody(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(fmt.Sprintf("started=%t", started), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if started {
					w.WriteHeader(http.StatusOK)
					fmt.Fprint(w, "{")
					http.NewResponseController(w).Flush()
				}
				<-r.Context().Done()
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			var response any
			_, err := doGet(ctx, server.URL, nil, &response)
			got, ok := errors.AsType[*ProviderError](err)
			if !ok || got.Class != ClassTimeout || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("error = %v; want typed timeout", err)
			}
		})
	}
}

func TestProviderRequestBudgets(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var budget time.Duration
		SetDoerForTest(t, doerFunc(func(req *http.Request) (*http.Response, error) {
			deadline, ok := req.Context().Deadline()
			if !ok {
				t.Fatal("request has no deadline")
			}
			budget = time.Until(deadline)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
		}))
		var result any
		if _, err := doGet(t.Context(), "https://example.invalid/user", nil, &result); err != nil || budget != 10*time.Second {
			t.Fatalf("single resource budget = %v, error = %v", budget, err)
		}
		if _, err := doListGet(t.Context(), "https://example.invalid/list", nil, &result); err != nil || budget != 20*time.Second {
			t.Fatalf("list budget = %v, error = %v", budget, err)
		}
	})
}

func TestProviderConditionalGETIsIsolated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if val := r.Header.Get("If-None-Match"); val != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"fixture-etag"`)
		w.Header().Set("X-Next-Page", "2")
		w.Header().Set("Vary", "Accept, Authorization")
		w.Header().Set("Set-Cookie", "provider-fixture-cookie=secret")
		json.NewEncoder(w).Encode(strings.TrimPrefix(r.Header.Get("Authorization"), "token-"))
	}))
	defer server.Close()

	read := func(account string, revision uint64, token, accept string, conditional bool) {
		t.Helper()
		var result string
		ctx := WithAccountCache(t.Context(), account, revision)
		hdr, err := doListGet(ctx, server.URL, map[string]string{"Authorization": token, "Accept": accept}, &result)
		if err != nil || result != strings.TrimPrefix(token, "token-") || hdr.Get("X-Next-Page") != "2" {
			t.Fatalf("conditional GET: result=%q headers=%v err=%v", result, hdr, err)
		}
		if got := hdr.Get("Set-Cookie") == ""; got != conditional {
			t.Errorf("cached body reused = %v, want %v", got, conditional)
		}
	}
	read("account-a", 1, "token-one", "application/json", false)
	read("account-a", 1, "token-one", "application/json", true)
	read("account-b", 1, "token-one", "application/json", false)
	read("account-a", 2, "token-one", "application/json", false)
	read("account-a", 1, "token-two", "application/json", false)
	read("account-a", 1, "token-one", "application/vnd.github+json", false)
}

func TestProviderConditionalGETRespectsCachePolicy(t *testing.T) {
	for _, tc := range []struct {
		name, key, value string
	}{
		{"no store", "Cache-Control", "private, no-store"},
		{"unsupported variance", "Vary", "Accept-Language"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch requests.Add(1) {
				case 1:
					w.Header().Set("ETag", `"fixture-etag"`)
				case 2:
					if r.Header.Get("If-None-Match") == "" {
						t.Error("conditional header missing on second request")
					}
					w.Header().Set(tc.key, tc.value)
					w.WriteHeader(http.StatusNotModified)
					return
				case 3:
					if r.Header.Get("If-None-Match") != "" {
						t.Error("conditional header survived cache invalidation")
					}
				}
				fmt.Fprint(w, "[]")
			}))
			defer server.Close()
			ctx := WithAccountCache(t.Context(), "account", 1)
			for range 3 {
				var result []string
				if _, err := doListGet(ctx, server.URL, nil, &result); err != nil {
					t.Fatalf("GET: %v", err)
				}
			}
		})
	}
}

func TestProviderPRRetryIsOptInAndOnlyOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		SetDoerForTest(t, doerFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			header := http.Header{"Retry-After": {"3"}}
			status := http.StatusTooManyRequests
			if calls == 3 {
				status = http.StatusOK
			}
			return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader("{}"))}, nil
		}))
		var result any
		now := time.Now()
		_, err := doListGet(t.Context(), "https://example.invalid/search", nil, &result)
		if !isRateLimited(err) || calls != 1 || time.Since(now) != 0 {
			t.Fatalf("ordinary GET waited or retried: calls=%d, err=%v", calls, err)
		}
		ctx := WithPRRetry(t.Context())
		if _, err := doListGet(ctx, "https://example.invalid/search", nil, &result); err != nil || calls != 3 || time.Since(now) != 3*time.Second {
			t.Fatalf("PR GET did not wait and retry once: calls=%d, elapsed=%v, err=%v", calls, time.Since(now), err)
		}
		_, err = doListGet(ctx, "https://example.invalid/search?page=2", nil, &result)
		if !isRateLimited(err) || calls != 4 || time.Since(now) != 3*time.Second {
			t.Fatalf("PR listing retried more than once: calls=%d, elapsed=%v, err=%v", calls, time.Since(now), err)
		}
	})
}
