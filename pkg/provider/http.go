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
