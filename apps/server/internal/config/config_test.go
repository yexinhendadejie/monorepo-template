package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var envKeys = []string{
	"RECIPE_HOST", "RECIPE_PORT", "RECIPE_MOCK_URL", "RECIPE_TIMEOUT", "RECIPE_DATABASE_URL",
	"LOG_LEVEL", "LOG_FORMAT", "LOG_DEST", "LOG_DIR", "LOG_ROTATE_MB", "LOG_KEEP",
}

// configEnv isolates tests from a developer's local config.toml.
func configEnv(t *testing.T) {
	t.Helper()
	t.Setenv("RECIPE_CONFIG", writeConfig(t, ""))
	for _, key := range envKeys {
		t.Setenv(key, "")
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConfigDefaults(t *testing.T) {
	configEnv(t)
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Address != "127.0.0.1:8080" || config.MockURL != "http://127.0.0.1:3101" || config.Timeout != 5*time.Second || config.DatabaseURL != "" {
		t.Fatalf("unexpected defaults: %+v", config)
	}
	if config.Log.Level != slog.LevelInfo || config.Log.Format != "text" || config.Log.Dest != "both" ||
		config.Log.Dir != "logs" || config.Log.RotateMB != 10 || config.Log.KeepFiles != 5 {
		t.Fatalf("unexpected defaults: %+v", config)
	}
}

func TestConfigEnvironmentOverrides(t *testing.T) {
	configEnv(t)
	t.Setenv("RECIPE_PORT", "18080")
	t.Setenv("RECIPE_MOCK_URL", "https://machine.example/internal/")
	t.Setenv("RECIPE_TIMEOUT", "750ms")
	t.Setenv("RECIPE_DATABASE_URL", "postgres://example")
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Address != "127.0.0.1:18080" || config.MockURL != "https://machine.example/internal" || config.Timeout != 750*time.Millisecond || config.DatabaseURL != "postgres://example" {
		t.Fatal("override not applied")
	}
}

func TestInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct{ key, value, message string }{
		{"RECIPE_PORT", "x", "RECIPE_PORT"}, {"RECIPE_PORT", "0", "RECIPE_PORT"}, {"RECIPE_PORT", "65536", "RECIPE_PORT"},
		{"RECIPE_MOCK_URL", ":bad", "RECIPE_MOCK_URL"}, {"RECIPE_MOCK_URL", "file:///tmp/data", "RECIPE_MOCK_URL"},
		{"RECIPE_MOCK_URL", "https://", "RECIPE_MOCK_URL"}, {"RECIPE_MOCK_URL", "http://u:secret@example.com", "RECIPE_MOCK_URL"},
		{"RECIPE_MOCK_URL", "http://example.com?q=1", "RECIPE_MOCK_URL"}, {"RECIPE_MOCK_URL", "http://example.com#part", "RECIPE_MOCK_URL"},
		{"RECIPE_TIMEOUT", "bad", "RECIPE_TIMEOUT"}, {"RECIPE_TIMEOUT", "0s", "RECIPE_TIMEOUT"},
		{"RECIPE_TIMEOUT", "-1s", "RECIPE_TIMEOUT"}, {"RECIPE_TIMEOUT", "61s", "RECIPE_TIMEOUT"},
	} {
		t.Run(tc.key+"/"+tc.value, func(t *testing.T) {
			configEnv(t)
			t.Setenv(tc.key, tc.value)
			_, err := LoadConfig()
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("wanted %s, got %v", tc.message, err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("error leaked a credential")
			}
		})
	}
}

func TestConfigReadsTOMLFile(t *testing.T) {
	configEnv(t)
	t.Setenv("RECIPE_CONFIG", writeConfig(t, `
[server]
host = "0.0.0.0"
port = 9090
timeout = "2s"

[mock]
url = "https://mock.internal/"

[database]
url = "postgres://example/recipe"

[log]
level = "debug"
format = "json"
dest = "file"
dir = "/var/log/recipe"
rotate_mb = 64
keep = 9
`))
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Address != "0.0.0.0:9090" || config.MockURL != "https://mock.internal" ||
		config.Timeout != 2*time.Second || config.DatabaseURL != "postgres://example/recipe" {
		t.Fatalf("file values ignored: %+v", config)
	}
	if config.Log.Level != slog.LevelDebug || config.Log.Format != "json" || config.Log.Dest != "file" ||
		config.Log.Dir != "/var/log/recipe" || config.Log.RotateMB != 64 || config.Log.KeepFiles != 9 {
		t.Fatalf("log section ignored: %+v", config.Log)
	}
}

func TestEnvironmentOverridesTOMLFile(t *testing.T) {
	configEnv(t)
	t.Setenv("RECIPE_CONFIG", writeConfig(t, `
[server]
port = 9090

[mock]
url = "https://from-file.internal"

[log]
level = "error"
`))
	t.Setenv("RECIPE_PORT", "7070")
	t.Setenv("RECIPE_MOCK_URL", "https://from-env.internal")
	t.Setenv("LOG_LEVEL", "warn")
	config, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.Address != "127.0.0.1:7070" || config.MockURL != "https://from-env.internal" || config.Log.Level != slog.LevelWarn {
		t.Fatalf("env must win over file: %+v", config)
	}
}

func TestExplicitConfigFileMustExistAndParse(t *testing.T) {
	configEnv(t)
	t.Setenv("RECIPE_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "missing.toml") {
		t.Fatalf("missing explicit config must fail loudly: %v", err)
	}
	t.Setenv("RECIPE_CONFIG", writeConfig(t, "server = ["))
	if _, err := LoadConfig(); err == nil {
		t.Fatal("malformed TOML accepted")
	}
}

func TestInvalidLogSectionIsRejected(t *testing.T) {
	for name, body := range map[string]string{
		"level":  "[log]\nlevel = \"verbose\"\n",
		"format": "[log]\nformat = \"xml\"\n",
		"dest":   "[log]\ndest = \"kafka\"\n",
		"rotate": "[log]\nrotate_mb = 0\n",
		"keep":   "[log]\nkeep = 0\n",
		"dir":    "[log]\ndir = \"\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			configEnv(t)
			t.Setenv("RECIPE_CONFIG", writeConfig(t, body))
			if _, err := LoadConfig(); err == nil {
				t.Fatalf("accepted invalid log config: %q", body)
			}
		})
	}
}

