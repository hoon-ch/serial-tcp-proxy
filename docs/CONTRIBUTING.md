# Contributing Guide

Thank you for your interest in contributing to Serial TCP Proxy!

## Development Setup

### Prerequisites

- Go 1.21 or later
- Git

### Clone and Build

```bash
git clone https://github.com/hoon-ch/serial-tcp-proxy.git
cd serial-tcp-proxy
go build -o serial-tcp-proxy ./cmd/serial-tcp-proxy
```

### Running Locally

```bash
export UPSTREAM_HOST=192.168.50.143
export UPSTREAM_PORT=8899
export LISTEN_PORT=18899
export WEB_PORT=18080

./serial-tcp-proxy
```

## Testing

### Run All Tests

```bash
go test -v ./...
```

### Run Tests with Coverage

```bash
go test -cover ./...
```

### Generate Coverage Report

```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
open coverage.html  # macOS
```

### Test Coverage Goals

| Package | Target | Current |
|---------|--------|---------|
| internal/web | 80% | 92.4% |
| internal/client | 80% | 93.9% |
| internal/upstream | 80% | 86.0% |
| internal/logger | 80% | 83.3% |
| internal/config | 70% | 72.7% |
| internal/proxy | 70% | 70.8% |

## Project Structure

```
serial-tcp-proxy/
├── cmd/
│   └── serial-tcp-proxy/    # Main entry point
├── internal/
│   ├── client/              # Client connection management
│   ├── config/              # Configuration handling
│   ├── logger/              # Logging utilities
│   ├── proxy/               # Core proxy logic
│   ├── upstream/            # Upstream connection management
│   └── web/                 # Web UI server
│       └── static/          # Static web assets
├── docs/                    # Documentation
├── addons/                  # Home Assistant Add-on config
└── .github/workflows/       # CI/CD pipelines
```

## Code Style

- Follow standard Go conventions
- Use `gofmt` for formatting
- Run `go vet` before committing
- Keep functions focused and small
- Write descriptive commit messages

## Commit Message Convention

We follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>: <description>

[optional body]

[optional footer]
```

### Types

| Type | Description |
|------|-------------|
| `feat` | New feature |
| `fix` | Bug fix |
| `docs` | Documentation only |
| `test` | Adding or updating tests |
| `refactor` | Code refactoring |
| `chore` | Maintenance tasks |
| `ci` | CI/CD changes |

### Examples

```
feat: add packet injection endpoint

fix: resolve upstream reconnection timeout

test: enhance web server test coverage to 92.4%

docs: add API documentation
```

## Pull Request Process

1. Fork the repository
2. Create a feature branch (`git checkout -b feat/my-feature`)
3. Make your changes
4. Write/update tests
5. Ensure all tests pass
6. Commit with conventional commit message
7. Push to your fork
8. Create a Pull Request

### PR Requirements

- [ ] Tests pass
- [ ] Coverage maintained or improved
- [ ] Documentation updated if needed
- [ ] Commit messages follow convention

## Reporting Issues

When reporting issues, please include:

- Go version (`go version`)
- OS and architecture
- Steps to reproduce
- Expected vs actual behavior
- Relevant logs

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
