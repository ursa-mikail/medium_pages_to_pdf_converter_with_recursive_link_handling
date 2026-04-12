FROM golang:1.22-bookworm AS builder

WORKDIR /app
COPY go.mod ./

# Use direct GitHub fetching — bypasses proxy.golang.org entirely.
# GONOSUMDB skips checksum DB verification.
# GOFLAGS=-mod=mod allows go.sum to be written during build.
ENV GOPROXY=direct \
    GONOSUMDB=* \
    GOFLAGS=-mod=mod \
    CGO_ENABLED=0 \
    GOOS=linux

RUN go mod download

COPY . .
RUN go build -o harvester ./main.go

# ─── Runtime ──────────────────────────────────────────────────────────────────
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    chromium \
    ca-certificates \
    fonts-liberation \
    fonts-noto-color-emoji \
    libnss3 \
    libxss1 \
    libatk-bridge2.0-0 \
    libgtk-3-0 \
    libdrm2 \
    libgbm1 \
    libxshmfence1 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /app/harvester .

ENV CHROME_BIN=/usr/bin/chromium
ENV OUTPUT_DIR=/app/output
ENV PORT=8080

EXPOSE 8080
CMD ["./harvester"]
