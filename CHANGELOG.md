# Changelog

All notable changes to Deployah will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Initial project structure and documentation
- Basic CLI framework using Cobra
- Helm SDK integration
- YAML manifest parsing and validation
- Environment management system
- Variable substitution with precedence rules
- JSON Schema validation
- Multi-phase deployment pipeline

### Changed

### Deprecated

### Removed

### Fixed

### Security

## [0.1.0] - 2025-01-XX

### Added
- Initial release of Deployah
- Core CLI functionality
- Basic deployment capabilities
- Documentation and examples

---

## Release Notes

### Version 0.1.0
This is the initial release of Deployah, providing a simplified CLI for deploying applications using Helm without requiring Kubernetes or Helm expertise.

**Key Features:**
- Simple YAML-based configuration
- Environment management
- Variable substitution
- Helm integration
- Multi-phase deployment pipeline

**Breaking Changes:**
None (initial release)

**Known Issues:**
- Limited to basic deployment scenarios
- Requires manual Helm chart setup
- No advanced Kubernetes features

---

## Contributing

To add entries to this changelog:

1. Add your changes under the `[Unreleased]` section
2. Use the appropriate category (Added, Changed, Deprecated, Removed, Fixed, Security)
3. Provide a clear, concise description of the change
4. Reference any related issues or pull requests

When releasing:
1. Move `[Unreleased]` changes to the new version section
2. Update the version number and date
3. Add any release notes or breaking change information