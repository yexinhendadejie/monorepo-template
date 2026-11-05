FROM node:22-alpine AS build
WORKDIR /src
ENV npm_config_update_notifier=false
COPY package.json package-lock.json ./
COPY apps/web/package.json apps/web/package.json
COPY apps/mockserver/package.json apps/mockserver/package.json
COPY packages/contracts/package.json packages/contracts/package.json
COPY packages/ui/package.json packages/ui/package.json
RUN npm ci --no-audit --no-fund
COPY apps/web ./apps/web
COPY packages ./packages
RUN npm run build -w @recipe/web

FROM nginx:1.27-alpine
COPY deploy/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/apps/web/dist /usr/share/nginx/html
EXPOSE 80
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
	CMD wget -qO- http://127.0.0.1/healthz >/dev/null || exit 1
