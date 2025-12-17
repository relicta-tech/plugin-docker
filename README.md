# Docker Plugin for Relicta

Official Docker plugin for [Relicta](https://github.com/relicta-tech/relicta) - Build and push Docker images to container registries.

## Installation

```bash
relicta plugin install docker
relicta plugin enable docker
```

## Configuration

Add to your `release.config.yaml`:

```yaml
plugins:
  - name: docker
    enabled: true
    config:
      image: "your-org/your-image"
      tags:
        - "{{version}}"
        - "{{major}}.{{minor}}"
        - "latest"
      platforms:
        - "linux/amd64"
        - "linux/arm64"
```

## Configuration Options

| Option | Type | Required | Description |
|--------|------|----------|-------------|
| `image` | string | Yes | Image name (e.g., `user/image`) |
| `registry` | string | No | Container registry URL (default: `docker.io`) |
| `tags` | array | No | Tags to apply. Supports `{{version}}`, `{{major}}`, `{{minor}}`, `{{patch}}` |
| `dockerfile` | string | No | Dockerfile path (default: `Dockerfile`) |
| `context` | string | No | Build context (default: `.`) |
| `build_args` | object | No | Build arguments |
| `platforms` | array | No | Target platforms for multi-arch builds |
| `username` | string | No | Registry username (or use `DOCKER_USERNAME` env) |
| `password` | string | No | Registry password (or use `DOCKER_PASSWORD` env) |
| `push` | boolean | No | Push after building (default: `true`) |
| `labels` | object | No | Image labels |
| `cache_from` | array | No | Cache source images |
| `no_cache` | boolean | No | Disable build cache |
| `target` | string | No | Target build stage |

## Environment Variables

- `DOCKER_USERNAME` - Registry username
- `DOCKER_PASSWORD` - Registry password/token

## Hooks

- `post_publish` - Builds and pushes Docker image after release is published

## License

MIT License - see [LICENSE](LICENSE) for details.
