# Stage 1: Build the Application
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy dependency files first (for better caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy the source code
COPY . .

# Build the binary named "relay"
RUN go build -o relay cmd/main.go

# Stage 2: Run the Application (Tiny Image)
FROM alpine:latest

WORKDIR /root/

# Copy the binary from the builder stage
COPY --from=builder /app/relay .

# Copy the config folder so the app can read settings
COPY --from=builder /app/configs ./configs

# Expose the port
EXPOSE 8080

# Run the binary
CMD ["./relay"]