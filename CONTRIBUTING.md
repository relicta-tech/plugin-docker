# Contributing to Relicta Plugin

Thank you for your interest in contributing to this Relicta plugin! This document provides guidelines and instructions for contributing.

## Getting Started

1. **Fork the repository** and clone it locally
2. **Install Go 1.22+** if you haven't already
3. **Install dependencies**: `go mod download`
4. **Run tests**: `go test -v ./...`

## Development Workflow

### Making Changes

1. Create a new branch for your changes:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. Make your changes, following the code style guidelines below

3. Write or update tests as needed

4. Run the test suite to ensure everything passes:
   ```bash
   go test -v ./...
   ```

5. Run the linter:
   ```bash
   golangci-lint run
   ```

### Code Style

- Follow standard Go conventions and idioms
- Use meaningful variable and function names
- Add comments for exported functions and types
- Keep functions focused and small
- Use table-driven tests where appropriate

### Commit Messages

We follow [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` - New features
- `fix:` - Bug fixes
- `docs:` - Documentation changes
- `test:` - Adding or updating tests
- `refactor:` - Code refactoring
- `chore:` - Maintenance tasks
- `ci:` - CI/CD changes

Examples:
```
feat: add support for custom registry authentication
fix: handle empty configuration gracefully
docs: update README with new configuration options
test: add tests for validation logic
```

### Pull Requests

1. Update the CHANGELOG.md with your changes under `[Unreleased]`
2. Ensure all tests pass
3. Ensure the linter passes
4. Update documentation if needed
5. Submit a pull request with a clear description

## Testing

### Running Tests

```bash
# Run all tests
go test -v ./...

# Run tests with coverage
go test -v -cover ./...

# Run tests with race detection
go test -v -race ./...
```

### Writing Tests

- Use table-driven tests for multiple test cases
- Test both success and error paths
- Use meaningful test names that describe the scenario
- Mock external dependencies appropriately

## Plugin Architecture

This plugin follows the Relicta Plugin SDK architecture:

```
plugin.go    - Main plugin implementation
main.go      - Plugin entry point (calls plugin.Serve)
*_test.go    - Unit tests
```

Key interfaces to implement:

- `GetInfo()` - Returns plugin metadata
- `Execute()` - Runs the plugin for a given hook
- `Validate()` - Validates plugin configuration

## Questions?

- Open an issue for bugs or feature requests
- Check existing issues before creating new ones
- Join discussions in the main [Relicta repository](https://github.com/relicta-tech/relicta)

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
