package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
	"recipeworkbench/packages/logging"
)

// DefaultConfigPath 是相对于仓库根目录的配置文件位置，使用 make dev / air 时自动生效。
const DefaultConfigPath = "apps/server/config.toml"

type Config struct {
	Address, MockURL, DatabaseURL string
	Timeout                       time.Duration
	Log                           logging.Config
}

// LoadConfig 读取 TOML 配置，环境变量优先于文件，文件优先于内置默认值。
func LoadConfig() (Config, error) {
	return loadConfig(DefaultConfigPath)
}

func loadConfig(defaultPath string) (Config, error) {
	v := viper.New()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	for key, names := range map[string][]string{
		"server.host":    {"RECIPE_HOST"},
		"server.port":    {"RECIPE_PORT"},
		"server.timeout": {"RECIPE_TIMEOUT"},
		"mock.url":       {"RECIPE_MOCK_URL"},
		"database.url":   {"RECIPE_DATABASE_URL"},
		"log.level":      {"LOG_LEVEL"},
		"log.format":     {"LOG_FORMAT"},
		"log.dest":       {"LOG_DEST"},
		"log.dir":        {"LOG_DIR"},
		"log.rotate_mb":  {"LOG_ROTATE_MB"},
		"log.keep":       {"LOG_KEEP"},
	} {
		if err := v.BindEnv(append([]string{key}, names...)...); err != nil {
			return Config{}, err
		}
	}
	v.SetDefault("server.host", "127.0.0.1")
	v.SetDefault("server.port", "8080")
	v.SetDefault("server.timeout", "5s")
	v.SetDefault("mock.url", "http://127.0.0.1:3101")
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "text")
	v.SetDefault("log.dest", "both")
	v.SetDefault("log.dir", "logs")
	v.SetDefault("log.rotate_mb", 10)
	v.SetDefault("log.keep", 5)

	path, explicit := os.Getenv("RECIPE_CONFIG"), true
	if path == "" {
		path, explicit = defaultPath, false
	}
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		if explicit || !errors.Is(err, fs.ErrNotExist) {
			return Config{}, fmt.Errorf("cannot read %s: %w", path, err)
		}
	}

	host := strings.TrimSpace(v.GetString("server.host"))
	if host == "" || strings.ContainsAny(host, "/ ") {
		return Config{}, fmt.Errorf("RECIPE_HOST must be a host name or address")
	}
	port, err := strconv.Atoi(v.GetString("server.port"))
	if err != nil || port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("RECIPE_PORT must be 1..65535")
	}
	mockURL := strings.TrimRight(v.GetString("mock.url"), "/")
	parsed, err := url.Parse(mockURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return Config{}, fmt.Errorf("RECIPE_MOCK_URL must be an HTTP base URL without credentials or query")
	}
	timeout, err := time.ParseDuration(v.GetString("server.timeout"))
	if err != nil || timeout <= 0 || timeout > time.Minute {
		return Config{}, fmt.Errorf("RECIPE_TIMEOUT must be >0 and <=1m")
	}
	level, err := logging.ParseLevel(v.GetString("log.level"))
	if err != nil {
		return Config{}, err
	}
	logConfig := logging.Config{
		Level:     level,
		Format:    strings.ToLower(v.GetString("log.format")),
		Dest:      strings.ToLower(v.GetString("log.dest")),
		Dir:       v.GetString("log.dir"),
		RotateMB:  int64(v.GetInt("log.rotate_mb")),
		KeepFiles: v.GetInt("log.keep"),
	}
	if err := logConfig.Validate(); err != nil {
		return Config{}, err
	}
	return Config{
		Address:     host + ":" + strconv.Itoa(port),
		MockURL:     mockURL,
		DatabaseURL: v.GetString("database.url"),
		Timeout:     timeout,
		Log:         logConfig,
	}, nil
}
