# Stage 1: Build web UI
FROM node:22-alpine AS web-builder

WORKDIR /app/web

# Copy web package files
COPY web/package*.json ./

# Install dependencies
RUN npm ci

# Copy web source
COPY web/ ./

# Build web UI
RUN npm run build

# Stage 2: Build Go binary
FROM golang:1.25.5-alpine AS go-builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY cmd/ ./cmd/
COPY internal/ ./internal/

# Copy built web UI from previous stage
COPY --from=web-builder /app/web/dist ./web/dist

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o kouke-bot ./cmd/character

# Stage 3: Runtime image
FROM alpine:latest

WORKDIR /app

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata nodejs npm && \
    npm install -g @google/gemini-cli && \
    npm cache clean --force

# Create necessary directories
RUN mkdir -p /app/configs /app/data /app/web/dist

# Copy the binary from builder
COPY --from=go-builder /app/kouke-bot /app/kouke-bot

# Copy web UI from builder
COPY --from=web-builder /app/web/dist /app/web/dist

# Copy configuration files (these will be overridden by volumes in production)
COPY configs/ /app/configs/

# Embed image version for init container sync gating
ARG IMAGE_VERSION=dev
RUN echo "${IMAGE_VERSION}" > /app/.image-version

# Create a non-root user
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

# Change ownership
RUN chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Expose port (adjust if needed)
EXPOSE 8080

# Set environment variables
ENV GIN_MODE=release

# Run the application
CMD ["/app/kouke-bot"]
