package forge

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkStaticKey(b *testing.B) {
	c := StaticKey("k")
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := c.Headers(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTokenCached(b *testing.B) {
	c := &tokenCredential{how: "bench", fetch: func(context.Context, *http.Client) (bearer, error) {
		return bearer{value: "tok", expires: time.Now().Add(time.Hour)}, nil
	}}
	ctx := context.Background()
	if _, err := c.Headers(ctx); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := c.Headers(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTokenCachedParallel(b *testing.B) {
	c := &tokenCredential{how: "bench", fetch: func(context.Context, *http.Client) (bearer, error) {
		return bearer{value: "tok", expires: time.Now().Add(time.Hour)}, nil
	}}
	ctx := context.Background()
	if _, err := c.Headers(ctx); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := c.Headers(ctx); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkKeyFile(b *testing.B) {
	path := filepath.Join(b.TempDir(), "key")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		b.Fatal(err)
	}
	c := KeyFile(path)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := c.Headers(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTransportOverhead(b *testing.B) {
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: http.NoBody, Request: r}, nil
	})
	client := &http.Client{Transport: Transport(StaticKey("k"), base)}
	b.ReportAllocs()
	for b.Loop() {
		resp, err := client.Get("https://example.invalid/")
		if err != nil {
			b.Fatal(err)
		}
		resp.Body.Close()
	}
}

func BenchmarkBaselineNoCredential(b *testing.B) {
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: http.NoBody, Request: r}, nil
	})
	client := &http.Client{Transport: base}
	b.ReportAllocs()
	for b.Loop() {
		resp, err := client.Get("https://example.invalid/")
		if err != nil {
			b.Fatal(err)
		}
		resp.Body.Close()
	}
}
