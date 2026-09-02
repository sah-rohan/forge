package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const DefaultScope = "https://cognitiveservices.azure.com/.default"

const (
	defaultTokenLifetime = 55 * time.Minute

	refreshSkew = 5 * time.Minute

	tokenTimeout = 10 * time.Second
)

type Credential interface {
	Headers(ctx context.Context) (map[string]string, error)
}

type Invalidator interface {
	Invalidate()
}

type staticKey struct{ headers map[string]string }

func StaticKey(key string) Credential {
	return &staticKey{headers: map[string]string{"api-key": key}}
}

func (s *staticKey) Headers(context.Context) (map[string]string, error) {
	if s.headers["api-key"] == "" {
		return nil, fmt.Errorf("empty api key")
	}
	return s.headers, nil
}

type keyFile struct{ path string }

func KeyFile(path string) Credential { return keyFile{path: path} }

func (k keyFile) Headers(context.Context) (map[string]string, error) {
	raw, err := os.ReadFile(k.path)
	if err != nil {
		return nil, fmt.Errorf("read key file %s: %w", k.path, err)
	}
	key := strings.TrimSpace(string(raw))
	if key == "" {
		return nil, fmt.Errorf("key file %s is empty", k.path)
	}
	return map[string]string{"api-key": key}, nil
}

type bearer struct {
	value   string
	expires time.Time
	headers map[string]string
}

func (b bearer) fresh(now time.Time) bool {
	return b.value != "" && now.Before(b.expires.Add(-refreshSkew))
}

type tokenFetch func(ctx context.Context, hc *http.Client) (bearer, error)

type tokenCredential struct {
	fetch tokenFetch
	http  *http.Client
	how   string

	cached atomic.Pointer[bearer]

	mu       sync.Mutex
	inflight *refresh
}

type refresh struct {
	done chan struct{}
	tok  bearer
	err  error
}

func (c *tokenCredential) Headers(ctx context.Context) (map[string]string, error) {
	if b := c.cached.Load(); b != nil && b.fresh(time.Now()) {
		return b.headers, nil
	}

	c.mu.Lock()
	if b := c.cached.Load(); b != nil && b.fresh(time.Now()) {
		c.mu.Unlock()
		return b.headers, nil
	}
	r := c.inflight
	if r == nil {
		r = &refresh{done: make(chan struct{})}
		c.inflight = r
		go c.run(r)
	}
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.done:
	}
	if r.err != nil {
		return nil, fmt.Errorf("acquire token via %s: %w", c.how, r.err)
	}
	return r.tok.headers, nil
}

func (c *tokenCredential) run(r *refresh) {
	ctx, cancel := context.WithTimeout(context.Background(), tokenTimeout)
	defer cancel()

	r.tok, r.err = c.fetch(ctx, c.http)
	if r.err == nil {
		r.tok.headers = map[string]string{"authorization": "Bearer " + r.tok.value}
		tok := r.tok
		c.cached.Store(&tok)
	}

	c.mu.Lock()
	c.inflight = nil
	c.mu.Unlock()
	close(r.done)
}

func (c *tokenCredential) Invalidate() {
	c.cached.Store(nil)
}

func EntraID(scope string) Credential {
	if scope == "" {
		scope = DefaultScope
	}
	fetch, how := entraFlow(scope)
	return &tokenCredential{fetch: fetch, http: tokenHTTPClient(), how: how}
}

func FromTokenProvider(fn func(ctx context.Context) (token string, expires time.Time, err error)) Credential {
	return &tokenCredential{
		how:  "token provider",
		http: tokenHTTPClient(),
		fetch: func(ctx context.Context, _ *http.Client) (bearer, error) {
			tok, exp, err := fn(ctx)
			if err != nil {
				return bearer{}, err
			}
			if tok == "" {
				return bearer{}, fmt.Errorf("token provider returned an empty token")
			}
			if exp.IsZero() {
				exp = time.Now().Add(defaultTokenLifetime)
			}
			return bearer{value: tok, expires: exp}, nil
		},
	}
}

func tokenHTTPClient() *http.Client { return &http.Client{Timeout: tokenTimeout} }

func entraFlow(scope string) (tokenFetch, string) {
	var (
		tenant   = os.Getenv("AZURE_TENANT_ID")
		clientID = os.Getenv("AZURE_CLIENT_ID")
		secret   = os.Getenv("AZURE_CLIENT_SECRET")
		fedFile  = os.Getenv("AZURE_FEDERATED_TOKEN_FILE")
	)

	switch {
	case fedFile != "":
		return func(ctx context.Context, hc *http.Client) (bearer, error) {

			assertion, err := os.ReadFile(fedFile)
			if err != nil {
				return bearer{}, fmt.Errorf("read federated token file %s: %w", fedFile, err)
			}
			return entraToken(ctx, hc, tenant, url.Values{
				"grant_type":            {"client_credentials"},
				"scope":                 {scope},
				"client_id":             {clientID},
				"client_assertion_type": {"urn:ietf:params:oauth:client-assertion-type:jwt-bearer"},
				"client_assertion":      {strings.TrimSpace(string(assertion))},
			})
		}, "workload identity federation"

	case secret != "":
		return func(ctx context.Context, hc *http.Client) (bearer, error) {
			return entraToken(ctx, hc, tenant, url.Values{
				"grant_type":    {"client_credentials"},
				"scope":         {scope},
				"client_id":     {clientID},
				"client_secret": {secret},
			})
		}, "service principal"

	case os.Getenv("IDENTITY_ENDPOINT") != "":
		return func(ctx context.Context, hc *http.Client) (bearer, error) {
			return appServiceToken(ctx, hc, resourceOf(scope), clientID)
		}, "App Service managed identity"

	default:
		return func(ctx context.Context, hc *http.Client) (bearer, error) {
			return imdsToken(ctx, hc, resourceOf(scope), clientID)
		}, "IMDS managed identity"
	}
}

