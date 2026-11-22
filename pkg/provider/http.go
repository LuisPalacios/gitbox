package provider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Doer sends one HTTP request. Every provider API request goes through the
// package's Doer, so tests can answer without a network and the transport
// rules that apply to every request live in one place.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// httpDoer is the Doer every request uses: one client shared by all
// providers, owned by this package instead of the process-wide
// http.DefaultClient. Tests replace it with SetDoerForTest.
var httpDoer Doer = newHTTPClient()

const (
	resourceTimeout = 10 * time.Second
	listingTimeout  = 20 * time.Second
	maxPRRetry      = 5 * time.Second
	maxCacheEntries = 64
	maxCachedBody   = 512 * 1024
)

func newHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = 4
	transport.IdleConnTimeout = 90 * time.Second
	transport.TLSHandshakeTimeout = 10 * time.Second
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Provider credentials can live in nonstandard headers. Never
			// forward them to another origin or accept a cross-origin login page.
			if len(via) >= 10 || !sameOrigin(req.URL, via[0].URL) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

func sameOrigin(left, right *url.URL) bool {
	return left.Scheme == right.Scheme && left.Host == right.Host
}

type requestOptions struct {
	account  string
	revision uint64
	retry    *prRetry
}

type requestOptionsKey struct{}

// WithAccountCache enables conditional requests for one account and
// credential revision. The effective authorization also participates in
// the key, including for credentials replaced outside the application.
func WithAccountCache(ctx context.Context, account string, revision uint64) context.Context {
	return context.WithValue(ctx, requestOptionsKey{}, requestOptions{account: account, revision: revision})
}

// WithPRRetry allows the refresh to spend up to five seconds waiting for
// one provider-requested retry across all its authenticated requests.
func WithPRRetry(ctx context.Context) context.Context {
	options, _ := ctx.Value(requestOptionsKey{}).(requestOptions)
	options.retry = &prRetry{}
	return context.WithValue(ctx, requestOptionsKey{}, options)
}

type prRetry struct {
	once sync.Once
}

type responseKey struct {
	account  string
	revision uint64
	url      string
	accept   string
	encoding string
	auth     [sha256.Size]byte
}

type cachedResponse struct {
	etag    string
	headers http.Header
	body    []byte
}

var responseCache = struct {
	sync.Mutex
	entries map[responseKey]cachedResponse
}{entries: make(map[responseKey]cachedResponse)}

func (key responseKey) load() cachedResponse {
	responseCache.Lock()
	defer responseCache.Unlock()
	return responseCache.entries[key]
}

func (key responseKey) save(response cachedResponse) {
	responseCache.Lock()
	defer responseCache.Unlock()
	if response.etag == "" {
		delete(responseCache.entries, key)
		return
	}
	if _, exists := responseCache.entries[key]; !exists && len(responseCache.entries) >= maxCacheEntries {
		for entry := range responseCache.entries {
			delete(responseCache.entries, entry)
			break
		}
	}
	responseCache.entries[key] = response
}

// SetDoerForTest replaces the package's Doer until the test ends.
func SetDoerForTest(t interface {
	Helper()
	Cleanup(func())
}, d Doer) {
	t.Helper()
	previous := httpDoer
	httpDoer = d
	t.Cleanup(func() { httpDoer = previous })
}

// doGet performs an authenticated GET for one resource.
func doGet(ctx context.Context, url string, headers map[string]string, target any) (http.Header, error) {
	ctx, cancel := context.WithTimeout(ctx, resourceTimeout)
	defer cancel()
	return get(ctx, url, headers, target, false)
}

// doListGet performs one authenticated page request. A whole listing can
// impose a shorter deadline through its caller's context.
func doListGet(ctx context.Context, url string, headers map[string]string, target any) (http.Header, error) {
	ctx, cancel := context.WithTimeout(ctx, listingTimeout)
	defer cancel()
	return get(ctx, url, headers, target, true)
}

func get(ctx context.Context, url string, headers map[string]string, target any, cacheable bool) (http.Header, error) {
	const method = http.MethodGet
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, requestError(method, url)
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	options, _ := ctx.Value(requestOptionsKey{}).(requestOptions)
	var key responseKey
	var cached cachedResponse
	if cacheable && options.account != "" {
		key = responseKey{
			account: options.account, revision: options.revision, url: url,
			accept: req.Header.Get("Accept"), encoding: req.Header.Get("Accept-Encoding"),
			auth: sha256.Sum256([]byte(req.Header.Get("Authorization") + "\x00" + req.Header.Get("Private-Token"))),
		}
		cached = key.load()
		if cached.etag != "" {
			req.Header.Set("If-None-Match", cached.etag)
		}
	}

	resp, err := getResponse(req, options.retry, cached.etag != "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		if err := json.NewDecoder(bytes.NewReader(cached.body)).Decode(target); err != nil {
			return nil, decodeError(method, url)
		}
		headers := cached.headers.Clone()
		for name, values := range resp.Header {
			headers[name] = append([]string(nil), values...)
		}
		if cacheForbidden(headers) {
			key.save(cachedResponse{})
		} else {
			if etag := headers.Get("ETag"); etag != "" {
				cached.etag = etag
			}
			cached.headers = cacheHeaders(headers)
			key.save(cached)
		}
		return headers, nil
	}

	body := io.Reader(resp.Body)
	var updated cachedResponse
	if key.account != "" && resp.Header.Get("ETag") != "" && !cacheForbidden(resp.Header) {
		buf, err := io.ReadAll(io.LimitReader(resp.Body, maxCachedBody+1))
		if err != nil {
			return nil, responseReadError(ctx, method, url, err)
		}
		body = io.MultiReader(bytes.NewReader(buf), resp.Body)
		if len(buf) <= maxCachedBody {
			updated = cachedResponse{etag: resp.Header.Get("ETag"), headers: cacheHeaders(resp.Header), body: buf}
		}
	}
	if err := json.NewDecoder(body).Decode(target); err != nil {
		return nil, responseReadError(ctx, method, url, err)
	}
	if key.account != "" {
		key.save(updated)
	}
	return resp.Header, nil
}

