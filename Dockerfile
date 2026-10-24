# syntax=docker/dockerfile:1

# ---- frontend ----
FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---- backend (pure Go, cross-compiled; no cgo needed for SQLite) ----
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS app
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/warranteed ./cmd/warranteed
RUN mkdir -p /out/data

# ---- runtime ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=app /out/warranteed /usr/local/bin/warranteed
COPY --from=app --chown=65532:65532 /out/data /data
ENV WARRANTEED_DATA_DIR=/data \
    WARRANTEED_LISTEN=:8080
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["/usr/local/bin/warranteed", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/warranteed"]
