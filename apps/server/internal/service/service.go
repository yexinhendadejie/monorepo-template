// Package service 承载配方的读取用例。
//
// 它把“取字节”和“解析成结构化配方”串起来，并统一错误语义；
// 机台访问细节在 machine 包，HTTP 细节在 httpapi 包，这里只写业务顺序。
package service

import (
	"context"
	"io"
	"log/slog"

	"recipeworkbench/apps/server/internal/apperr"
	"recipeworkbench/apps/server/internal/machine"
	"recipeworkbench/packages/recipe"
)

// MachineReader 是 service 对机台访问层的最小依赖，便于测试替换。
type MachineReader interface {
	Machines(ctx context.Context) ([]machine.RecipeFile, error)
	Recipes(ctx context.Context, machineID string) ([]machine.RecipeFile, error)
	Content(ctx context.Context, machineID, name string) ([]byte, error)
}

// Service 提供只读的配方查询能力。
type Service struct {
	machines MachineReader
	logger   *slog.Logger
}

// New 组装 service；logger 为空时丢弃日志。
func New(machines MachineReader, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Service{machines: machines, logger: logger}
}

// Machines 返回可用机台。
func (s *Service) Machines(ctx context.Context) ([]machine.RecipeFile, error) {
	return s.machines.Machines(ctx)
}

// Recipes 返回指定机台的配方列表。
func (s *Service) Recipes(ctx context.Context, machineID string) ([]machine.RecipeFile, error) {
	return s.machines.Recipes(ctx, machineID)
}

// Recipe 读取并解析一份配方。
func (s *Service) Recipe(ctx context.Context, machineID, name string) (*recipe.Body, error) {
	data, err := s.machines.Content(ctx, machineID, name)
	if err != nil {
		return nil, err
	}
	body, err := recipe.Parse(machineID, name, data)
	if err != nil {
		s.logger.Error("recipe_parse_failed", "machine", machineID, "recipe", name, "bytes", len(data), "err", err)
		return nil, apperr.New(apperr.Parse, "配方解析失败："+err.Error(), err)
	}
	return body, nil
}

// Source 返回配方原始字节，供下载核对。
func (s *Service) Source(ctx context.Context, machineID, name string) ([]byte, error) {
	return s.machines.Content(ctx, machineID, name)
}
