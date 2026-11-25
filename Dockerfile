FROM node:24-bookworm-slim AS frontend
WORKDIR /src
COPY web/package.json web/package-lock.json ./web/
RUN npm --prefix web ci
COPY web/ ./web/
RUN npm --prefix web run build

FROM golang:1.27.4-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY --from=frontend /src/internal/web/dist/ ./internal/web/dist/
RUN CGO_ENABLED=0 go build -trimpath -o /out/static-host ./cmd/static-host

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && mkdir /data && chown 10001:10001 /data && chmod 0700 /data
COPY --from=build /out/static-host /usr/local/bin/static-host
USER 10001:10001
ENV STATIC_HOST_DATA_DIR=/data STATIC_HOST_LISTEN=0.0.0.0:8081
EXPOSE 8081
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:8081/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/static-host"]
