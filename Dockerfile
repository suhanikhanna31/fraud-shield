# --- Build stage ---
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Cache dependencies first
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source
COPY . .

# Static binary, stripped. Built from cmd/server.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o fraud-shield ./cmd/server

# --- Run stage ---
FROM alpine:3.19

WORKDIR /app

# Certs needed for outbound TLS (e.g. managed Postgres with sslmode=require)
RUN apk add --no-cache ca-certificates

COPY --from=builder /app/fraud-shield .

# OpenShift's restricted SCC runs containers under a random UID in group 0,
# so the binary must be world-executable and nothing may need to be written.
# A numeric non-root USER also keeps this safe on plain Docker / Render.
RUN chmod 0755 /app/fraud-shield
USER 1001

# Server listens on :8080 by default, override-able via PORT env var
EXPOSE 8080

CMD ["./fraud-shield"]
