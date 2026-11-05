package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"recipeworkbench/apps/server/internal/apperr"
	"recipeworkbench/apps/server/internal/machine"
	"recipeworkbench/apps/server/internal/service"
	"recipeworkbench/packages/recipe"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// routerFor 用真实的 machine 客户端指向假上游，覆盖从 HTTP 到解析的整条链路。
func routerFor(t *testing.T, handler http.HandlerFunc) http.Handler {
	t.Helper()
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	return routerForClient(t, machine.New(upstream.URL, time.Second, nil))
}

func routerForClient(t *testing.T, client *machine.Client) http.Handler {
	t.Helper()
	router, err := NewRouter(service.New(client, nil), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return router
}

func request(router http.Handler, method, path string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(method, path, nil))
	return response
}

func TestCatalogAndReadOnlyAPI(t *testing.T) {
	router := routerFor(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/machines":
			io.WriteString(w, `[{"id":"ABMCPT01","name":"ABMCPT01","format":"pgs","process":"CP"}]`)
		case "/api/machines/ABMCPT01/recipes":
			io.WriteString(w, `[{"id":"sample.pgs","name":"sample.pgs","format":"pgs"}]`)
		default:
			t.Errorf("unexpected upstream path %s", r.URL.Path)
		}
	})
	for _, path := range []string{"/healthz", "/api/machines", "/api/machines/ABMCPT01/recipes"} {
		response := request(router, "GET", path)
		if response.Code != 200 || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
		}
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		response := request(router, method, "/api/machines")
		if response.Code != 405 {
			t.Fatalf("%s: got %d", method, response.Code)
		}
	}
	if response := request(router, "GET", "/api/unknown"); response.Code != 404 {
		t.Fatal(response.Code)
	}
}

func TestAllMachineFilesConvertAndDownloadWithoutMutation(t *testing.T) {
	paths, err := filepath.Glob("../../../mockserver/testdata/*/*")
	if err != nil || len(paths) == 0 {
		t.Fatalf("missing sample data: %d %v", len(paths), err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			machineID, name := filepath.Base(filepath.Dir(path)), filepath.Base(path)
			expectedPath := "/api/machines/" + machineID + "/recipes/" + name + "/content"
			router := routerFor(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != expectedPath || r.Method != "GET" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				w.Write(raw)
			})
			endpoint := "/api/machines/" + machineID + "/recipes/" + name
			response := request(router, "GET", endpoint)
			if response.Code != 200 {
				t.Fatal(response.Body.String())
			}
			var body recipe.Body
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.MachineID != machineID || body.SourceName != name || len(body.Tests) == 0 || len(body.SHA256) != 64 {
				t.Fatalf("broken API contract: %+v", body)
			}
			download := request(router, "GET", endpoint+"/source")
			if download.Code != 200 || !bytes.Equal(download.Body.Bytes(), raw) {
				t.Fatal("download altered source bytes")
			}
			if !strings.Contains(download.Header().Get("Content-Disposition"), name) || download.Header().Get("Content-Type") != "application/octet-stream" {
				t.Fatal("download headers missing")
			}
		})
	}
}

func TestUpstreamFailureStatusesAreBoundedAndReadable(t *testing.T) {
	for _, status := range []int{404, 400, 500, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			router := routerFor(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				io.WriteString(w, "upstream secret detail")
			})
			for _, path := range []string{"/api/machines", "/api/machines/M1/recipes", "/api/machines/M1/recipes/a.pgs", "/api/machines/M1/recipes/a.pgs/source"} {
				response := request(router, "GET", path)
				expected := 502
				if status == 404 {
					expected = 404
				}
				if response.Code != expected || strings.Contains(response.Body.String(), "secret") {
					t.Fatalf("unexpected upstream disclosure or status: %d %s", response.Code, response.Body.String())
				}
			}
		})
	}
}

func TestInvalidCatalogsAreNotRelayedAsSuccess(t *testing.T) {
	for _, source := range []string{"HTML error", "null", "{}", `[{}]`, `[{"id":"a","name":"a","format":"exe"}]`, `[{"id":"a","format":"pgs"}]`, `[{"name":"a","format":"pgs"}]`} {
		t.Run(source, func(t *testing.T) {
			router := routerFor(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, source) })
			if response := request(router, "GET", "/api/machines"); response.Code != 502 {
				t.Fatalf("accepted invalid catalog: %s", source)
			}
		})
	}
	router := routerFor(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "[]") })
	if response := request(router, "GET", "/api/machines"); response.Code != 200 || response.Body.String() != "[]" {
		t.Fatal("valid empty catalogs must remain empty")
	}
}

