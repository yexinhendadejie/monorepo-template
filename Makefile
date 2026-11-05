SHELL := /bin/bash

-include .env
export

NODE_VERSION := 22.12
GO_PACKAGES := ./...
WEB_PORT ?= 5173
API_PORT ?= 8080
MOCK_PORT ?= 3101
LOG_DIR ?= logs
svc ?= api

.DEFAULT_GOAL := help
.PHONY: help install doctor dev dev-mock dev-api dev-web build test test-mock test-go test-web \
	cover cover-go cover-web lint fmt check smoke tidy clean clean-all \
	logs logs-api logs-list logs-grep logs-clean \
	deploy deploy-build deploy-up deploy-down deploy-restart deploy-ps deploy-logs

## help: 列出可用命令
help:
	@echo "Recipe Body · 可用命令"
	@echo
	@grep -hE '^## [a-z-]+:' $(MAKEFILE_LIST) | sed -E 's/^## ([a-z-]+): */  make \1 — /' | sort

## install: 安装前端与 Go 依赖
install:
	npm install
	go mod download

## doctor: 检查本机工具版本是否满足要求
doctor:
	@printf 'node    %s (需要 >= %s)\n' "$$(node -v 2>/dev/null || echo 缺失)" "$(NODE_VERSION)"
	@printf 'npm     %s\n' "$$(npm -v 2>/dev/null || echo 缺失)"
	@printf 'go      %s\n' "$$(go version 2>/dev/null | awk '{print $$3}' || echo 缺失)"
	@printf 'docker  %s\n' "$$(docker compose version --short 2>/dev/null || echo 未安装，db 命令不可用)"
	@printf 'make    %s\n' "$$(make -v 2>/dev/null | head -1 | awk '{print $$3}' || echo 缺失)"

## dev: 同时启动机台模拟、Go API 与前端；服务日志写入 logs/，用 make logs 查看
dev:
	@mkdir -p "$(LOG_DIR)"
	LOG_LEVEL="$${LOG_LEVEL:-debug}" LOG_DEST="$${LOG_DEST:-file}" npm run dev

## dev-mock: 只启动机台模拟服务
dev-mock:
	npm run dev -w @recipe/mockserver

## dev-api: 只启动 Go API（Air 热重载）
dev-api:
	npm run dev:air

## dev-web: 只启动前端 Vite
dev-web:
	npm run dev -w @recipe/web

## build: 构建前端产物与 Go 二进制
build:
	npm run build

## test: 运行全部测试
test:
	npm test

## test-mock: 测试机台模拟服务
test-mock:
	npm run test:mock

## test-go: 以 -race 运行 Go 测试
test-go:
	npm run test:go

## test-web: 运行前端测试与覆盖率
test-web:
	npm run test:web

## cover: 生成并汇总覆盖率报告
cover: cover-go cover-web

## cover-go: 生成 Go 覆盖率 HTML（tmp/go-coverage.html）
cover-go:
	mkdir -p tmp
	go test -race -coverprofile=tmp/go-coverage.out $(GO_PACKAGES)
	go tool cover -func=tmp/go-coverage.out | tail -1
	go tool cover -html=tmp/go-coverage.out -o tmp/go-coverage.html

## cover-web: 生成前端覆盖率报告（apps/web/coverage）
cover-web:
	npm run test:web

## lint: 运行 go vet、格式检查与 TypeScript 检查
lint:
	go vet $(GO_PACKAGES)
	@unformatted="$$(gofmt -l apps packages)"; \
	if [ -n "$$unformatted" ]; then echo "需要 gofmt: $$unformatted"; exit 1; fi
	npm run typecheck

## fmt: 格式化 Go 代码
fmt:
	gofmt -w apps packages

## check: 提交前检查（格式、静态检查、全部测试）
check: lint test

