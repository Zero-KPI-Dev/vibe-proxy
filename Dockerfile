FROM golang:1.22 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 go build \
    -tags="netgo osusergo" \
    -trimpath \
    -ldflags='-s -w -linkmode external -extldflags "-static"' \
    -o /out/vibe-proxy ./cmd/vibe-proxy && \
    mkdir -m 1777 /out/tmp

FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /out/tmp /tmp
COPY --from=build /out/vibe-proxy /usr/local/bin/vibe-proxy

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/vibe-proxy"]
