package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// doGet performs an authenticated GET request, decodes the JSON response into target,
// and returns the response headers. The authHeader is sent as-is in the Authorization
// (or provider-specific) header.
func doGet(ctx context.Context, url string, headers map[string]string, target any) (http.Header, error) {
	const method = http.MethodGet
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, requestError(method, url)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, transportError(method, url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, statusError(method, url, resp)
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return nil, decodeError(method, url)
	}

	return resp.Header, nil
}

// doPost performs an authenticated POST request with a JSON body.
// If target is non-nil, the response body is JSON-decoded into it.
// Accepts 200 and 201 as success status codes.
func doPost(ctx context.Context, url string, headers map[string]string, body io.Reader, target any) (http.Header, error) {
	const method = http.MethodPost
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, requestError(method, url)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, transportError(method, url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, statusError(method, url, resp)
	}

	if target != nil {
		if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
			return nil, decodeError(method, url)
		}
	}

	return resp.Header, nil
}

// doDelete performs an authenticated DELETE request.
// Accepts 200, 202, and 204 as success status codes. GitLab returns 202
// Accepted when a project delete is queued; GitHub/Gitea return 204.
func doDelete(ctx context.Context, url string, headers map[string]string) error {
	const method = http.MethodDelete
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return requestError(method, url)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return transportError(method, url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		return statusError(method, url, resp)
	}

	return nil
}
