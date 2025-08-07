# Security Policy

## Supported Versions

Use this section to tell people about which versions of your project are currently being supported with security updates.

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | :white_check_mark: |
| < 0.1   | :x:                |

## Reporting a Vulnerability

We take the security of Deployah seriously. If you believe you have found a security vulnerability, please report it to us as described below.

### Reporting Process

1. **DO NOT** create a public GitHub issue for the vulnerability.
2. Email your findings to [security@deployah.dev](mailto:security@deployah.dev).
3. Provide a detailed description of the vulnerability, including:
   - Type of issue (buffer overflow, SQL injection, cross-site scripting, etc.)
   - Full paths of source file(s) related to the vulnerability
   - The location of the affected source code (tag/branch/commit or direct URL)
   - Any special configuration required to reproduce the issue
   - Step-by-step instructions to reproduce the issue
   - Proof-of-concept or exploit code (if possible)
   - Impact of the issue, including how an attacker might exploit it

### What to Expect

- You will receive an acknowledgment within 48 hours
- We will investigate and provide updates on our progress
- Once the issue is confirmed, we will work on a fix
- We will coordinate the disclosure with you
- We will credit you in the security advisory (unless you prefer to remain anonymous)

### Responsible Disclosure Timeline

- **48 hours**: Initial acknowledgment
- **7 days**: Status update
- **30 days**: Target for fix completion
- **90 days**: Maximum time before public disclosure

## Security Best Practices

### For Users

- Always use the latest stable version of Deployah
- Keep your Kubernetes cluster and Helm versions updated
- Review and validate your deployment manifests
- Use secrets management for sensitive configuration
- Enable RBAC in your Kubernetes cluster
- Regularly audit your deployments

### For Contributors

- Follow secure coding practices
- Validate all user inputs
- Use parameterized queries and prepared statements
- Implement proper error handling
- Avoid logging sensitive information
- Keep dependencies updated

## Security Features

Deployah implements several security measures:

- **Input Validation**: All user inputs are validated using JSON Schema
- **Environment Isolation**: Clear separation between different deployment environments
- **Variable Substitution**: Secure handling of configuration variables
- **Helm Integration**: Leverages Helm's security features
- **No Sensitive Data Logging**: Avoids logging sensitive configuration values

## Security Updates

Security updates are released as patch versions (0.1.x) and should be applied as soon as possible. Critical security fixes may be backported to previous minor versions.

## Security Team

The security team consists of project maintainers and security experts. Contact us at [security@deployah.dev](mailto:security@deployah.dev) for security-related questions.

## Acknowledgments

We would like to thank all security researchers who responsibly disclose vulnerabilities to us. Your contributions help make Deployah more secure for everyone.

## Related Links

- [CNCF Security Best Practices](https://github.com/cncf/tag-security)
- [Kubernetes Security](https://kubernetes.io/docs/concepts/security/)
- [Helm Security](https://helm.sh/docs/topics/security/)