# Contributing to Deployah

Thank you for your interest in contributing to Deployah! This document provides guidelines and information for contributors.

## Code of Conduct

This project follows the [CNCF Code of Conduct](https://github.com/cncf/foundation/blob/main/code-of-conduct.md). By participating, you are expected to uphold this code.

## Getting Started

### Prerequisites

- Go 1.24.0 or later
- Git
- A Kubernetes cluster (for testing)
- Helm 3.x

### Development Setup

1. **Fork and clone the repository:**
   ```bash
   git clone https://github.com/YOUR_USERNAME/deployah.git
   cd deployah
   ```

2. **Install dependencies:**
   ```bash
   go mod download
   ```

3. **Build the project:**
   ```bash
   go build ./cmd/deployah
   ```

4. **Run tests:**
   ```bash
   go test ./...
   ```

## Development Workflow

### Branch Strategy

- `main` - Production-ready code
- `develop` - Integration branch for features
- `feature/*` - Feature branches
- `bugfix/*` - Bug fix branches
- `release/*` - Release preparation branches

### Commit Message Format

We follow the [Conventional Commits](https://www.conventionalcommits.org/) specification:

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code style changes (formatting, etc.)
- `refactor`: Code refactoring
- `test`: Adding or updating tests
- `chore`: Maintenance tasks

### Pull Request Process

1. **Create a feature branch** from `develop`
2. **Make your changes** following the coding standards
3. **Add tests** for new functionality
4. **Update documentation** as needed
5. **Ensure all tests pass**
6. **Submit a pull request** to `develop`

### Pull Request Requirements

- [ ] Code follows the project's style guidelines
- [ ] Self-review of code has been completed
- [ ] Code has been tested locally
- [ ] Tests have been added/updated
- [ ] Documentation has been updated
- [ ] Commit messages follow conventional format
- [ ] PR description clearly describes the changes

## Coding Standards

### Go Code Style

- Follow [Effective Go](https://golang.org/doc/effective_go.html)
- Use `gofmt` for formatting
- Run `golint` and `go vet` before submitting
- Aim for 80%+ test coverage

### Error Handling

- Always check and handle errors explicitly
- Use meaningful error messages
- Wrap errors with context when appropriate

### Documentation

- Document all exported functions and types
- Keep README.md up to date
- Add examples for new features

## Testing

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run tests with race detection
go test -race ./...

# Run benchmarks
go test -bench=. ./...
```

### Test Guidelines

- Write unit tests for all new functionality
- Use table-driven tests where appropriate
- Mock external dependencies
- Test both success and error cases

## Release Process

### Versioning

We follow [Semantic Versioning](https://semver.org/) (MAJOR.MINOR.PATCH).

### Release Steps

1. **Create release branch** from `develop`
2. **Update version** in relevant files
3. **Update CHANGELOG.md**
4. **Run full test suite**
5. **Create release tag**
6. **Merge to main**
7. **Create GitHub release**

## Communication

- **Issues**: Use GitHub Issues for bug reports and feature requests
- **Discussions**: Use GitHub Discussions for questions and general discussion
- **Security**: Report security issues to security@deployah.dev

## Getting Help

- Check existing issues and discussions
- Join our community channels
- Ask questions in GitHub Discussions

## License

By contributing to Deployah, you agree that your contributions will be licensed under the Apache License 2.0.