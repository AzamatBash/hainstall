
FROM golang:1.22-alpine AS build
WORKDIR /src

RUN apk add --no-cache git ca-certificates

COPY agent/go.mod agent/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY agent/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
      -ldflags "-s -w -X github.com/azabash/hapanel/agent/internal/api.Version=0.1.0" \
      -o /out/hapanel-agent ./cmd/agent

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/hapanel-agent /usr/local/bin/hapanel-agent
EXPOSE 9100
ENTRYPOINT ["/usr/local/bin/hapanel-agent"]
