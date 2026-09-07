FROM --platform=$BUILDPLATFORM node:24-bookworm-slim AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build
RUN mkdir -p /licenses/npm && find /src/web/node_modules -type f -iname 'license*' -exec cp --parents {} /licenses/npm/ \;

FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS backend
WORKDIR /src
COPY go.mod go.sum ./
ARG GOPROXY=https://proxy.golang.org,direct
RUN GOPROXY=$GOPROXY go mod download
COPY cmd/ cmd/
COPY internal/ internal/
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/mytiktok ./cmd/mytiktok
RUN mkdir -p /licenses/go && find /go/pkg/mod -type f -iname 'license*' -exec cp --parents {} /licenses/go/ \;

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg ca-certificates tzdata && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=backend /out/mytiktok /app/mytiktok
COPY --from=web /src/web/dist /app/web
COPY --from=web /licenses/npm /usr/share/doc/mytiktok/npm
COPY --from=backend /licenses/go /usr/share/doc/mytiktok/go
COPY THIRD_PARTY_NOTICES.md /usr/share/doc/mytiktok/THIRD_PARTY_NOTICES.md
ENV DATA_DIR=/data WEB_DIR=/app/web MEDIA_ROOT=/media LISTEN_ADDR=:8080 TZ=Asia/Hong_Kong
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/app/mytiktok"]
