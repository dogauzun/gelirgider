# Build stage: produce the static frontend/dist bundle.
FROM node:22.23-alpine AS build
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
# The release version, shown in the sidebar; dev when not given.
ARG VERSION=dev
ENV VITE_APP_VERSION=$VERSION
RUN npm run build

# Final stage: serve the static build with nginx, proxying /api to the backend.
# nginx-unprivileged runs as uid 101 instead of root, so it listens on 8080 and
# keeps its pid file and temp paths in /tmp.
FROM nginxinc/nginx-unprivileged:1.31-alpine
COPY --from=build /src/dist /usr/share/nginx/html
# The project's license (AGPL-3.0) and the license notices of the bundled
# dependencies (make notices), served as /LICENSE and /THIRD_PARTY_NOTICES.md.
COPY LICENSE /usr/share/nginx/html/LICENSE
COPY THIRD_PARTY_NOTICES.md /usr/share/nginx/html/THIRD_PARTY_NOTICES.md
COPY deploy/nginx.conf /etc/nginx/templates/default.conf.template

# Name of the backend service/host:port, substituted into nginx.conf at startup
# (the image runs envsubst on templates/*.template before launch).
ENV BACKEND_UPSTREAM=backend:8080

EXPOSE 8080
