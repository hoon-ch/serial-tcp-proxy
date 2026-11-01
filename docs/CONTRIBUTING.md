# Contributing Guide

Thank you for your interest in contributing to Serial TCP Proxy!

## Development Setup

### Prerequisites

- Rust toolchain with Cargo and Clippy
- Bun for contract tests
- Docker for image builds
- Git

### Clone and Build

```bash
git clone https://github.com/hoon-ch/serial-tcp-proxy.git
cd serial-tcp-proxy
cargo build --release
```

### Running Locally

```bash
export UPSTREAM_HOST=192.168.50.143
export UPSTREAM_PORT=8899
export LISTEN_PORT=18899
export WEB_PORT=18080

./target/release/serial-tcp-proxy
```

## Testing

Run the unit tests and contract tests:

```bash
cargo test
cargo build --release
bun tests/contract/contract.mjs --bin target/release/serial-tcp-proxy
```

Build the Docker image:

```bash
docker build -t serial-tcp-proxy .
```

## Project Structure

```
serial-tcp-proxy/
├── src/                     # Rust server and unit tests
│   ├── proxy/               # Proxy and upstream connections
│   └── web/                 # Web server
├── web/static/              # Embedded web assets
├── tests/contract/          # Contract tests
├── docs/                    # Documentation
├── config.yaml              # Home Assistant add-on configuration
└── .github/workflows/       # CI/CD pipelines
```

## Code Style

- Run `cargo fmt --check` before committing
- Run `cargo clippy --all-targets -- -D warnings` before committing
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

## Release Process

This project uses automated releases triggered by GitHub Releases.

### How to Create a Release

1. **Go to GitHub Releases**
   - Navigate to the repository on GitHub
   - Click "Releases" in the right sidebar
   - Click "Draft a new release"

2. **Create a New Tag**
   - In the "Choose a tag" dropdown, type your version (e.g., `v1.3.0`)
   - Select "Create new tag: v1.3.0 on publish"
   - Target branch: `main`

3. **Fill Release Information**
   - **Title**: `v1.3.0 - Brief Description`
   - **Description**: Summarize key changes (auto-generated release notes available)

4. **Publish**
   - Click "Publish release"
   - The Release workflow will automatically:
     - Update version in `config.yaml`, `README.md`, `docs/API.md`, `docs/README_ko.md`
     - Update `CHANGELOG.md` (converts `[Unreleased]` to version with date)
     - Commit version changes to `main`
     - Build binaries for all platforms
     - Build and push Docker images
     - Attach binaries and checksums to the release

### Version Numbering

We follow [Semantic Versioning](https://semver.org/):

```
MAJOR.MINOR.PATCH

- MAJOR: Breaking changes
- MINOR: New features (backward compatible)
- PATCH: Bug fixes (backward compatible)
```

### Pre-Release Checklist

Before creating a release:

- [ ] All CI checks pass on `main`
- [ ] `CHANGELOG.md` has `[Unreleased]` section with all changes
- [ ] Documentation is up to date
- [ ] No known critical bugs

### Updating CHANGELOG

When making changes, add an `[Unreleased]` section at the top of `CHANGELOG.md`:

```markdown
## [Unreleased]

### Added
- New feature description

### Fixed
- Bug fix description

### Changed
- Change description

## [1.2.0] - 2025-11-30
...
```

**Important**: Only add `[Unreleased]` when you have actual changes to document. The release workflow will convert it to the version number with the release date. If there's no `[Unreleased]` section, the workflow will skip CHANGELOG updates.

### Workflow Behavior

| Trigger | CI Runs? | Release Runs? |
|---------|----------|---------------|
| PR to main | Yes | No |
| Merge to main | Yes | No |
| Version file changes only | No | No |
| GitHub Release published | No | Yes |

---

## Reporting Issues

When reporting issues, please include:

- Rust version (`rustc --version`)
- OS and architecture
- Steps to reproduce
- Expected vs actual behavior
- Relevant logs

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
