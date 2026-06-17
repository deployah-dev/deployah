## deployah init

Create a new Deployah spec for a project

### Synopsis

Create a new Deployah spec for a project

```text
deployah init [flags]
```

### Options

```text
      --dry-run         Preview the generated spec without saving it
  -o, --output string   The output file path. (default "deployah.yaml")
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
