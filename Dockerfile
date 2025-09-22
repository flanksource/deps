# Build stage
FROM golang:1.25.1-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build \
    -a -installsuffix cgo \
    -ldflags '-w -s' \
    -o deps \
    ./cmd/deps/main.go

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates wget curl

WORKDIR /root/

# Copy the binary from builder stage
COPY --from=builder /src/deps .

# Make binary executable
RUN chmod +x ./deps

# Create bin directory for installed tools
RUN mkdir -p /usr/local/bin

# Set PATH to include installed tools
ENV PATH="/usr/local/bin:${PATH}"

# Set default bin directory for deps
ENV DEPS_BIN_DIR="/usr/local/bin"

ENTRYPOINT ["./deps"]