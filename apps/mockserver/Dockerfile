FROM node:22-alpine
WORKDIR /app
ENV NODE_ENV=production \
	MOCK_HOST=0.0.0.0 \
	MOCK_PORT=3101
COPY apps/mockserver ./
USER node
EXPOSE 3101
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
	CMD wget -qO- http://127.0.0.1:3101/api/machines >/dev/null || exit 1
CMD ["node", "src/index.js"]