func TestMalformedAndOversizedRecipes(t *testing.T) {
	t.Run("parse failure", func(t *testing.T) {
		router := routerFor(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "damaged recipe") })
		response := request(router, "GET", "/api/machines/M1/recipes/a.pgs")
		if response.Code != 422 || !strings.Contains(response.Body.String(), "配方解析失败") {
			t.Fatal(response.Code, response.Body.String())
		}
	})
	t.Run("size limit", func(t *testing.T) {
		router := routerFor(t, func(w http.ResponseWriter, r *http.Request) {
			w.Write(bytes.Repeat([]byte("x"), recipe.MaxFileBytes+1))
		})
		response := request(router, "GET", "/api/machines/M1/recipes/a.pgs")
		if response.Code != 502 || !strings.Contains(response.Body.String(), "10 MiB") {
			t.Fatal(response.Code, response.Body.String())
		}
	})
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (failingReader) Close() error             { return nil }

func TestGatewayTransportAndReadErrors(t *testing.T) {
	for _, tc := range []struct {
		name, base string
		transport  roundTripFunc
	}{
		{"invalid address", ":invalid", func(r *http.Request) (*http.Response, error) {
			t.Fatal("must not execute invalid request")
			return nil, nil
		}},
		{"offline", "http://mock", func(r *http.Request) (*http.Response, error) { return nil, errors.New("connection refused") }},
		{"body read error", "http://mock", func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: failingReader{}, Header: http.Header{}}, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := routerForClient(t, machine.NewWithClient(tc.base, &http.Client{Transport: tc.transport}, nil))
			response := request(router, "GET", "/api/machines")
			if response.Code != 502 {
				t.Fatalf("got %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestFilenamesAreEscapedAsPathSegments(t *testing.T) {
	router := routerFor(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.RequestURI, "with%20space.pgs/content") {
			t.Errorf("unescaped path: %s", r.RequestURI)
		}
		w.WriteHeader(404)
	})
	if response := request(router, "GET", "/api/machines/M1/recipes/with%20space.pgs/source"); response.Code != 404 {
		t.Fatal(response.Code)
	}
}

func TestWriteErrorMapsKindsAndHidesUnknownDetails(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		body   string
	}{
		{"not found", apperr.New(apperr.NotFound, "机台或配方不存在", nil), 404, "机台或配方不存在"},
		{"parse", apperr.New(apperr.Parse, "配方解析失败：bad", nil), 422, "配方解析失败：bad"},
		{"invalid", apperr.New(apperr.Invalid, "参数不合法", nil), 400, "参数不合法"},
		{"upstream", apperr.New(apperr.Upstream, "机台服务暂时不可用，请重试", errors.New("dial tcp")), 502, "机台服务暂时不可用，请重试"},
		{"unknown kind", apperr.New(apperr.Kind(99), "未知错误", nil), 500, "未知错误"},
		{"plain error", errors.New("internal detail"), 500, "服务器内部错误"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/machines", nil)
			writeError(c, tc.err)
			if recorder.Code != tc.status || !strings.Contains(recorder.Body.String(), tc.body) {
				t.Fatalf("got %d %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestEmptyMachineListIsSerialisedAsArray(t *testing.T) {
	router, err := NewRouter(service.New(emptyReader{}, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	response := request(router, "GET", "/api/machines")
	if response.Code != 200 || response.Body.String() != "[]" {
		t.Fatalf("empty list should be []: %d %s", response.Code, response.Body.String())
	}
}

type emptyReader struct{}

func (emptyReader) Machines(context.Context) ([]machine.RecipeFile, error) {
	return []machine.RecipeFile{}, nil
}
func (emptyReader) Recipes(context.Context, string) ([]machine.RecipeFile, error) {
	return nil, nil
}
func (emptyReader) Content(context.Context, string, string) ([]byte, error) { return nil, nil }
