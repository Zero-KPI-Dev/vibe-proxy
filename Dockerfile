FROM golang:1.26.5 AS build

WORKDIR /src
ARG GOPROXY=https://proxy.golang.org,direct
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    GOPROXY="${GOPROXY}" go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build \
    -tags="netgo osusergo" \
    -trimpath \
    -ldflags='-s -w' \
    -o /out/vibe-proxy ./cmd/vibe-proxy && \
    mkdir -m 1777 /out/tmp

FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /out/tmp /tmp
COPY --from=build /out/vibe-proxy /usr/local/bin/vibe-proxy

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/vibe-proxy"]
