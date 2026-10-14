# Build stage: compile a static Go binary (CGO disabled, matches modernc.org/sqlite's no-cgo driver).
FROM golang:1.26 AS build
ARG VERSION=dev
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -ldflags "-X main.version=$VERSION" -o /out/server ./cmd/server

# Final stage: distroless static image, just the binary, run as the image's
# nonroot user (uid/gid 65532). The /data mount must be owned by that uid,
# see deploy/README.md.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
# The project's license (AGPL-3.0) and the license notices of the dependencies
# compiled into the binary (make notices).
COPY LICENSE /LICENSE
COPY THIRD_PARTY_NOTICES.md /THIRD_PARTY_NOTICES.md

# DB_PATH's SQLite file should live on a mounted volume, e.g.:
#   docker run -v gelirgider-data:/data -e DB_PATH=/data/gelirgider.db ...
VOLUME ["/data"]

ENV PORT=8080
EXPOSE 8080

# Distroless has no shell/curl, so the healthcheck is a mode of the binary
# itself (see main.go -healthcheck): one GET to /api/health, exit 0/1.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/server", "-healthcheck"]

ENTRYPOINT ["/server"]
