package logging

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadConfigDefaultsAndOverrides(t *testing.T) {
	cfg, err := LoadConfig(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Level != slog.LevelInfo || cfg.Format != "text" || cfg.Dest != "both" || cfg.Dir != "logs" ||
		cfg.RotateMB != 10 || cfg.KeepFiles != 5 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	cfg, err = LoadConfig(env(map[string]string{
		"LOG_LEVEL": "DEBUG", "LOG_FORMAT": "JSON", "LOG_DEST": "file",
		"LOG_DIR": "/tmp/app-logs", "LOG_ROTATE_MB": "2", "LOG_KEEP": "3",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Level != slog.LevelDebug || cfg.Format != "json" || cfg.Dest != "file" ||
		cfg.Dir != "/tmp/app-logs" || cfg.RotateMB != 2 || cfg.KeepFiles != 3 {
		t.Fatalf("overrides ignored: %+v", cfg)
	}
}

func TestLoadConfigRejectsInvalidValues(t *testing.T) {
	cases := []map[string]string{
		{"LOG_LEVEL": "verbose"},
		{"LOG_FORMAT": "xml"},
		{"LOG_DEST": "kafka"},
		{"LOG_ROTATE_MB": "0"},
		{"LOG_ROTATE_MB": "abc"},
		{"LOG_KEEP": "0"},
		{"LOG_KEEP": "101"},
	}
	for _, values := range cases {
		if _, err := LoadConfig(env(values)); err == nil {
			t.Fatalf("accepted invalid config %v", values)
		}
	}
}

func TestParseLevelAcceptsAliases(t *testing.T) {
	for raw, want := range map[string]slog.Level{
		"": slog.LevelInfo, "info": slog.LevelInfo, "debug": slog.LevelDebug,
		"WARN": slog.LevelWarn, "warning": slog.LevelWarn, "error": slog.LevelError,
	} {
		got, err := ParseLevel(raw)
		if err != nil || got != want {
			t.Fatalf("ParseLevel(%q) = %v, %v", raw, got, err)
		}
	}
}

func TestLoggerFiltersByLevelAndWritesText(t *testing.T) {
	var buffer bytes.Buffer
	logger := slog.New(NewHandler("text", slog.LevelWarn, &buffer))
	logger.Info("ignored")
	logger.Debug("ignored too")
	logger.Warn("disk almost full", "free_mb", 12)
	output := buffer.String()
	if strings.Contains(output, "ignored") {
		t.Fatalf("level filter failed: %q", output)
	}
	if !strings.Contains(output, "level=WARN") || !strings.Contains(output, "free_mb=12") {
		t.Fatalf("missing structured fields: %q", output)
	}
}

func TestJSONHandlerProducesParsableLines(t *testing.T) {
	var buffer bytes.Buffer
	logger := slog.New(NewHandler("json", slog.LevelDebug, &buffer))
	logger.Debug("parsed recipe", "machine", "ABMCPT01", "bytes", 4801)
	var record map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &record); err != nil {
		t.Fatalf("invalid json log: %v (%q)", err, buffer.String())
	}
	if record["msg"] != "parsed recipe" || record["machine"] != "ABMCPT01" || record["level"] != "DEBUG" {
		t.Fatalf("unexpected record: %v", record)
	}
}

func TestNewWritesServiceLogAndClosesCleanly(t *testing.T) {
	dir := t.TempDir()
	logger, closer, err := New(Config{Level: slog.LevelDebug, Format: "text", Dest: "file", Dir: dir, RotateMB: 1, KeepFiles: 2}, "api", nil)
	if err != nil {
		t.Fatal(err)
	}
	logger.Debug("hello")
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "api", "api.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "service=api") || !strings.Contains(string(data), "hello") {
		t.Fatalf("missing service field: %q", data)
	}
}

func TestNewRejectsUnwritableDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := New(Config{Level: slog.LevelInfo, Format: "text", Dest: "file", Dir: file, RotateMB: 1, KeepFiles: 1}, "api", nil); err == nil {
		t.Fatal("expected error for unwritable log directory")
	}
}

func TestStdoutOnlyDoesNotCreateFiles(t *testing.T) {
	dir := t.TempDir()
	var buffer bytes.Buffer
	logger, closer, err := New(Config{Level: slog.LevelInfo, Format: "text", Dest: "stdout", Dir: dir, RotateMB: 1, KeepFiles: 1}, "api", &buffer)
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("to stdout")
	closer.Close()
	if !strings.Contains(buffer.String(), "to stdout") {
		t.Fatalf("stdout writer unused: %q", buffer.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "api")); !os.IsNotExist(err) {
		t.Fatalf("stdout mode must not create log files: %v", err)
	}
}

func TestRotatingWriterArchivesAndKeepsLimit(t *testing.T) {
	dir := t.TempDir()
	writer, err := NewRotatingWriter(dir, "api", 120, 2)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("x", 40) + "\n"
	for i := 0; i < 12; i++ {
		if _, err := io.WriteString(writer, line); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	archives, err := os.ReadDir(filepath.Join(dir, "archive"))
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 2 {
		t.Fatalf("expected keep=2 archives, got %d", len(archives))
	}
	current, err := os.ReadFile(filepath.Join(dir, "api.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(current) > 120 {
		t.Fatalf("current log exceeded limit: %d", len(current))
	}
}

func TestRotatingWriterAppendsToExistingFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "archive"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "api.log"), []byte("existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writer, err := NewRotatingWriter(dir, "api", 1024, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(writer, "more\n"); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	data, err := os.ReadFile(filepath.Join(dir, "api.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "existing\nmore\n" {
		t.Fatalf("append semantics broken: %q", data)
	}
}

func TestRotatingWriterRejectsBadLimits(t *testing.T) {
	if _, err := NewRotatingWriter(t.TempDir(), "api", 0, 1); err == nil {
		t.Fatal("zero max accepted")
	}
	if _, err := NewRotatingWriter(t.TempDir(), "api", 10, 0); err == nil {
		t.Fatal("zero keep accepted")
	}
}

func TestSortStringsOrdersArchives(t *testing.T) {
	values := []string{"b", "c", "a"}
	sortStrings(values)
	if strings.Join(values, ",") != "a,b,c" {
		t.Fatalf("unexpected order: %v", values)
	}
}

func TestRotatingWriterReopensAfterClose(t *testing.T) {
	dir := t.TempDir()
	writer, err := NewRotatingWriter(dir, "api", 1024, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(writer, "first\n"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("second close must be a no-op: %v", err)
	}
	if _, err := io.WriteString(writer, "second\n"); err != nil {
		t.Fatalf("write after close must reopen: %v", err)
	}
	writer.Close()
	data, err := os.ReadFile(filepath.Join(dir, "api.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first\nsecond\n" {
		t.Fatalf("reopen lost data: %q", data)
	}
}

func TestConfigValidateChecksAllFields(t *testing.T) {
	valid := Config{Format: "text", Dest: "both", Dir: "logs", RotateMB: 10, KeepFiles: 5}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	for name, mutate := range map[string]func(*Config){
		"format": func(c *Config) { c.Format = "xml" },
		"dest":   func(c *Config) { c.Dest = "kafka" },
		"dir":    func(c *Config) { c.Dir = "  " },
		"rotate": func(c *Config) { c.RotateMB = 0 },
		"keep":   func(c *Config) { c.KeepFiles = 101 },
	} {
		t.Run(name, func(t *testing.T) {
			config := valid
			mutate(&config)
			if err := config.Validate(); err == nil {
				t.Fatalf("invalid %s accepted: %+v", name, config)
			}
		})
	}
}
