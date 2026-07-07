# --- Build stage ---
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Cache dependencies first
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source
COPY . .

# Build the server binary from cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -o fraud-shield ./cmd/server

# --- Run stage ---
FROM alpine:3.19

WORKDIR /app

# Certs needed for outbound HTTPS calls (e.g. if you ever call external APIs)
RUN apk add --no-cache ca-certificates

COPY --from=builder /app/fraud-shield .

# Server listens on :8080 by default, override-able via PORT env var
EXPOSE 8080

CMD ["./fraud-shield"]
