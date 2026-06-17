## deployah shell

Connect to a shell in a container

### Synopsis

Connect to an interactive shell in a container of a deployed project.

```text
deployah shell <project> [flags]
```

### Options

```text
      --command string       Command to execute (default: shell)
      --component string     Component name (e.g., api, web, worker)
      --container string     Container name (if pod has multiple containers)
  -e, --environment string   Filter by environment name (e.g., dev, staging, prod)
      --shell string         Preferred shell (bash, zsh, sh, ash, dash, fish)
      --workdir string       Working directory in container
```

### Options inherited from parent commands

```text
      --context string      Kubernetes context to use (overrides the current context and any environment 'context' field)
  -d, --debug               Enable debug mode (verbose logging and keep temporary files)
  -h, --help                show help for this command
  -k, --kubeconfig string   Path to the kubeconfig file to use (defaults to standard kubeconfig resolution)
  -n, --namespace string    Kubernetes namespace to use for Deployah operations (defaults to current context namespace)
  -s, --spec string         Path to the Deployah spec file (YAML or JSON) (default "deployah.yaml")
  -t, --timeout duration    Timeout for Deployah operations (install/upgrade, list, status, logs, delete) (default 10m0s)
```

### SEE ALSO

* [deployah](deployah.md)  - Deployah turns a spec into a running release on Kubernetes (Spec-to-Release)
