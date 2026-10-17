# syntax=docker/dockerfile:1
# Carbon Cloud web console image.
#
# Multi-stage build: compiles SvelteKit static build with oven/bun,
# serves SPA via unprivileged nginx on port 4173.

FROM oven/bun:1-alpine AS builder
WORKDIR /app

COPY package.json bun.lock* ./
RUN bun install --frozen-lockfile

COPY . .
RUN bun run build

FROM nginxinc/nginx-unprivileged:alpine-slim
COPY --from=builder /app/build /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 4173
CMD ["nginx", "-g", "daemon off;"]
