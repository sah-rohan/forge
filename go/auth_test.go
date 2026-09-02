package forge

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type authStub struct {
	t       *testing.T
	replies []int

	mu     sync.Mutex
	auth   []string
	bodies []string
}

func newAuthStub(t *testing.T, cred Credential, replies ...int) (*authStub, *http.Client, string) {
	t.Helper()
	s := &authStub{t: t, replies: replies}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)

	client := Client(cred)
	return s, client, srv.URL
}

func (s *authStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	seen := "api-key: " + r.Header.Get("api-key")
	if a := r.Header.Get("authorization"); a != "" {
		seen = "authorization: " + a
	}
	body, _ := io.ReadAll(r.Body)

	s.mu.Lock()
	n := len(s.auth)
	s.auth = append(s.auth, seen)
	s.bodies = append(s.bodies, string(body))
	s.mu.Unlock()

	if n >= len(s.replies) {
		s.t.Errorf("stub got %d calls, only %d replies configured", n+1, len(s.replies))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(s.replies[n])
}

func (s *authStub) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.auth...)
}

func post(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	resp, err := client.Post(url, "application/json", strings.NewReader(`{"hello":"world"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

type rotating struct{ n int32 }

func (r *rotating) Headers(context.Context) (map[string]string, error) {
	return map[string]string{"api-key": fmt.Sprintf("key-%d", atomic.AddInt32(&r.n, 1))}, nil
}

func countingTokens() tokenFetch {
	var n int32
	return func(context.Context, *http.Client) (bearer, error) {
		return bearer{
			value:   fmt.Sprintf("token-%d", atomic.AddInt32(&n, 1)),
			expires: time.Now().Add(time.Hour),
		}, nil
	}
}

func TestCredentialResolvedPerRequest(t *testing.T) {
	stub, client, url := newAuthStub(t, &rotating{}, 200, 200)

	post(t, client, url)
	post(t, client, url)

	want := []string{"api-key: key-1", "api-key: key-2"}
	if got := stub.seen(); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("auth headers = %v, want %v", got, want)
	}
}

func TestUnauthorizedInvalidatesAndRetriesOnce(t *testing.T) {
	cred := &tokenCredential{how: "test", fetch: countingTokens()}
	stub, client, url := newAuthStub(t, cred, 401, 200)

	resp := post(t, client, url)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 after the reauth retry", resp.StatusCode)
	}
	got := stub.seen()
	if len(got) != 2 || got[0] != "authorization: Bearer token-1" || got[1] != "authorization: Bearer token-2" {
		t.Fatalf("expected a fresh token on the retry, got %v", got)
	}
}

func TestUnauthorizedRetryResendsTheBody(t *testing.T) {
	cred := &tokenCredential{how: "test", fetch: countingTokens()}
	stub, client, url := newAuthStub(t, cred, 401, 200)

	post(t, client, url)

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.bodies) != 2 || stub.bodies[0] != stub.bodies[1] {
		t.Fatalf("the retry must resend the body, got %q", stub.bodies)
	}
}

func TestUnauthorizedRetriedOnlyOnce(t *testing.T) {
	cred := &tokenCredential{how: "test", fetch: countingTokens()}
	stub, client, url := newAuthStub(t, cred, 401, 401)

	resp := post(t, client, url)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want the second 401 to be surfaced", resp.StatusCode)
	}

	if n := len(stub.seen()); n != 2 {
		t.Fatalf("expected 2 attempts, got %d", n)
	}
}

func TestUnauthorizedWithStaticKeyIsNotRetried(t *testing.T) {

	stub, client, url := newAuthStub(t, StaticKey("k"), 401)

	if resp := post(t, client, url); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if n := len(stub.seen()); n != 1 {
		t.Fatalf("expected 1 attempt, got %d", n)
	}
}

func TestCredentialFailureIsSurfaced(t *testing.T) {
	broken := credentialFunc(func(context.Context) (map[string]string, error) {
		return nil, fmt.Errorf("identity endpoint unreachable")
	})
	_, client, url := newAuthStub(t, broken)

	_, err := client.Post(url, "application/json", strings.NewReader("{}"))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "identity endpoint unreachable") {
		t.Fatalf("error should carry the cause, got %q", err)
	}
}

func TestTransportDoesNotMutateTheCallersRequest(t *testing.T) {

	_, client, url := newAuthStub(t, StaticKey("k"), 200)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := req.Header.Get("api-key"); got != "" {
		t.Fatalf("the caller's request was mutated: api-key = %q", got)
	}
}

func TestTransportWrapsABaseRoundTripper(t *testing.T) {
	var wrapped int32
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		atomic.AddInt32(&wrapped, 1)
		if r.Header.Get("api-key") != "k" {
			t.Errorf("credential not applied before the base transport")
		}
		return &http.Response{StatusCode: 200, Body: http.NoBody, Request: r}, nil
	})

	client := &http.Client{Transport: Transport(StaticKey("k"), base)}
	resp, err := client.Get("https://example.invalid/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if wrapped != 1 {
		t.Fatalf("base transport called %d times, want 1", wrapped)
	}
}

func TestStaticKeySetsTheHeader(t *testing.T) {
	h, err := StaticKey("abc").Headers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h["api-key"] != "abc" {
		t.Fatalf("api-key = %q", h["api-key"])
	}
}

func TestStaticKeyRejectsAnEmptyKey(t *testing.T) {
	if _, err := StaticKey("").Headers(context.Background()); err == nil {
		t.Fatal("expected an error for an empty key")
	}
}

func TestKeyFileRereadsOnEveryRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stub, client, url := newAuthStub(t, KeyFile(path), 200, 200)

	post(t, client, url)

	if err := os.WriteFile(path, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	post(t, client, url)

	got := stub.seen()
	if len(got) != 2 || got[0] != "api-key: first" || got[1] != "api-key: second" {
		t.Fatalf("auth headers = %v, want the rotated key on the second call", got)
	}
}

func TestKeyFileMissingIsAnError(t *testing.T) {
	if _, err := KeyFile(filepath.Join(t.TempDir(), "absent")).Headers(context.Background()); err == nil {
		t.Fatal("expected an error for a missing key file")
	}
}

func TestKeyFileEmptyIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := KeyFile(path).Headers(context.Background()); err == nil {
		t.Fatal("expected an error for an empty key file")
	}
}

func TestTokenIsCachedAcrossRequests(t *testing.T) {
	var fetches int32
	cred := &tokenCredential{how: "test", fetch: func(context.Context, *http.Client) (bearer, error) {
		atomic.AddInt32(&fetches, 1)
		return bearer{value: "tok", expires: time.Now().Add(time.Hour)}, nil
	}}

	for i := 0; i < 5; i++ {
		if _, err := cred.Headers(context.Background()); err != nil {
			t.Fatalf("headers: %v", err)
		}
	}
	if fetches != 1 {
		t.Fatalf("expected 1 token fetch, got %d", fetches)
	}
}

func TestTokenRefreshesBeforeExpiry(t *testing.T) {
	var fetches int32
	cred := &tokenCredential{how: "test", fetch: func(context.Context, *http.Client) (bearer, error) {
		atomic.AddInt32(&fetches, 1)

		return bearer{value: "tok", expires: time.Now().Add(refreshSkew / 2)}, nil
	}}

	for i := 0; i < 3; i++ {
		if _, err := cred.Headers(context.Background()); err != nil {
			t.Fatalf("headers: %v", err)
		}
	}
	if fetches != 3 {
		t.Fatalf("a token inside the refresh skew must not be reused: got %d fetches, want 3", fetches)
	}
}

func TestConcurrentRefreshesAreCoalesced(t *testing.T) {
	var fetches int32
	release := make(chan struct{})
	cred := &tokenCredential{how: "test", fetch: func(context.Context, *http.Client) (bearer, error) {
		atomic.AddInt32(&fetches, 1)
		<-release
		return bearer{value: "tok", expires: time.Now().Add(time.Hour)}, nil
	}}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := cred.Headers(context.Background()); err != nil {
				t.Errorf("headers: %v", err)
			}
		}()
	}

	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if fetches != 1 {
		t.Fatalf("20 concurrent callers produced %d token fetches, want 1", fetches)
	}
}

func TestOneCallerCancellingDoesNotAbortTheShared(t *testing.T) {
	release := make(chan struct{})
	cred := &tokenCredential{how: "test", fetch: func(context.Context, *http.Client) (bearer, error) {
		<-release
		return bearer{value: "tok", expires: time.Now().Add(time.Hour)}, nil
	}}

	ctx, cancel := context.WithCancel(context.Background())
	giveUp := make(chan error, 1)
	go func() { _, err := cred.Headers(ctx); giveUp <- err }()

	patient := make(chan string, 1)
	go func() {
		h, _ := cred.Headers(context.Background())
		patient <- h["authorization"]
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-giveUp; err == nil {
		t.Fatal("the cancelled caller should see its own context error")
	}

	close(release)
	if got := <-patient; got != "Bearer tok" {
		t.Fatalf("the remaining caller got %q; a cancellation must not abort the shared refresh", got)
	}
}

func TestInvalidateForcesRefetch(t *testing.T) {
	cred := &tokenCredential{how: "test", fetch: countingTokens()}

	h1, err := cred.Headers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cred.Invalidate()
	h2, err := cred.Headers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h1["authorization"] == h2["authorization"] {
		t.Fatalf("Invalidate did not force a new token (both %q)", h1["authorization"])
	}
}

func TestTokenFetchErrorNamesTheFlow(t *testing.T) {
	cred := &tokenCredential{how: "IMDS managed identity", fetch: func(context.Context, *http.Client) (bearer, error) {
		return bearer{}, fmt.Errorf("connection refused")
	}}

	_, err := cred.Headers(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "IMDS managed identity") {
		t.Fatalf("error %q should name the flow", err)
	}
}

func TestFromTokenProviderCachesAndDefaultsExpiry(t *testing.T) {
	var calls int32
	cred := FromTokenProvider(func(context.Context) (string, time.Time, error) {
		atomic.AddInt32(&calls, 1)
		return "provided", time.Time{}, nil
	})

	for i := 0; i < 3; i++ {
		h, err := cred.Headers(context.Background())
		if err != nil {
			t.Fatalf("headers: %v", err)
		}
		if h["authorization"] != "Bearer provided" {
			t.Fatalf("authorization = %q", h["authorization"])
		}
	}
	if calls != 1 {
		t.Fatalf("expected the provider to be called once, got %d", calls)
	}
}

func TestTokenEndpointResponseShapes(t *testing.T) {
	cases := []struct {
		name string
		body string
		want time.Duration
	}{
		{"expires_in number", `{"access_token":"t","expires_in":3600}`, time.Hour},
		{"expires_in quoted", `{"access_token":"t","expires_in":"3600"}`, time.Hour},
		{"expires_on quoted unix", fmt.Sprintf(`{"access_token":"t","expires_on":"%d"}`, time.Now().Add(time.Hour).Unix()), time.Hour},
		{"unparseable expiry", `{"access_token":"t","expires_on":"6/1/2026 12:00:00 AM +00:00"}`, defaultTokenLifetime},
		{"no expiry at all", `{"access_token":"t"}`, defaultTokenLifetime},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("content-type", "application/json")
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()

			req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
			tok, err := doTokenRequest(srv.Client(), req)
			if err != nil {
				t.Fatalf("token request: %v", err)
			}
			if tok.value != "t" {
				t.Fatalf("token = %q", tok.value)
			}
			if got := time.Until(tok.expires); got < tc.want-time.Minute || got > tc.want+time.Minute {
				t.Fatalf("lifetime = %v, want about %v", got, tc.want)
			}
		})
	}
}

func TestTokenEndpointErrorIsDescribed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid_client","error_description":"secret is expired"}`)
	}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	if _, err := doTokenRequest(srv.Client(), req); err == nil {
		t.Fatal("expected an error")
	} else if !strings.Contains(err.Error(), "secret is expired") {
		t.Fatalf("error should carry the endpoint's description, got %q", err)
	}
}

func TestTokenEndpointWithoutATokenIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"token_type":"Bearer"}`)
	}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	if _, err := doTokenRequest(srv.Client(), req); err == nil {
		t.Fatal("expected an error when no access_token is returned")
	}
}

func TestResourceOfStripsDefaultScope(t *testing.T) {
	if got := resourceOf(DefaultScope); got != "https://cognitiveservices.azure.com" {
		t.Fatalf("resourceOf = %q", got)
	}
}

func TestEntraFlowSelection(t *testing.T) {
	clear := func(t *testing.T) {
		for _, k := range []string{
			"AZURE_TENANT_ID", "AZURE_CLIENT_ID", "AZURE_CLIENT_SECRET",
			"AZURE_FEDERATED_TOKEN_FILE", "IDENTITY_ENDPOINT", "IDENTITY_HEADER",
		} {
			t.Setenv(k, "")
		}
	}

	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"workload identity", map[string]string{"AZURE_FEDERATED_TOKEN_FILE": "/var/run/token"}, "workload identity federation"},
		{"service principal", map[string]string{"AZURE_CLIENT_SECRET": "s"}, "service principal"},
		{"app service", map[string]string{"IDENTITY_ENDPOINT": "http://localhost/token"}, "App Service managed identity"},
		{"imds then cli by default", nil, "managed identity or the Azure CLI"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clear(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if _, how := entraFlow(DefaultScope); how != tc.want {
				t.Fatalf("flow = %q, want %q", how, tc.want)
			}
		})
	}
}

func TestFromEnvPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte("from-file"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("key file wins", func(t *testing.T) {
		t.Setenv("AZURE_OPENAI_KEY_FILE", path)
		t.Setenv("AZURE_OPENAI_KEY", "inline")
		h, err := FromEnv().Headers(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if h["api-key"] != "from-file" {
			t.Fatalf("api-key = %q, want the file's contents", h["api-key"])
		}
	})

	t.Run("inline key next", func(t *testing.T) {
		t.Setenv("AZURE_OPENAI_KEY_FILE", "")
		t.Setenv("AZURE_OPENAI_KEY", "inline")
		h, err := FromEnv().Headers(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if h["api-key"] != "inline" {
			t.Fatalf("api-key = %q", h["api-key"])
		}
	})

	t.Run("entra id when no key is set", func(t *testing.T) {
		t.Setenv("AZURE_OPENAI_KEY_FILE", "")
		t.Setenv("AZURE_OPENAI_KEY", "")
		if _, ok := FromEnv().(*tokenCredential); !ok {
			t.Fatal("expected an Entra ID credential when no key is configured")
		}
	})
}

func TestDefaultIsASingleton(t *testing.T) {
	t.Setenv("AZURE_OPENAI_KEY", "shared")

	a, b := Default(), Default()
	if a != b {
		t.Fatal("Default must return the same credential every time, so one token cache serves the process")
	}

	if c := FromEnv(); c == a {
		t.Fatal("FromEnv should build a new credential, not return the singleton")
	}
}

func TestDefaultIsSafeUnderConcurrency(t *testing.T) {
	t.Setenv("AZURE_OPENAI_KEY", "shared")

	got := make([]Credential, 32)
	var wg sync.WaitGroup
	for i := range got {
		wg.Add(1)
		go func(i int) { defer wg.Done(); got[i] = Default() }(i)
	}
	wg.Wait()

	for i, c := range got {
		if c != got[0] {
			t.Fatalf("goroutine %d built a second credential", i)
		}
	}
}

type credentialFunc func(context.Context) (map[string]string, error)

func (f credentialFunc) Headers(ctx context.Context) (map[string]string, error) { return f(ctx) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAzureCLIToken(t *testing.T) {
	restore := runAzureCLI
	t.Cleanup(func() { runAzureCLI = restore })

	exp := time.Now().Add(time.Hour).Unix()
	runAzureCLI = func(context.Context, string) ([]byte, error) {
		return fmt.Appendf(nil, `{"accessToken":"cli-token","expires_on":%d,"tokenType":"Bearer"}`, exp), nil
	}

	tok, err := azureCLIToken(context.Background(), "https://cognitiveservices.azure.com")
	if err != nil {
		t.Fatalf("azureCLIToken: %v", err)
	}
	if tok.value != "cli-token" {
		t.Fatalf("token = %q", tok.value)
	}
	if got := time.Until(tok.expires); got < 55*time.Minute || got > time.Hour+time.Minute {
		t.Fatalf("lifetime = %v, want about an hour", got)
	}
}

func TestAzureCLITokenDefaultsExpiry(t *testing.T) {
	restore := runAzureCLI
	t.Cleanup(func() { runAzureCLI = restore })

	runAzureCLI = func(context.Context, string) ([]byte, error) {
		return []byte(`{"accessToken":"cli-token"}`), nil
	}

	tok, err := azureCLIToken(context.Background(), "r")
	if err != nil {
		t.Fatal(err)
	}
	if got := time.Until(tok.expires); got < defaultTokenLifetime-time.Minute {
		t.Fatalf("lifetime = %v, want the default", got)
	}
}

func TestAzureCLIFailureExplainsTheOptions(t *testing.T) {
	restore := runAzureCLI
	t.Cleanup(func() { runAzureCLI = restore })

	runAzureCLI = func(context.Context, string) ([]byte, error) {
		return nil, fmt.Errorf("the Azure CLI is not installed")
	}

	_, err := azureCLIToken(context.Background(), "r")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"no managed identity", "az login", "AZURE_OPENAI_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got %q", want, err)
		}
	}
}

func TestFallsBackToTheCLIWhenIMDSIsUnreachable(t *testing.T) {
	for _, k := range []string{
		"AZURE_TENANT_ID", "AZURE_CLIENT_ID", "AZURE_CLIENT_SECRET",
		"AZURE_FEDERATED_TOKEN_FILE", "IDENTITY_ENDPOINT", "IDENTITY_HEADER",
	} {
		t.Setenv(k, "")
	}

	restore := runAzureCLI
	t.Cleanup(func() { runAzureCLI = restore })
	runAzureCLI = func(context.Context, string) ([]byte, error) {
		return []byte(`{"accessToken":"cli-token","expires_in":3600}`), nil
	}

	fetch, _ := entraFlow(DefaultScope)
	tok, err := fetch(context.Background(), &http.Client{Timeout: time.Second})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if tok.value != "cli-token" {
		t.Fatalf("token = %q, want the CLI to serve it when IMDS is unreachable", tok.value)
	}
}
