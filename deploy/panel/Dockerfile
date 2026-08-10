# Multi-stage: build UI, then Go panel server + olcnode agent binary
FROM node:20-alpine AS web
WORKDIR /web
COPY panel/web/package.json ./
RUN npm install
COPY panel/web/ ./
RUN npm run build

FROM golang:1.22-alpine AS server
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY panel/server/ ./panel/server/
WORKDIR /src/panel/server
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/panel ./cmd/panel

FROM golang:1.22-alpine AS olcnode
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY olcnode/ ./olcnode/
WORKDIR /src/olcnode
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/olcnode ./cmd/olcnode

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
  && adduser -D -H -u 10001 panel
WORKDIR /app
COPY --from=server /out/panel /app/panel
COPY --from=olcnode /out/olcnode /app/olcnode
COPY --from=web /web/dist /app/web/dist
RUN mkdir -p /app/data && chown -R panel:panel /app
USER panel
ENV PANEL_LISTEN=:3080 \
    PANEL_DB_PATH=/app/data/panel.db \
    PANEL_STATIC_DIR=/app/web/dist \
    OLCNODE_BIN=/app/olcnode \
    HAPANEL_INSECURE_SKIP_VERIFY=true
EXPOSE 3080
VOLUME ["/app/data"]
ENTRYPOINT ["/app/panel"]
