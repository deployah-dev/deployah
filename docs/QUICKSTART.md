# Deployah Quick Start Guide

Get up and running with Deployah in minutes! This guide will walk you through deploying your first application.

## Prerequisites

Before you begin, make sure you have:

- **Kubernetes Cluster**: A running Kubernetes cluster (local or remote)
- **kubectl**: Configured to communicate with your cluster
- **Helm**: Version 3.x installed and configured
- **Go**: Version 1.24.0 or later (for building from source)

## Installation

### Option 1: Build from Source

```bash
# Clone the repository
git clone https://github.com/deployah-dev/deployah.git
cd deployah

# Build the binary
go build -o deployah ./cmd/deployah

# Move to your PATH (optional)
sudo mv deployah /usr/local/bin/
```

### Option 2: Download Binary (Coming Soon)

```bash
# Download the latest release
curl -L https://github.com/deployah-dev/deployah/releases/latest/download/deployah-linux-amd64 -o deployah
chmod +x deployah
sudo mv deployah /usr/local/bin/
```

## Your First Deployment

### Step 1: Create a Simple Application

Let's deploy a simple web application. Create a new directory for your project:

```bash
mkdir my-first-app
cd my-first-app
```

### Step 2: Create the Deployah Manifest

Create a `.deployah.yaml` file:

```yaml
# .deployah.yaml
components:
  web-app:
    image: nginx:latest
    ports:
      - 80
    replicas: 2
    resources:
      requests:
        memory: "64Mi"
        cpu: "250m"
      limits:
        memory: "128Mi"
        cpu: "500m"
```

### Step 3: Deploy Your Application

```bash
# Deploy to your cluster
deployah --env default
```

You should see output similar to:

```
🚀 Deployah - Simplified Kubernetes Deployments

📄 Parsing manifest...
✅ Validating configuration...
🌍 Resolving environment...
🔄 Substituting variables...
⚙️ Applying defaults...
📊 Generating Helm values...
🚀 Deploying to Kubernetes...

✅ Deployment successful!
   Application: web-app
   Namespace: default
   Status: Running
```

### Step 4: Verify Your Deployment

```bash
# Check the deployment
kubectl get deployments
kubectl get pods
kubectl get services

# Access your application (if using port-forward)
kubectl port-forward service/web-app 8080:80
```

Visit `http://localhost:8080` to see your application running!

## Environment Management

### Multiple Environments

Create environment-specific configurations:

```yaml
# .deployah.yaml
environments:
  - name: development
    variables:
      IMAGE_TAG: latest
      REPLICAS: 1
  - name: production
    variables:
      IMAGE_TAG: v1.0.0
      REPLICAS: 3

components:
  web-app:
    image: my-app:${IMAGE_TAG}
    replicas: ${REPLICAS}
    ports:
      - 80
```

### Environment-Specific Files

Create `.env` files for each environment:

```bash
# .env.development
DPY_VAR_DATABASE_URL=postgres://dev-db:5432
DPY_VAR_API_KEY=dev-key

# .env.production
DPY_VAR_DATABASE_URL=postgres://prod-db:5432
DPY_VAR_API_KEY=prod-key
```

### Deploy to Specific Environment

```bash
# Deploy to development
deployah --env development

# Deploy to production
deployah --env production
```

## Advanced Configuration

### Custom Resources

```yaml
components:
  web-app:
    image: my-app:latest
    ports:
      - 80
    resources:
      requests:
        memory: "128Mi"
        cpu: "250m"
      limits:
        memory: "256Mi"
        cpu: "500m"
    env:
      - name: DATABASE_URL
        value: ${DATABASE_URL}
      - name: API_KEY
        valueFrom:
          secretKeyRef:
            name: app-secrets
            key: api-key
    volumes:
      - name: config
        configMap:
          name: app-config
    volumeMounts:
      - name: config
        mountPath: /app/config
```

### Health Checks

```yaml
components:
  web-app:
    image: my-app:latest
    ports:
      - 80
    livenessProbe:
      httpGet:
        path: /health
        port: 80
      initialDelaySeconds: 30
      periodSeconds: 10
    readinessProbe:
      httpGet:
        path: /ready
        port: 80
      initialDelaySeconds: 5
      periodSeconds: 5
```

## Common Commands

```bash
# Deploy an application
deployah --env production

# Deploy with custom manifest
deployah --manifest custom.yaml --env staging

# Check deployment status
deployah status

# List all deployments
deployah list

# Delete a deployment
deployah delete web-app

# Get help
deployah --help
```

## Troubleshooting

### Common Issues

1. **"No environments defined"**
   - Make sure you have defined environments in your `.deployah.yaml`
   - Or use `--env default` for a single environment

2. **"Invalid manifest"**
   - Check your YAML syntax
   - Validate against the JSON schema
   - Use `deployah validate` to check your manifest

3. **"Deployment failed"**
   - Check your Kubernetes cluster is running
   - Verify your `kubectl` configuration
   - Check the deployment logs: `kubectl logs deployment/web-app`

### Getting Help

- **Documentation**: Check the [main README](../README.md)
- **Issues**: Report bugs on [GitHub Issues](https://github.com/deployah-dev/deployah/issues)
- **Discussions**: Ask questions in [GitHub Discussions](https://github.com/deployah-dev/deployah/discussions)
- **Security**: Report security issues to security@deployah.dev

## Next Steps

Now that you've deployed your first application:

1. **Explore Advanced Features**: Check out the [full documentation](../README.md)
2. **Join the Community**: Contribute to the project or ask questions
3. **Share Your Experience**: Let us know how Deployah works for you
4. **Stay Updated**: Watch the repository for new features and releases

## Examples

Check out the [examples directory](../examples/) for more complex deployment scenarios:

- [Multi-service application](../examples/multi-service/)
- [Database with persistent storage](../examples/database/)
- [Microservices architecture](../examples/microservices/)
- [Production-ready configuration](../examples/production/)

Happy deploying! 🚀