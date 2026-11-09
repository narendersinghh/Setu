# Setu v2 – one image: React app built and embedded in a static Go binary.
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npx vite build --outDir /out/dist --emptyOutDir

FROM golang:1.26-alpine AS server
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
COPY --from=web /out/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/setu ./cmd/setu

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -H -u 10001 setu && mkdir -p /data && chown setu /data
COPY --from=server /out/setu /usr/local/bin/setu
USER setu
ENV SETU_STORAGE=/data SETU_ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["setu"]
CMD ["all"]
