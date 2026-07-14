# syntax=docker/dockerfile:1.7

FROM golang:1.26.5-bookworm@sha256:18aedc16aa19b3fd7ded7245fc14b109e054d65d22ed53c355c899582bbb2113 AS builder

WORKDIR /src

ARG TARGETOS=linux
ARG TARGETARCH=amd64
# Render exposes RENDER_GIT_COMMIT to Docker builds. Explicit VERSION,
# REVISION and CREATED arguments from release workflows take precedence.
ARG RENDER_GIT_COMMIT=unknown
ARG VERSION=
ARG REVISION=
ARG CREATED=

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    go mod download && \
    go mod verify

COPY cmd ./cmd
COPY internal ./internal

RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    set -eu; \
    resolved_revision="${REVISION:-${RENDER_GIT_COMMIT}}"; \
    if [ -z "${resolved_revision}" ]; then resolved_revision="unknown"; fi; \
    resolved_version="${VERSION}"; \
    if [ -z "${resolved_version}" ]; then \
      if [ "${resolved_revision}" = "unknown" ]; then \
        resolved_version="dev"; \
      else \
        resolved_version="$(printf '%s' "${resolved_revision}" | cut -c1-12)"; \
      fi; \
    fi; \
    resolved_created="${CREATED:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"; \
    printf '{"version":"%s","revision":"%s","created":"%s"}\n' \
      "${resolved_version}" "${resolved_revision}" "${resolved_created}" \
      > /out-build-metadata.json; \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -buildvcs=false \
      -ldflags="-s -w -X main.version=${resolved_version} -X main.revision=${resolved_revision} -X main.created=${resolved_created}" \
      -o /out-albion-market-api ./cmd/api; \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -buildvcs=false -ldflags='-s -w' \
      -o /out-healthcheck ./cmd/healthcheck; \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -buildvcs=false -ldflags='-s -w' \
      -o /out-migrate ./cmd/migrate; \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -buildvcs=false -ldflags='-s -w' \
      -o /out-account-admin ./cmd/account-admin

FROM scratch AS runtime

ARG VERSION=dev
ARG REVISION=unknown
ARG CREATED=1970-01-01T00:00:00Z

LABEL org.opencontainers.image.title="Albion Market API" \
      org.opencontainers.image.description="API centralizada de precios e historial de Albion Online" \
      org.opencontainers.image.source="https://github.com/nachodev-ui/albion-market-api" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.created="${CREATED}"

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /usr/share/zoneinfo/UTC /usr/share/zoneinfo/UTC
COPY --from=builder /out-albion-market-api /usr/local/bin/albion-market-api
COPY --from=builder /out-healthcheck /usr/local/bin/healthcheck
COPY --from=builder /out-migrate /usr/local/bin/migrate
COPY --from=builder /out-account-admin /usr/local/bin/account-admin
COPY --from=builder /out-build-metadata.json /usr/local/share/albion-market-api/build-metadata.json
COPY migrations /migrations

ENV APP_ENV=production \
    HTTP_ADDR=:8080 \
    LOAD_DOTENV=false \
    LOG_COLOR=never \
    LOG_FORMAT=json \
    TZ=UTC \
    HEALTHCHECK_URL=http://127.0.0.1:8080/healthz

USER 65532:65532

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/healthcheck"]

CMD ["/usr/local/bin/albion-market-api"]
