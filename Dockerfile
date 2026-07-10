# syntax=docker/dockerfile:1.7

FROM golang:1.26.4-bookworm@sha256:b305420a68d0f229d91eb3b3ed9e519fcf2cf5461da4bef997bf927e8c0bfd2b AS builder

WORKDIR /src

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev
ARG REVISION=unknown

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    go mod download && \
    go mod verify

COPY cmd ./cmd
COPY internal ./internal

RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -buildvcs=false \
      -ldflags="-s -w -X main.version=${VERSION} -X main.revision=${REVISION}" \
      -o /out/albion-market-api ./cmd/api && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -buildvcs=false -ldflags='-s -w' \
      -o /out/healthcheck ./cmd/healthcheck && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -buildvcs=false -ldflags='-s -w' \
      -o /out/migrate ./cmd/migrate

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
COPY --from=builder /out/albion-market-api /usr/local/bin/albion-market-api
COPY --from=builder /out/healthcheck /usr/local/bin/healthcheck
COPY --from=builder /out/migrate /usr/local/bin/migrate
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
