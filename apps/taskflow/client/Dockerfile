# syntax=docker/dockerfile:1
#
# taskflow client image: builds the lit + tailwind SPA and serves it with
# nginx, proxying /api to the server service. Build from services/taskflow/client:
#
#   docker build -t taskflow-client .
#
# or simply: docker compose up --build   (see services/taskflow/docker-compose.yml)

FROM node:24-alpine AS build
WORKDIR /app
RUN npm install -g pnpm@10.33.1 --silent

COPY package.json pnpm-lock.yaml ./
RUN --mount=type=cache,id=pnpm-store,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile

COPY . ./
RUN pnpm build

FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
