// Package logging 提供统一的日志配置：等级、格式、输出目标和按大小归档。
package logging

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config 描述日志行为，全部可由环境变量覆盖，便于生产部署。
type Config struct {
	Level     slog.Level
	Format    string // text | json
	Dest      string // stdout | file | both
	Dir       string
	RotateMB  int64
	KeepFiles int
}

const (
	defaultRotateMB  = 10
	defaultKeepFiles = 5
)

// LoadConfig 从 getenv 读取 LOG_* 变量，缺省值面向本地开发。
func LoadConfig(getenv func(string) string) (Config, error) {
	level, err := ParseLevel(getenv("LOG_LEVEL"))
	if err != nil {
		return Config{}, err
	}
	format := strings.ToLower(strings.TrimSpace(getenv("LOG_FORMAT")))
	if format == "" {
		format = "text"
	}
	dest := strings.ToLower(strings.TrimSpace(getenv("LOG_DEST")))
	if dest == "" {
		dest = "both"
	}
	dir := strings.TrimSpace(getenv("LOG_DIR"))
	if dir == "" {
		dir = "logs"
	}
	rotate := int64(defaultRotateMB)
	if raw := strings.TrimSpace(getenv("LOG_ROTATE_MB")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 1 || value > 1024 {
			return Config{}, fmt.Errorf("LOG_ROTATE_MB must be 1..1024")
		}
		rotate = value
	}
	keep := defaultKeepFiles
	if raw := strings.TrimSpace(getenv("LOG_KEEP")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			return Config{}, fmt.Errorf("LOG_KEEP must be 1..100")
		}
		keep = value
	}
	config := Config{Level: level, Format: format, Dest: dest, Dir: dir, RotateMB: rotate, KeepFiles: keep}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

// Validate 校验格式、目标与归档参数是否可用。
func (c Config) Validate() error {
	if c.Format != "text" && c.Format != "json" {
		return fmt.Errorf("LOG_FORMAT must be text or json")
	}
	if c.Dest != "stdout" && c.Dest != "file" && c.Dest != "both" {
		return fmt.Errorf("LOG_DEST must be stdout, file or both")
	}
	if strings.TrimSpace(c.Dir) == "" {
		return fmt.Errorf("LOG_DIR must not be empty")
	}
	if c.RotateMB < 1 || c.RotateMB > 1024 {
		return fmt.Errorf("LOG_ROTATE_MB must be 1..1024")
	}
	if c.KeepFiles < 1 || c.KeepFiles > 100 {
		return fmt.Errorf("LOG_KEEP must be 1..100")
	}
	return nil
}

// ParseLevel 接受 debug/info/warn/error，空值表示 info。
func ParseLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("LOG_LEVEL must be debug, info, warn or error")
}

// New 构建按 service 分目录写入的 logger。返回的 closer 负责刷新并关闭文件。
func New(cfg Config, service string, stdout io.Writer) (*slog.Logger, io.Closer, error) {
	writers := []io.Writer{}
	var closer io.Closer = nopCloser{}
	var file *rotatingWriter
	if cfg.Dest != "stdout" {
		created, err := NewRotatingWriter(filepath.Join(cfg.Dir, service), service, cfg.RotateMB<<20, cfg.KeepFiles)
		if err != nil {
			return nil, nil, err
		}
		file = created
		closer = created
	}
	if cfg.Dest != "file" {
		if stdout == nil {
			stdout = os.Stdout
		}
		writers = append(writers, stdout)
	}
	if file != nil {
		writers = append(writers, file)
	}
	handler := NewHandler(cfg.Format, cfg.Level, io.MultiWriter(writers...))
	return slog.New(handler).With("service", service), closer, nil
}

// NewHandler 按格式返回写往 w 的 slog handler。
func NewHandler(format string, level slog.Level, w io.Writer) slog.Handler {
	options := &slog.HandlerOptions{Level: level}
	if format == "json" {
		return slog.NewJSONHandler(w, options)
	}
	return slog.NewTextHandler(w, options)
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

type rotatingWriter struct {
	mu      sync.Mutex
	dir     string
	service string
	max     int64
	keep    int
	file    *os.File
	size    int64
}

// NewRotatingWriter 在 dir 下维护当前日志文件，超过 max 字节后归档到 dir/archive。
func NewRotatingWriter(dir, service string, max int64, keep int) (*rotatingWriter, error) {
	if max <= 0 {
		return nil, errors.New("max bytes must be positive")
	}
	if keep < 1 {
		return nil, errors.New("keep must be at least 1")
	}
	if err := os.MkdirAll(filepath.Join(dir, "archive"), 0o755); err != nil {
		return nil, err
	}
	w := &rotatingWriter{dir: dir, service: service, max: max, keep: keep}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *rotatingWriter) open() error {
	file, err := os.OpenFile(filepath.Join(w.dir, w.service+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	w.file = file
	w.size = info.Size()
	return nil
}

func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if w.size > 0 && w.size+int64(len(p)) > w.max {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingWriter) rotate() error {
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return err
		}
		w.file = nil
	}
	stamp := time.Now().UTC().Format("20060102-150405.000000000")
	archive := filepath.Join(w.dir, "archive", w.service+"-"+stamp+".log")
	if err := os.Rename(filepath.Join(w.dir, w.service+".log"), archive); err != nil && !os.IsNotExist(err) {
		return err
	}
	w.size = 0
	if err := w.open(); err != nil {
		return err
	}
	return w.prune()
}

func (w *rotatingWriter) prune() error {
	entries, err := os.ReadDir(filepath.Join(w.dir, "archive"))
	if err != nil {
		return err
	}
	archives := []string{}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), w.service+"-") && strings.HasSuffix(entry.Name(), ".log") {
			archives = append(archives, entry.Name())
		}
	}
	if len(archives) <= w.keep {
		return nil
	}
	sortStrings(archives)
	for _, name := range archives[:len(archives)-w.keep] {
		if err := os.Remove(filepath.Join(w.dir, "archive", name)); err != nil {
			return err
		}
	}
	return nil
}

// Close 关闭当前日志文件。
func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
