package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	bodyMarker  = "secret-body-marker"
	queryMarker = "CANARY-QUERY-TOKEN"
	pathMarker  = "private-org/private-repo"
)

// assertProviderErrorSecretFree fails when the text of err holds the response
// body, the request path or its query.
func assertProviderErrorSecretFree(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, text := range []string{err.Error(), fmt.Sprintf("%+v", err), fmt.Sprintf("wrapped: %v", fmt.Errorf("github get repo: %w", err))} {
		for _, secret := range []string{bodyMarker, queryMarker, pathMarker, "Bearer", "http://", "https://"} {
			if strings.Contains(text, secret) {
				t.Errorf("error leaks %q: %s", secret, text)
			}
		}
	}
}

func TestProviderErrorsCarryNoSecrets(t *testing.T) {
	tests := []struct {
		status int
		class  ProviderClass
	}{
		{http.StatusUnauthorized, ClassUnauthorized},
		{http.StatusForbidden, ClassScopeDenied},
		{http.StatusNotFound, ClassNotFound},
		{http.StatusTooManyRequests, ClassRateLimited},
		{http.StatusUnprocessableEntity, ClassUnknown},
		{http.StatusInternalServerError, ClassServerError},
		{http.StatusBadGateway, ClassServerError},
	}
	headers := map[string]string{"Authorization": "Bearer " + queryMarker}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"message":"` + bodyMarker + ` for ` + queryMarker + `"}`))
			}))
			defer srv.Close()
			target := srv.URL + "/api/" + pathMarker + "?access_token=" + queryMarker
			host := strings.TrimPrefix(srv.URL, "http://")

			var out map[string]any
			_, getErr := doGet(context.Background(), target, headers, &out)
			_, postErr := doPost(context.Background(), target, headers, strings.NewReader(`{}`), nil)
			delErr := doDelete(context.Background(), target, headers)
			for method, err := range map[string]error{"GET": getErr, "POST": postErr, "DELETE": delErr} {
				assertProviderErrorSecretFree(t, err)
				pe, ok := errors.AsType[*ProviderError](err)
				if !ok {
					t.Fatalf("%s error = %#v, want a *ProviderError", method, err)
				}
				if pe.Status != tt.status || pe.Class != tt.class {
					t.Errorf("%s: status %d class %q, want %d %q", method, pe.Status, pe.Class, tt.status, tt.class)
				}
				if tt.status != http.StatusUnauthorized && tt.status != http.StatusForbidden &&
					pe.Detail != fmt.Sprintf("HTTP %d from %s (%s)", tt.status, host, method) {
					t.Errorf("%s detail = %q", method, pe.Detail)
				}
			}
		})
	}
}

func TestProviderTransportErrorsCarryNoSecrets(t *testing.T) {
	// Port 1 on loopback refuses the connection at once.
	target := "http://127.0.0.1:1/api/" + pathMarker + "?access_token=" + queryMarker
	var out map[string]any
	_, err := doGet(context.Background(), target, nil, &out)
	assertProviderErrorSecretFree(t, err)
	pe, ok := errors.AsType[*ProviderError](err)
	if !ok || pe.Class != ClassOffline || pe.Status != 0 {
		t.Fatalf("refused connection = %#v, want an offline *ProviderError", err)
	}
	if !IsNetworkError(err) {
		t.Errorf("IsNetworkError must still see the network failure behind a ProviderError")
	}

	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = doGet(ctx, slow.URL+"/"+pathMarker+"?t="+queryMarker, nil, &out)
	assertProviderErrorSecretFree(t, err)
	if pe, ok := errors.AsType[*ProviderError](err); !ok || pe.Class != ClassTimeout || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expired request = %#v, want a timeout that still unwraps to the deadline", err)
	}

	_, err = doGet(context.Background(), "http://bad host/"+pathMarker+"?t="+queryMarker, nil, &out)
	assertProviderErrorSecretFree(t, err)

	garbled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(bodyMarker + " is not JSON"))
	}))
	defer garbled.Close()
	_, err = doGet(context.Background(), garbled.URL+"/"+pathMarker+"?t="+queryMarker, nil, &out)
	assertProviderErrorSecretFree(t, err)
}

func TestProviderErrorClassifiers(t *testing.T) {
	respond := func(status int, body string) error {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		defer srv.Close()
		_, err := doPost(context.Background(), srv.URL+"/repos", nil, strings.NewReader(`{}`), nil)
		return fmt.Errorf("github create repo: %w", err)
	}
	unprocessable := respond(http.StatusUnprocessableEntity, `{"errors":[{"message":"name already exists on this account"}]}`)
	conflict := respond(http.StatusConflict, `{}`)
	invalid := respond(http.StatusUnprocessableEntity, `{"message":"name is too long"}`)
	serverSaysExists := respond(http.StatusInternalServerError, `already exists`)
	forbidden := respond(http.StatusForbidden, `already exists 401 404`)
	unauthorized := respond(http.StatusUnauthorized, `403 404`)
	notFound := respond(http.StatusNotFound, `401 403`)
	plain := errors.New("HTTP 401 HTTP 403 HTTP 404 already exists")

	checks := []struct {
		name string
		fn   func(error) bool
		yes  []error
		no   []error
	}{
		{"IsAlreadyExists", IsAlreadyExists, []error{unprocessable, conflict}, []error{invalid, serverSaysExists, notFound, plain, nil}},
		{"IsForbiddenError", IsForbiddenError, []error{forbidden}, []error{unauthorized, notFound, plain, nil}},
		{"IsUnauthorized", IsUnauthorized, []error{unauthorized}, []error{forbidden, notFound, plain, nil}},
		{"IsNotFound", IsNotFound, []error{notFound}, []error{forbidden, unauthorized, serverSaysExists, plain, nil}},
	}
	for _, c := range checks {
		for _, err := range c.yes {
			if !c.fn(err) {
				t.Errorf("%s(%v) = false, want true", c.name, err)
			}
		}
		for _, err := range c.no {
			if c.fn(err) {
				t.Errorf("%s(%v) = true, want false", c.name, err)
			}
		}
	}
}
