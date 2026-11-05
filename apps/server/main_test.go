package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// isolateConfig 防止开发者本机的 config.toml 影响测试结果。
func isolateConfig(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RECIPE_CONFIG", path)
}

func TestServeHandlesRequestsAndStopsOnCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			_, _ = w.Write([]byte("alive"))
		}))
	}()
	response, err := http.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not complete")
	}
	if _, err := http.Get("http://" + listener.Addr().String()); err == nil {
		t.Fatal("listener still accepting after shutdown")
	}
}

func TestServeReportsListenerFailures(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = listener.Close()
	err = Serve(context.Background(), listener, httptest.NewServer(http.NotFoundHandler()).Config.Handler)
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected closed listener error, got %v", err)
	}
}

func TestDatabaseIsOptionalAndNeverCreatesTables(t *testing.T) {
	db, err := OpenDatabase("")
	if db != nil || err != nil {
		t.Fatalf("read pipeline must work without a database: %v", err)
	}
	db, err = OpenDatabase("host=127.0.0.1 port=1 user=fixture dbname=fixture sslmode=disable")
	if err != nil {
		t.Fatal("configuration should not dial PostgreSQL", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if sqlDB.Stats().OpenConnections != 0 {
		t.Fatal("optional storage must not connect or migrate")
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = OpenDatabase("postgres://%zz")
	if err == nil {
		t.Fatal("malformed DSN accepted")
	}
}

func freeAddress(t *testing.T) (string, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	address := "127.0.0.1:" + strconv.Itoa(port)
	return address, strconv.Itoa(port)
}

func TestRunServesUntilContextCancellation(t *testing.T) {
	isolateConfig(t)
	address, port := freeAddress(t)
	t.Setenv("RECIPE_PORT", port)
	t.Setenv("RECIPE_MOCK_URL", "http://127.0.0.1:1")
	t.Setenv("LOG_DEST", "stdout")
	t.Setenv("LOG_DIR", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	var body string
	for attempt := 0; attempt < 100; attempt++ {
		response, err := http.Get("http://" + address + "/healthz")
		if err == nil {
			bytes, _ := io.ReadAll(response.Body)
			response.Body.Close()
			body = string(bytes)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(body, `"ok"`) {
		t.Fatalf("health endpoint never became ready: %q", body)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not stop")
	}
}

func TestRunRejectsInvalidConfigurationAndBusyPort(t *testing.T) {
	isolateConfig(t)
	t.Setenv("RECIPE_PORT", "invalid")
	if err := run(context.Background()); err == nil {
		t.Fatal("invalid configuration accepted")
	}
	_, port := freeAddress(t)
	busy, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	t.Setenv("RECIPE_PORT", port)
	if err := run(context.Background()); err == nil {
		t.Fatal("busy port accepted")
	}
	t.Setenv("RECIPE_PORT", port)
	t.Setenv("RECIPE_DATABASE_URL", "postgres://%zz")
	if err := run(context.Background()); err == nil || !strings.Contains(err.Error(), "database") {
		t.Fatalf("invalid database configuration accepted: %v", err)
	}
}

func TestRunRejectsInvalidLogConfiguration(t *testing.T) {
	isolateConfig(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RECIPE_PORT", strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))
	listener.Close()
	t.Setenv("LOG_LEVEL", "verbose")
	if err := run(context.Background()); err == nil || !strings.Contains(err.Error(), "LOG_LEVEL") {
		t.Fatalf("expected log config error, got %v", err)
	}
}

func TestRunLogsStartupToConfiguredDirectory(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	t.Setenv("RECIPE_PORT", strconv.Itoa(port))
	t.Setenv("RECIPE_MOCK_URL", "http://127.0.0.1:1")
	t.Setenv("LOG_DEST", "file")
	t.Setenv("LOG_DIR", dir)
	t.Setenv("LOG_FORMAT", "json")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, readErr := os.ReadFile(filepath.Join(dir, "api", "api.log"))
		if readErr == nil && bytes.Contains(data, []byte("server_started")) {
			cancel()
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("startup log missing: %v", readErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := <-done; err != nil {
		t.Fatalf("run returned %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "api", "api.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\"log_level\":\"INFO\"") {
		t.Fatalf("structured startup log missing level: %q", data)
	}
}

func TestRunAcceptsConfiguredDatabaseWithoutDialing(t *testing.T) {
	isolateConfig(t)
	address, port := freeAddress(t)
	_ = address
	t.Setenv("RECIPE_PORT", port)
	t.Setenv("RECIPE_MOCK_URL", "http://127.0.0.1:1")
	t.Setenv("LOG_DEST", "stdout")
	t.Setenv("LOG_DIR", t.TempDir())
	// 不可达地址仍然被接受：首期只建立连接池，不做自动 ping。
	t.Setenv("RECIPE_DATABASE_URL", "postgres://user:pass@127.0.0.1:1/recipe?sslmode=disable")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, err := http.Get("http://" + address + "/healthz")
		if err == nil {
			response.Body.Close()
			cancel()
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("server never became ready: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not stop")
	}
}
