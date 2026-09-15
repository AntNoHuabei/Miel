package websearch

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestSearchCachesAndDeduplicatesResults(t *testing.T) {
	calls := 0
	service := New(Config{ProbeProxy: func(context.Context) bool { return false }})
	service.backends = []*backend{{name: "one", find: func(context.Context, *requester, string) ([]Result, error) {
		calls++
		return []Result{{Title: "first", URL: "https://example.com/a"}, {Title: "duplicate", URL: "https://example.com/a"}}, nil
	}}}
	first, err := service.Search(context.Background(), "miel", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Results) != 1 || first.Cached {
		t.Fatalf("first = %#v", first)
	}
	second, err := service.Search(context.Background(), "miel", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Cached || calls != 1 {
		t.Fatalf("cache result = %#v calls=%d", second, calls)
	}
}

func TestSearchOpensCircuitAfterRepeatedFailure(t *testing.T) {
	failures := 0
	service := New(Config{ProbeProxy: func(context.Context) bool { return false }})
	service.backends = []*backend{
		{name: "broken", find: func(context.Context, *requester, string) ([]Result, error) {
			failures++
			return nil, errors.New("offline")
		}},
		{name: "healthy", find: func(_ context.Context, _ *requester, query string) ([]Result, error) {
			return []Result{{Title: query, URL: "https://example.com/" + query}}, nil
		}},
	}
	for _, query := range []string{"one", "two", "three"} {
		if _, err := service.Search(context.Background(), query, 1); err != nil {
			t.Fatal(err)
		}
	}
	if failures != 2 {
		t.Fatalf("failed backend calls = %d, want 2 after circuit opens", failures)
	}
}

type retryableError struct{}

func (retryableError) Error() string   { return "proxy unavailable" }
func (retryableError) Timeout() bool   { return false }
func (retryableError) Temporary() bool { return true }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRequesterFallsBackOnlyForProxyTransportFailures(t *testing.T) {
	directCalls := 0
	requester := newRequester(context.Background(), true, func(context.Context, string, ...any) {})
	requester.proxied.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, retryableError{} })
	requester.direct.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		directCalls++
		return &http.Response{StatusCode: 200, Body: ioNop("ok"), Header: make(http.Header)}, nil
	})
	if _, err := requester.get(context.Background(), "https://example.com", 16); err != nil {
		t.Fatal(err)
	}
	if directCalls != 1 {
		t.Fatalf("direct calls = %d", directCalls)
	}
	requester.proxied.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Body: ioNop("no"), Header: make(http.Header)}, nil
	})
	if _, err := requester.get(context.Background(), "https://example.com", 16); err == nil {
		t.Fatal("HTTP response must not retry direct")
	}
	if directCalls != 1 {
		t.Fatalf("HTTP failure retried direct: %d", directCalls)
	}
}

func ioNop(value string) io.ReadCloser { return io.NopCloser(strings.NewReader(value)) }

var _ net.Error = retryableError{}