func getResponse(req *http.Request, retry *prRetry, conditional bool) (*http.Response, error) {
	ctx := req.Context()
	rawURL := req.URL.String()
	for attempt := 0; ; attempt++ {
		resp, err := httpDoer.Do(req)
		if err != nil {
			return nil, transportError(req.Method, rawURL, err)
		}
		if resp.StatusCode == http.StatusOK || conditional && resp.StatusCode == http.StatusNotModified {
			return resp, nil
		}
		failure := statusError(req.Method, rawURL, resp)
		resp.Body.Close()
		if attempt == 0 && ctx.Err() == nil && retry.wait(ctx, failure) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, transportError(req.Method, rawURL, err)
		}
		return nil, failure
	}
}

// Only response metadata needed for pagination and cache decisions is kept.
func cacheHeaders(headers http.Header) http.Header {
	result := make(http.Header)
	for _, name := range []string{"Link", "X-Next-Page", "Vary", "Cache-Control"} {
		if values := headers.Values(name); len(values) > 0 {
			result[name] = append([]string(nil), values...)
		}
	}
	return result
}

func cacheForbidden(headers http.Header) bool {
	for _, value := range headers.Values("Cache-Control") {
		for directive := range strings.SplitSeq(value, ",") {
			if strings.EqualFold(strings.TrimSpace(directive), "no-store") {
				return true
			}
		}
	}
	for _, value := range headers.Values("Vary") {
		for name := range strings.SplitSeq(value, ",") {
			switch strings.ToLower(strings.TrimSpace(name)) {
			case "", "accept", "authorization", "private-token", "accept-encoding":
				// Request headers used in the key or fixed by this transport.
			default:
				return true
			}
		}
	}
	return false
}

func (r *prRetry) wait(ctx context.Context, err *ProviderError) bool {
	if r == nil || err.Class != ClassRateLimited || err.RetryAfter <= 0 || err.RetryAfter > maxPRRetry {
		return false
	}
	retry := false
	r.once.Do(func() {
		timer := time.NewTimer(err.RetryAfter)
		defer timer.Stop()
		select {
		case <-ctx.Done():
		case <-timer.C:
			retry = ctx.Err() == nil
		}
	})
	return retry
}

func responseReadError(ctx context.Context, method, url string, err error) *ProviderError {
	if cause := ctx.Err(); cause != nil {
		return transportError(method, url, cause)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return transportError(method, url, err)
	}
	return decodeError(method, url)
}

type nextPage func(headers http.Header, count, current int) int

func listNumberedPages[T any](ctx context.Context, firstURL string, headers map[string]string, limit int, next nextPage) ([]T, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, listingTimeout)
	defer cancel()
	var items []T
	page := 1
	for range limit {
		var batch []T
		responseHeaders, err := doListGet(ctx, fmt.Sprintf("%s&page=%d", firstURL, page), headers, &batch)
		if err != nil {
			return nil, false, err
		}
		items = append(items, batch...)
		newPage := next(responseHeaders, len(batch), page)
		if newPage == 0 {
			return items, true, nil
		}
		if newPage <= page {
			break
		}
		page = newPage
	}
	return items, false, nil
}

func hasNextPage(headers http.Header) bool {
	for _, value := range headers.Values("Link") {
		if strings.Contains(value, `rel="next"`) {
			return true
		}
	}
	return false
}

func nextFullPage(size int) nextPage {
	return func(headers http.Header, count, current int) int {
		if count >= size || hasNextPage(headers) {
			return current + 1
		}
		return 0
	}
}

// doPost performs an authenticated POST request with a JSON body.
// If target is non-nil, the response body is JSON-decoded into it.
// Accepts 200 and 201 as success status codes.
func doPost(ctx context.Context, url string, headers map[string]string, body io.Reader, target any) (http.Header, error) {
	const method = http.MethodPost
	ctx, cancel := context.WithTimeout(ctx, resourceTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, requestError(method, url)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpDoer.Do(req)
	if err != nil {
		return nil, transportError(method, url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		failure := statusError(method, url, resp)
		if err := ctx.Err(); err != nil {
			return nil, transportError(method, url, err)
		}
		return nil, failure
	}

	if target != nil {
		if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
			return nil, responseReadError(ctx, method, url, err)
		}
	}

	return resp.Header, nil
}

// doDelete performs an authenticated DELETE request.
// Accepts 200, 202, and 204 as success status codes. GitLab returns 202
// Accepted when a project delete is queued; GitHub/Gitea return 204.
func doDelete(ctx context.Context, url string, headers map[string]string) error {
	const method = http.MethodDelete
	ctx, cancel := context.WithTimeout(ctx, resourceTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return requestError(method, url)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpDoer.Do(req)
	if err != nil {
		return transportError(method, url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		failure := statusError(method, url, resp)
		if err := ctx.Err(); err != nil {
			return transportError(method, url, err)
		}
		return failure
	}

	return nil
}