func resourceOf(scope string) string {
	return strings.TrimSuffix(scope, "/.default")
}

func entraToken(ctx context.Context, hc *http.Client, tenant string, form url.Values) (bearer, error) {
	if tenant == "" {
		return bearer{}, fmt.Errorf("AZURE_TENANT_ID is not set")
	}
	if form.Get("client_id") == "" {
		return bearer{}, fmt.Errorf("AZURE_CLIENT_ID is not set")
	}

	endpoint := "https://login.microsoftonline.com/" + url.PathEscape(tenant) + "/oauth2/v2.0/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return bearer{}, err
	}
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	return doTokenRequest(hc, req)
}

func imdsToken(ctx context.Context, hc *http.Client, resource, clientID string) (bearer, error) {
	q := url.Values{"api-version": {"2018-02-01"}, "resource": {resource}}
	if clientID != "" {

		q.Set("client_id", clientID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://169.254.169.254/metadata/identity/oauth2/token?"+q.Encode(), nil)
	if err != nil {
		return bearer{}, err
	}
	req.Header.Set("Metadata", "true")

	tok, err := doTokenRequest(hc, req)
	if err != nil {
		return bearer{}, fmt.Errorf("%w — no managed identity is reachable here; "+
			"set AZURE_OPENAI_KEY (or AZURE_OPENAI_KEY_FILE) for local development", err)
	}
	return tok, nil
}

func appServiceToken(ctx context.Context, hc *http.Client, resource, clientID string) (bearer, error) {
	endpoint := os.Getenv("IDENTITY_ENDPOINT")
	header := os.Getenv("IDENTITY_HEADER")
	if header == "" {
		return bearer{}, fmt.Errorf("IDENTITY_ENDPOINT is set but IDENTITY_HEADER is not")
	}

	q := url.Values{"api-version": {"2019-08-01"}, "resource": {resource}}
	if clientID != "" {
		q.Set("client_id", clientID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return bearer{}, err
	}
	req.Header.Set("X-IDENTITY-HEADER", header)
	return doTokenRequest(hc, req)
}

type tokenResponse struct {
	AccessToken      string  `json:"access_token"`
	ExpiresIn        flexInt `json:"expires_in"`
	ExpiresOn        flexInt `json:"expires_on"`
	Error            string  `json:"error"`
	ErrorDescription string  `json:"error_description"`
}

type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		return nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		*f = flexInt(n)
	}
	return nil
}

func doTokenRequest(hc *http.Client, req *http.Request) (bearer, error) {
	resp, err := hc.Do(req)
	if err != nil {
		return bearer{}, err
	}
	defer resp.Body.Close()

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil && resp.StatusCode == http.StatusOK {
		return bearer{}, fmt.Errorf("decode token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := tr.ErrorDescription
		if msg == "" {
			msg = tr.Error
		}
		if msg == "" {
			msg = "no detail"
		}
		return bearer{}, fmt.Errorf("identity endpoint returned %d: %s", resp.StatusCode, msg)
	}
	if tr.AccessToken == "" {
		return bearer{}, fmt.Errorf("identity endpoint returned no access_token")
	}

	switch {
	case tr.ExpiresIn > 0:
		return bearer{value: tr.AccessToken, expires: time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)}, nil
	case tr.ExpiresOn > 0:
		return bearer{value: tr.AccessToken, expires: time.Unix(int64(tr.ExpiresOn), 0)}, nil
	default:
		return bearer{value: tr.AccessToken, expires: time.Now().Add(defaultTokenLifetime)}, nil
	}
}

var (
	defaultOnce sync.Once
	defaultCred Credential
)

func Default() Credential {
	defaultOnce.Do(func() { defaultCred = FromEnv() })
	return defaultCred
}

func FromEnv() Credential {
	if path := os.Getenv("AZURE_OPENAI_KEY_FILE"); path != "" {
		return KeyFile(path)
	}
	if key := os.Getenv("AZURE_OPENAI_KEY"); key != "" {
		return StaticKey(key)
	}
	return EntraID(os.Getenv("AZURE_OPENAI_SCOPE"))
}

func Client(cred Credential) *http.Client {
	return &http.Client{Transport: Transport(cred, nil)}
}

func Transport(cred Credential, base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &transport{cred: cred, base: base}
}

type transport struct {
	cred Credential
	base http.RoundTripper
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.attempt(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}

	inv, ok := t.cred.(Invalidator)
	if !ok {
		return resp, nil
	}
	body, err := replayBody(req)
	if err != nil {

		return resp, nil
	}

	resp.Body.Close()
	inv.Invalidate()

	retry := req.Clone(req.Context())
	retry.Body = body
	return t.attempt(retry)
}

func (t *transport) attempt(req *http.Request) (*http.Response, error) {
	headers, err := t.cred.Headers(req.Context())
	if err != nil {
		return nil, fmt.Errorf("forge: %w", err)
	}

	authed := req.Clone(req.Context())
	for name, value := range headers {
		authed.Header.Set(name, value)
	}
	return t.base.RoundTrip(authed)
}

func replayBody(req *http.Request) (io.ReadCloser, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return req.Body, nil
	}
	if req.GetBody == nil {
		return nil, fmt.Errorf("request body cannot be replayed")
	}
	return req.GetBody()
}
