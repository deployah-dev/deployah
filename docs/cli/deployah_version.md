## deployah version

Print version information

```text
deployah version [flags]
```

### Options

```text
      --format string   output format: "text", "short", or "json" (default "text")
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
