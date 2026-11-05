package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestLoggerRecordsLevelsAndFields(t *testing.T) {
	var buffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelDebug}))
	router := gin.New()
	router.Use(RequestLogger(logger))
	router.GET("/ok", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	router.GET("/missing", func(c *gin.Context) { c.JSON(404, gin.H{"error": "nope"}) })
	router.GET("/boom", func(c *gin.Context) { c.JSON(500, gin.H{"error": "failed"}) })

	for _, path := range []string{"/ok", "/missing", "/boom"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	}

	lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 log lines, got %d: %q", len(lines), buffer.String())
	}
	if !strings.Contains(lines[0], "level=INFO") || !strings.Contains(lines[0], "path=/ok") ||
		!strings.Contains(lines[0], "status=200") || !strings.Contains(lines[0], "duration_ms=") {
		t.Fatalf("missing request fields: %q", lines[0])
	}
	if !strings.Contains(lines[1], "level=WARN") || !strings.Contains(lines[1], "status=404") {
		t.Fatalf("4xx should warn: %q", lines[1])
	}
	if !strings.Contains(lines[2], "level=ERROR") || !strings.Contains(lines[2], "status=500") {
		t.Fatalf("5xx should be error: %q", lines[2])
	}
}

func TestRequestLoggerToleratesNilLogger(t *testing.T) {
	router := gin.New()
	router.Use(RequestLogger(nil))
	router.GET("/ok", func(c *gin.Context) { c.Status(204) })
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if recorder.Code != 204 {
		t.Fatalf("nil logger broke the request: %d", recorder.Code)
	}
}
