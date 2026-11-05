// Package machine 封装对机台数据服务的访问。
//
// 它只关心“如何从机台侧取到可信的目录与字节”，不解析配方、不拼装 HTTP 响应。
// 将来机台侧从 mock 换成真实网关、或增加缓存，只要这一层替换即可。
package machine

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"recipeworkbench/apps/server/internal/apperr"
	"recipeworkbench/packages/recipe"
)

// RecipeFile 是机台目录里的一条配方记录。
type RecipeFile struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Format  string `json:"format"`
	Process string `json:"process"`
}

// Client 通过 HTTP 读取机台服务。
type Client struct {
	baseURL string
	http    *http.Client
	logger  *slog.Logger
}

// New 构造客户端；timeout 为零时沿用 http.Client 的默认行为。
func New(baseURL string, timeout time.Duration, logger *slog.Logger) *Client {
	return NewWithClient(baseURL, &http.Client{Timeout: timeout}, logger)
}

// NewWithClient 允许调用方（测试或未来的连接池）接管底层 HTTP 客户端。
func NewWithClient(baseURL string, client *http.Client, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Client{baseURL: baseURL, http: client, logger: logger}
}

// Machines 读取机台列表。
func (c *Client) Machines(ctx context.Context) ([]RecipeFile, error) {
	return c.catalog(ctx, "/api/machines")
}

// Recipes 读取单台机台的配方列表。
func (c *Client) Recipes(ctx context.Context, machineID string) ([]RecipeFile, error) {
	return c.catalog(ctx, "/api/machines/"+url.PathEscape(machineID)+"/recipes")
}

// Content 读取一份配方的原始字节，保证与机台侧完全一致。
func (c *Client) Content(ctx context.Context, machineID, name string) ([]byte, error) {
	return c.read(ctx, "/api/machines/"+url.PathEscape(machineID)+"/recipes/"+url.PathEscape(name)+"/content")
}

func (c *Client) catalog(ctx context.Context, path string) ([]RecipeFile, error) {
	data, err := c.read(ctx, path)
	if err != nil {
		return nil, err
	}
	// 在边界解码并校验，避免把机台侧的坏 JSON 或错字段原样透给前端。
	var entries []RecipeFile
	if err := json.Unmarshal(data, &entries); err != nil || entries == nil {
		c.logger.Warn("mock_catalog_invalid", "path", path, "err", err)
		return nil, apperr.New(apperr.Upstream, "机台目录格式无效", err)
	}
	for _, entry := range entries {
		if entry.ID == "" || entry.Name == "" || (entry.Format != "pgs" && entry.Format != "dcex") {
			c.logger.Warn("mock_catalog_fields_invalid", "path", path, "entry", entry)
			return nil, apperr.New(apperr.Upstream, "机台目录字段无效", nil)
		}
	}
	return entries, nil
}

func (c *Client) read(ctx context.Context, path string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		c.logger.Error("mock_request_invalid", "path", path, "err", err)
		return nil, apperr.New(apperr.Upstream, "机台服务地址无效", err)
	}
	start := time.Now()
	response, err := c.http.Do(request)
	if err != nil {
		c.logger.Warn("mock_unavailable", "path", path, "err", err, "duration_ms", time.Since(start).Milliseconds())
		return nil, apperr.New(apperr.Upstream, "机台服务暂时不可用，请重试", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		c.logger.Warn("mock_status", "path", path, "status", response.StatusCode)
		return nil, apperr.New(apperr.NotFound, "机台或配方不存在", nil)
	}
	if response.StatusCode != http.StatusOK {
		c.logger.Warn("mock_status", "path", path, "status", response.StatusCode)
		return nil, apperr.New(apperr.Upstream, "机台服务返回异常", nil)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, recipe.MaxFileBytes+1))
	if err != nil || len(data) > recipe.MaxFileBytes {
		c.logger.Error("mock_read_failed", "path", path, "bytes", len(data), "err", err)
		return nil, apperr.New(apperr.Upstream, "机台文件读取失败或超出 10 MiB 限制", err)
	}
	c.logger.Debug("mock_read", "path", path, "bytes", len(data), "duration_ms", time.Since(start).Milliseconds())
	return data, nil
}
