# ── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.24-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /gigguide-mcp .

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM scratch

# HTTPS requests need CA certificates
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder /gigguide-mcp /gigguide-mcp

LABEL io.modelcontextprotocol.server.name="io.github.cuotos/gigguide-mcp"

ENTRYPOINT ["/gigguide-mcp"]
