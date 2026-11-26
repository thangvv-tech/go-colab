package colab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	xssiPrefix        = ")]}'\n"
	headerClientAgent = "X-Colab-Client-Agent"
	headerAccept      = "Accept"
	headerXSRF        = "X-Goog-Colab-Token"
	clientAgentValue  = "colab-cli"
	tunnelEndpoint    = "/tun/m"
)

// Environment holds the three Colab API base URLs.
type Environment struct {
	Domain    string // https://colab.research.google.com  (tunnel)
	API       string // https://colab.pa.googleapis.com
	PublicAPI string // https://colaboratory.googleapis.com
}

// ProdEnv is the production Colab environment.
var ProdEnv = Environment{
	Domain:    "https://colab.research.google.com",
	API:       "https://colab.pa.googleapis.com",
	PublicAPI: "https://colaboratory.googleapis.com",
}

// Client wraps an authenticated http.Client and handles Colab-specific
// protocol details: XSSI stripping, XSRF, authuser param, retries.
type Client struct {
	http *http.Client
	env  Environment
}

// NewClient creates a Client from an Authenticator.
func NewClient(ctx context.Context, auth Authenticator, env Environment) (*Client, error) {
	hc, err := auth.HTTPClient(ctx)
	if err != nil {
		return nil, err
	}
	return &Client{http: hc, env: env}, nil
}

// NewClientFromHTTP creates a Client from an already-authenticated *http.Client.
// Use this when you manage token refresh yourself (e.g. in backend services).
func NewClientFromHTTP(hc *http.Client, env Environment) *Client {
	return &Client{http: hc, env: env}
}

// HTTPClient returns the authenticated HTTP client used by this Colab client.
func (c *Client) HTTPClient() *http.Client {
	return c.http
}

// stripXSSI removes the XSSI prefix ")]}'\n" from Colab API responses.
func stripXSSI(body string) string {
	return strings.TrimPrefix(body, xssiPrefix)
}

// isTunnelDomain returns true if the URL targets the Colab tunnel domain.
func (c *Client) isTunnelDomain(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	t, err := url.Parse(c.env.Domain)
	if err != nil {
		return false
	}
	return u.Hostname() == t.Hostname()
}

// do executes an HTTP request with Colab headers. It strips XSSI and decodes
// the response body into out (if non-nil). Returns *ColabError on non-2xx.
func (c *Client) do(ctx context.Context, method, rawURL string, body io.Reader, extraHeaders map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return err
	}

	// Colab standard headers
	req.Header.Set(headerAccept, "application/json")
	req.Header.Set(headerClientAgent, clientAgentValue)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	// authuser=0 required for tunnel domain
	if c.isTunnelDomain(rawURL) {
		q := req.URL.Query()
		q.Set("authuser", "0")
		req.URL.RawQuery = q.Encode()
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &ColabError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("%s %s: %s", method, rawURL, resp.Status),
			Body:       string(respBody),
		}
	}

	if out == nil || len(respBody) == 0 {
		return nil
	}
	cleaned := stripXSSI(string(respBody))
	return json.Unmarshal([]byte(cleaned), out)
}

// GetJSON performs a GET and JSON-decodes the response into out.
func (c *Client) GetJSON(ctx context.Context, rawURL string, out any) error {
	return c.doWithRetry(ctx, http.MethodGet, rawURL, nil, nil, out)
}

// PostJSON performs a POST with JSON body and decodes the response into out.
func (c *Client) PostJSON(ctx context.Context, rawURL string, reqBody any, out any) error {
	var r io.Reader
	if reqBody != nil {
		b, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	return c.doWithRetry(ctx, http.MethodPost, rawURL, r, nil, out)
}

// PostWithXSRF does the two-step XSRF dance: GET to fetch token, POST with it.
// Colab uses this for unassign and assign.
func (c *Client) PostWithXSRF(ctx context.Context, rawURL string, getOut any, postOut any) error {
	// Step 1: GET to obtain XSRF token
	if err := c.doWithRetry(ctx, http.MethodGet, rawURL, nil, nil, getOut); err != nil {
		return err
	}
	// getOut must expose the XSRF token — caller extracts it and calls PostWithToken
	return nil
}

// PostWithToken POSTs with an explicit XSRF token header.
func (c *Client) PostWithToken(ctx context.Context, rawURL, xsrfToken string, out any) error {
	headers := map[string]string{headerXSRF: xsrfToken}
	return c.doWithRetry(ctx, http.MethodPost, rawURL, nil, headers, out)
}

func (c *Client) postWithTokenOnce(ctx context.Context, rawURL, xsrfToken string, out any) error {
	return c.do(ctx, http.MethodPost, rawURL, nil, map[string]string{headerXSRF: xsrfToken}, out)
}

// doWithRetry wraps do with exponential backoff on 502/503/504 and network errors.
// Max 3 retries, 2s base backoff (doubles each attempt).
func (c *Client) doWithRetry(ctx context.Context, method, rawURL string, body io.Reader, extraHeaders map[string]string, out any) error {
	// Buffer body for retry if needed
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = io.ReadAll(body)
		if err != nil {
			return err
		}
	}

	const maxRetries = 3
	backoff := 2 * time.Second

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
		}

		var r io.Reader
		if bodyBytes != nil {
			r = bytes.NewReader(bodyBytes)
		}

		err := c.do(ctx, method, rawURL, r, extraHeaders, out)
		if err == nil {
			return nil
		}

		// Check if retryable
		var colabErr *ColabError
		if ok := isColabError(err, &colabErr); ok {
			if colabErr.StatusCode == 412 {
				return &TooManyAssignmentsError{Cause: err}
			}
			if colabErr.StatusCode == 502 || colabErr.StatusCode == 503 || colabErr.StatusCode == 504 {
				lastErr = err
				continue
			}
			return err
		}
		// Network error — retry
		lastErr = err
	}
	return lastErr
}

func isColabError(err error, out **ColabError) bool {
	ce, ok := err.(*ColabError)
	if ok {
		*out = ce
	}
	return ok
}
