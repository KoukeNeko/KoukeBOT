# Docker Deployment Guide

## Building the Image

```bash
# Build the Docker image
docker build -t kouke-bot:latest .

# Or build with docker-compose
docker-compose build
```

## Running the Container

### Using Docker Compose (Recommended)

```bash
# Start the service
docker-compose up -d

# View logs
docker-compose logs -f

# Stop the service
docker-compose down
```

### Using Docker directly

```bash
# Run the container
docker run -d \
  --name kouke-bot \
  -p 8080:8080 \
  -v $(pwd)/configs:/app/configs:ro \
  -v $(pwd)/data:/app/data \
  -e TZ=Asia/Tokyo \
  --restart unless-stopped \
  kouke-bot:latest

# View logs
docker logs -f kouke-bot

# Stop the container
docker stop kouke-bot
docker rm kouke-bot
```

## Volume Mounts

- `/app/configs` - Configuration files (persona configs, settings) - **mounted read-only**
- `/app/data` - Runtime data (conversation history, images, settings.json)

## Environment Variables

- `GIN_MODE` - Gin framework mode (default: `release`)
- `TZ` - Timezone (default: `Asia/Tokyo`)

## Port Mapping

- `8080` - Web UI and API endpoint

## Multi-Architecture Build

To build for multiple architectures (e.g., ARM64 for Raspberry Pi):

```bash
# Create a buildx builder
docker buildx create --name multiarch --use

# Build for multiple platforms
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t kouke-bot:latest \
  --push \
  .
```

## Troubleshooting

### Check container logs
```bash
docker-compose logs -f kouke-bot
```

### Enter the container shell
```bash
docker-compose exec kouke-bot sh
```

### Rebuild after code changes
```bash
docker-compose down
docker-compose build --no-cache
docker-compose up -d
```

### Check resource usage
```bash
docker stats kouke-bot
```