## smoke: 用 curl 验证运行中的 API 与前端
smoke:
	@printf 'health   '; curl -fsS http://127.0.0.1:$(API_PORT)/healthz; echo
	@printf 'machines '; curl -fsS http://127.0.0.1:$(API_PORT)/api/machines | head -c 200; echo
	@printf 'web      '; curl -fsS -o /dev/null -w '%{http_code}\n' http://127.0.0.1:$(WEB_PORT)/
	@printf 'machine-data '; ls machine-data 2>/dev/null | wc -l | tr -d ' '; echo ' 项（make sync 拉取）'
	@printf 'mock     '; curl -fsS -o /dev/null -w '%{http_code}\n' http://127.0.0.1:$(MOCK_PORT)/api/machines

## sync: 从云盘同步机台数据到 machine-data/
sync:
	@test -n "$$FTP_HOST" || { echo "缺少 FTP_HOST：复制 .env.example 为 .env 并填写云盘地址"; exit 1; }
	node scripts/sync-machine-data.mjs

## sync-dry: 只预览将要同步的机台文件
sync-dry:
	node scripts/sync-machine-data.mjs --dry-run

## sync-prune: 同步并删除云盘上已不存在的本地机台文件
sync-prune:
	node scripts/sync-machine-data.mjs --prune

## tidy: 整理 Go 依赖
tidy:
	go mod tidy

## logs: 跟随服务端日志（tail -F，轮转后自动续读；svc= 切换服务，默认 api）
logs:
	@log="$(LOG_DIR)/$(svc)/$(svc).log"; \
	test -f "$$log" || { echo "还没有 $${log}，先跑 make dev 让服务写日志"; exit 1; }; \
	echo "跟随 $${log}（Ctrl-C 退出，不影响服务；日志轮转后自动续到新文件）"; \
	tail -n 80 -F "$$log"

## logs-api: logs 的别名，固定跟随 Go API
logs-api:
	@$(MAKE) --no-print-directory logs svc=api

## logs-list: 列出已有日志文件与大小
logs-list:
	@test -d "$(LOG_DIR)" || { echo "还没有日志目录，先跑 make dev"; exit 1; }
	@find "$(LOG_DIR)" -type f -name '*.log' -print0 | xargs -0 ls -lh | awk '{print $$5, $$9}'

## logs-grep: 在 logs/ 中检索关键字（用法 make logs-grep q=http_status）
logs-grep:
	@test -n "$(q)" || { echo "用法: make logs-grep q=关键字"; exit 1; }
	@rg --no-ignore -a --no-heading -n "$(q)" "$(LOG_DIR)" || true

## logs-clean: 删除全部本地日志
logs-clean:
	rm -rf "$(LOG_DIR)"

## clean: 清理构建产物与覆盖率
clean:
	rm -rf apps/web/dist apps/web/coverage apps/server/tmp tmp

## clean-all: 清理产物与 node_modules
clean-all: clean
	rm -rf node_modules apps/*/node_modules packages/*/node_modules

## deploy: 构建并在后台启动 deploy/ 里的生产栈
deploy: deploy-build deploy-up

## deploy-build: 只构建镜像（改完代码后重建用）
deploy-build:
	@cd deploy && test -f .env || { echo "缺少 deploy/.env：执行 cd deploy && cp .env.example .env"; exit 1; }
	@cd deploy && test -f config.toml || { echo "缺少 deploy/config.toml：执行 cd deploy && cp config.example.toml config.toml"; exit 1; }
	cd deploy && docker compose build

## deploy-up: 启动或更新生产栈（幂等，可反复执行）
deploy-up:
	@cd deploy && test -f .env || { echo "缺少 deploy/.env：执行 cd deploy && cp .env.example .env"; exit 1; }
	@cd deploy && test -f config.toml || { echo "缺少 deploy/config.toml：执行 cd deploy && cp config.example.toml config.toml"; exit 1; }
	cd deploy && docker compose up -d --remove-orphans

## deploy-restart: 重启 api，让刚改过的 deploy/config.toml 生效
deploy-restart:
	cd deploy && docker compose restart api

## deploy-down: 停止生产栈，保留日志卷
deploy-down:
	cd deploy && docker compose down

## deploy-ps: 查看生产栈容器与健康状态
deploy-ps:
	cd deploy && docker compose ps

## deploy-logs: 跟随 API 容器日志（Ctrl-C 退出，不影响服务）
deploy-logs:
	cd deploy && docker compose logs -f --tail 80 api
