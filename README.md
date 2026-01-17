# 🚀 Relay - Intelligent AI API Gateway

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Docker Pulls](https://img.shields.io/docker/pulls/yourusername/relay)](https://hub.docker.com/r/yourusername/relay)
[![GitHub Stars](https://img.shields.io/github/stars/yourusername/relay)](https://github.com/yourusername/relay)

> A high-performance reverse proxy and API gateway for AI services (OpenAI, Anthropic, etc.) with built-in caching, rate limiting, cost tracking, and observability.

## ✨ Features

- ⚡ **Smart Caching** - Redis-backed response caching to reduce costs
- 🛡️ **Rate Limiting** - Distributed rate limiting with Redis (or in-memory fallback)
- 💰 **Cost Tracking** - Real-time token usage and cost estimation
- 🔄 **Circuit Breaker** - Automatic failure detection and recovery
- 📊 **Prometheus Metrics** - Built-in observability with `/metrics` endpoint
- 🔥 **Hot Reload** - Configuration updates without restarts
- 🐳 **Docker Ready** - Multi-stage builds for minimal image size
- 🔌 **Zero Dependencies** - Works standalone or with Redis for advanced features

## 🎯 Use Cases

- **Cost Optimization**: Cache repeated queries to reduce AI API costs by up to 80%
- **Rate Limit Management**: Prevent overages with smart request throttling
- **Multi-Model Support**: Route requests to different AI providers
- **Observability**: Track usage, costs, and performance in real-time
- **Team Collaboration**: Centralized AI gateway for multiple applications

## 🚀 Quick Start

### Option 1: Docker (Recommended)

```bash
# Clone the repository
git clone https://github.com/yourusername/relay.git
cd relay

# Copy and edit configuration
cp configs/config.example.yaml configs/config.yaml
nano configs/config.yaml

# Start with Docker Compose (includes Redis)
docker-compose up -d

# Your relay is now running on http://localhost:8080
```

### Option 2: Binary

```bash
# Download latest release
curl -sSL https://github.com/yourusername/relay/releases/latest/download/relay-linux-amd64 -o relay
chmod +x relay

# Create config
curl -sSL https://raw.githubusercontent.com/yourusername/relay/main/configs/config.example.yaml -o config.yaml

# Run
./relay
```

### Option 3: From Source

```bash
git clone https://github.com/yourusername/relay.git
cd relay
cp configs/config.example.yaml configs/config.yaml
go run cmd/main.go
```

## 📖 Usage

### Basic Proxying

```bash
# Replace OpenAI API calls with your relay endpoint
curl http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_OPENAI_KEY" \
  -d '{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

### Using with OpenAI Python SDK

```python
import openai

# Point to your relay instead of OpenAI directly
openai.api_base = "http://localhost:8080/v1"
openai.api_key = "YOUR_OPENAI_KEY"

response = openai.ChatCompletion.create(
    model="gpt-4",
    messages=[{"role": "user", "content": "Hello!"}]
)
```

### Monitoring

```bash
# View Prometheus metrics
curl http://localhost:8080/metrics

# Key metrics:
# - relay_cache_hits_total
# - relay_cache_misses_total
# - relay_request_tokens (histogram)
# - relay_upstream_latency_seconds (histogram)
```

## ⚙️ Configuration

Edit `configs/config.yaml`:

```yaml
server:
  port: ":8080"

proxy:
  target: "https://api.openai.com"  # Target API endpoint

ratelimit:
  enabled: true
  requests_per_second: 10.0         # Adjust based on your needs
  burst: 20                          # Allow bursts

redis:
  enabled: true                      # Disable for in-memory mode
  address: "localhost:6379"
  password: ""
  db: 0

# Pricing in USD per 1K tokens (for cost tracking)
models:
  gpt-4: 0.03
  gpt-4-32k: 0.06
  gpt-3.5-turbo: 0.002
  claude-3-opus: 0.015
  claude-3-sonnet: 0.003
```

**Hot Reload**: Changes to `config.yaml` are automatically detected and applied without restart!

## 🏗️ Architecture

```
┌─────────┐      ┌─────────────────────────────┐      ┌──────────┐
│ Client  │─────▶│         Relay               │─────▶│ OpenAI   │
└─────────┘      │                             │      │ API      │
                 │  ┌───────────────────────┐  │      └──────────┘
                 │  │ Request Logger        │  │
                 │  ├───────────────────────┤  │
                 │  │ Token Cost Tracker    │  │
                 │  ├───────────────────────┤  │      ┌──────────┐
                 │  │ Redis Cache           │◀─┼─────▶│  Redis   │
                 │  ├───────────────────────┤  │      └──────────┘
                 │  │ Rate Limiter          │  │
                 │  ├───────────────────────┤  │
                 │  │ Circuit Breaker       │  │
                 │  └───────────────────────┘  │
                 └─────────────────────────────┘
                              │
                              ▼
                      ┌──────────────┐
                      │ Prometheus   │
                      │ Metrics      │
                      └──────────────┘
```

## 🔧 Advanced Features

### Rate Limiting Strategies

```yaml
# Per-second limits (smooth traffic)
ratelimit:
  requests_per_second: 10.0
  burst: 20

# Low-frequency limits (e.g., 1 request per 5 seconds)
ratelimit:
  requests_per_second: 0.2  # 1/5 = 0.2
  burst: 1
```

### Distributed vs In-Memory Mode

| Feature | With Redis | Without Redis |
|---------|------------|---------------|
| Caching | ✅ Persistent | ❌ N/A |
| Rate Limiting | ✅ Distributed (multi-instance) | ⚠️ Per-instance only |
| Scalability | ✅ Horizontal | ⚠️ Limited |

### Environment Variables

Override config with environment variables:

```bash
export SERVER_PORT=":9090"
export REDIS_ADDRESS="redis.prod.example.com:6379"
export REDIS_PASSWORD="secret"
./relay
```

## 📊 Monitoring & Observability

### Prometheus Integration

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'relay'
    static_configs:
      - targets: ['localhost:8080']
```

### Grafana Dashboard

Import the included dashboard: `deploy/grafana/relay-dashboard.json`

**Key Metrics:**
- Cache hit rate
- Request latency (p50, p95, p99)
- Token usage by model
- Estimated costs
- Rate limit violations
- Circuit breaker state

## 🚢 Production Deployment

### Docker Swarm

```bash
docker stack deploy -c docker-compose.yml relay-stack
```

### Kubernetes

```bash
kubectl apply -f deploy/kubernetes/
```

### Helm

```bash
helm repo add relay https://yourusername.github.io/relay-helm
helm install my-relay relay/relay
```

## 🛠️ Development

```bash
# Install dependencies
go mod download

# Run tests
go test ./...

# Run with live reload (install air: go install github.com/cosmtrek/air@latest)
air

# Build
go build -o relay cmd/main.go
```

## 📚 Documentation

- [Configuration Guide](docs/configuration.md)
- [Deployment Guide](docs/deployment.md)
- [API Reference](docs/api.md)
- [Troubleshooting](docs/troubleshooting.md)

## 🤝 Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- Built with [Go](https://golang.org/)
- Uses [Redis](https://redis.io/) for distributed caching
- Metrics powered by [Prometheus](https://prometheus.io/)
- Token counting via [tiktoken-go](https://github.com/pkoukk/tiktoken-go)
