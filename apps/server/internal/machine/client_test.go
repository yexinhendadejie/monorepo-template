package machine

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"recipeworkbench/apps/server/internal/apperr"
	"recipeworkbench/packages/recipe"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (failingReader) Close() error             { return nil }

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	return New(upstream.URL, time.Second, nil)
}

func kindOf(t *testing.T, err error) apperr.Kind {
	t.Helper()
	kind, ok := apperr.KindOf(err)
	if !ok {
		t.Fatalf("expected a business error, got %v", err)
	}
	return kind
}

func TestCatalogsAndContent(t *testing.T) {
	var seen []string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		switch r.URL.Path {
		case "/api/machines":
			io.WriteString(w, `[{"id":"ABMCPT01","name":"ABMCPT01","format":"pgs","process":"CP"}]`)
		case "/api/machines/ABMCPT01/recipes":
			io.WriteString(w, `[{"id":"sample.pgs","name":"sample.pgs","format":"pgs"}]`)
		case "/api/machines/ABMCPT01/recipes/with space.pgs/content":
			if !strings.Contains(r.RequestURI, "with%20space.pgs") {
				t.Errorf("unescaped path: %s", r.RequestURI)
			}
			io.WriteString(w, "raw-bytes")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	ctx := context.Background()
	machines, err := client.Machines(ctx)
	if err != nil || len(machines) != 1 || machines[0].Process != "CP" {
		t.Fatalf("machines: %+v %v", machines, err)
	}
	recipes, err := client.Recipes(ctx, "ABMCPT01")
	if err != nil || len(recipes) != 1 || recipes[0].Name != "sample.pgs" {
		t.Fatalf("recipes: %+v %v", recipes, err)
	}
	data, err := client.Content(ctx, "ABMCPT01", "with space.pgs")
	if err != nil || string(data) != "raw-bytes" {
		t.Fatalf("content: %q %v", data, err)
	}
	if len(seen) != 3 {
		t.Fatalf("expected three upstream calls, got %v", seen)
	}
}

func TestCatalogValidation(t *testing.T) {
	for _, source := range []string{"HTML error", "null", "{}", `[{}]`, `[{"id":"a","name":"a","format":"exe"}]`, `[{"id":"a","format":"pgs"}]`, `[{"name":"a","format":"pgs"}]`} {
		t.Run(source, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, source) })
			if _, err := client.Machines(context.Background()); kindOf(t, err) != apperr.Upstream {
				t.Fatalf("accepted invalid catalog %q", source)
			}
		})
	}
	t.Run("empty catalog stays valid", func(t *testing.T) {
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "[]") })
		machines, err := client.Machines(context.Background())
		if err != nil || machines == nil || len(machines) != 0 {
			t.Fatalf("empty catalog rejected: %v %v", machines, err)
		}
	})
}

func TestStatusAndTransportErrors(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })
		_, err := client.Content(context.Background(), "M1", "a.pgs")
		if kindOf(t, err) != apperr.NotFound || !strings.Contains(apperr.MessageOf(err), "不存在") {
			t.Fatalf("got %v", err)
		}
	})
	for _, status := range []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				io.WriteString(w, "upstream secret detail")
			})
			_, err := client.Machines(context.Background())
			if kindOf(t, err) != apperr.Upstream || strings.Contains(apperr.MessageOf(err), "secret") {
				t.Fatalf("unexpected disclosure: %v", err)
			}
		})
	}
	for _, tc := range []struct {
		name      string
		base      string
		transport roundTripFunc
	}{
		{"invalid address", ":invalid", func(r *http.Request) (*http.Response, error) {
			t.Fatal("must not execute invalid request")
			return nil, nil
		}},
		{"offline", "http://mock", func(r *http.Request) (*http.Response, error) {
			return nil, errors.New("connection refused")
		}},
		{"body read error", "http://mock", func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: failingReader{}, Header: http.Header{}}, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := NewWithClient(tc.base, &http.Client{Transport: tc.transport}, nil)
			if _, err := client.Machines(context.Background()); kindOf(t, err) != apperr.Upstream {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestOversizedBodiesAreRejected(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, recipe.MaxFileBytes+1))
	})
	_, err := client.Content(context.Background(), "M1", "a.pgs")
	if kindOf(t, err) != apperr.Upstream || !strings.Contains(apperr.MessageOf(err), "10 MiB") {
		t.Fatalf("got %v", err)
	}
}

func TestNilLoggerIsSafe(t *testing.T) {
	client := NewWithClient("http://mock", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})}, nil)
	if _, err := client.Machines(context.Background()); err == nil {
		t.Fatal("expected failure")
	}
}

func TestLoggerReceivesDebugTrace(t *testing.T) {
	var builder strings.Builder
	logger := slog.New(slog.NewTextHandler(&builder, &slog.HandlerOptions{Level: slog.LevelDebug}))
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "[]") })
	client.logger = logger
	if _, err := client.Machines(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(builder.String(), "mock_read") {
		t.Fatalf("missing trace: %s", builder.String())
	}
}
