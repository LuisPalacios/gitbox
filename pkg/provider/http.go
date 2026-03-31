package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// doGet performs an authenticated GET request, decodes the JSON response into target,
// and returns the response headers. The authHeader is sent as-is in the Authorization
// (or provider-specific) header.
func doGet(ctx context.Context, url string, headers map[string]string, target any) (http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("authentication failed (HTTP 401) — token is invalid or expired")
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("insufficient permissions (HTTP 403) — token needs broader scopes")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, url, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return nil, fmt.Errorf("decoding response from %s: %w", url, err)
	}

	return resp.Header, nil
}

// doPost performs an authenticated POST request with a JSON body.
// If target is non-nil, the response body is JSON-decoded into it.
// Accepts 200 and 201 as success status codes.
func doPost(ctx context.Context, url string, headers map[string]string, body io.Reader, target any) (http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("authentication failed (HTTP 401) — token is invalid or expired")
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("insufficient permissions (HTTP 403) — token needs broader scopes")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, url, string(body))
	}

	if target != nil {
		if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
			return nil, fmt.Errorf("decoding response from %s: %w", url, err)
		}
	}

	return resp.Header, nil
}

// doDelete performs an authenticated DELETE request.
// Accepts 200 and 204 as success status codes.
func doDelete(ctx context.Context, url string, headers map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP DELETE %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("authentication failed (HTTP 401) — token is invalid or expired")
	}
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("insufficient permissions (HTTP 403) — token needs broader scopes")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, url, string(body))
	}

	return nil
}