func TestInvalidHostIsRejected(t *testing.T) {
	configEnv(t)
	t.Setenv("RECIPE_HOST", "bad host/name")
	if _, err := LoadConfig(); err == nil || !strings.Contains(err.Error(), "RECIPE_HOST") {
		t.Fatalf("expected host error, got %v", err)
	}
}

func TestExampleConfigFileParses(t *testing.T) {
	for _, key := range envKeys {
		t.Setenv(key, "")
	}
	t.Setenv("RECIPE_CONFIG", "../../config.example.toml")
	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("config.example.toml is invalid: %v", err)
	}
	if config.Address != "127.0.0.1:8080" || config.Log.Level != slog.LevelInfo || config.Log.Dir != "logs" {
		t.Fatalf("example drifted from documented defaults: %+v", config)
	}
}

func TestDeployConfigFileParses(t *testing.T) {
	for _, key := range envKeys {
		t.Setenv(key, "")
	}
	t.Setenv("RECIPE_CONFIG", "../../../../deploy/config.example.toml")
	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("deploy/config.example.toml is invalid: %v", err)
	}
	if config.Address != "0.0.0.0:8080" || config.MockURL != "http://mock:3101" || config.Timeout != 10*time.Second {
		t.Fatalf("deploy example drifted: %+v", config)
	}
	if config.Log.Format != "json" || config.Log.Dest != "both" || config.Log.Dir != "/app/logs" || config.Log.KeepFiles != 5 {
		t.Fatalf("deploy example drifted: %+v", config.Log)
	}
}
