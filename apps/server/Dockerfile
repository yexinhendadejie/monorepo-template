# 多阶段构建：编译期带完整工具链，运行期只留静态二进制与证书。
FROM golang:1.26-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOOS=linux
COPY go.mod go.sum ./
RUN go mod download
COPY apps/server ./apps/server
COPY packages ./packages
RUN go build -trimpath -ldflags="-s -w" -o /out/recipe-api ./apps/server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
	&& addgroup -g 10001 -S app \
	&& adduser -u 10001 -S -G app -H -h /app app
WORKDIR /app
COPY --from=build /out/recipe-api /app/recipe-api
RUN mkdir -p /app/logs && chown -R app:app /app
USER app
# 运行参数由挂载的 config.toml 提供，这里不设 RECIPE_* / LOG_* 默认值，
# 否则环境变量会压过配置文件。
ENV RECIPE_CONFIG=/app/config.toml \
	TZ=Asia/Shanghai
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
	CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["/app/recipe-api"]
