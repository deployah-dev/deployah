## deployah cluster kubeconfig

Print the local cluster kubeconfig path or contents

### Synopsis

Print the path to the deployah-managed kubeconfig for the local cluster. Use --raw to print the kubeconfig YAML contents instead, which is handy for piping into other tools.

```text
deployah cluster kubeconfig [flags]
```

### Options

```text
      --raw   Print the kubeconfig YAML contents instead of its path
```

### Options inherited from parent commands

```text
      --context string         Kubernetes context to use (overrides the current context and any environment 'context' field)
  -d, --debug                  Enable debug mode (verbose logging and keep temporary files)
  -h, --help                   show help for this command
  -k, --kubeconfig string      Path to the kubeconfig file to use (defaults to standard kubeconfig resolution)
  -n, --namespace string       Kubernetes namespace to use for Deployah operations (defaults to current context namespace)
      --platform-file string   Path to the platform config file (overrides DEPLOYAH_PLATFORM_FILE and the default same-directory lookup)
  -s, --spec string            Path to the Deployah spec file (YAML or JSON) (default "deployah.yaml")
  -t, --timeout duration       Timeout for Deployah operations (install/upgrade, list, status, logs, delete) (default 10m0s)
```

### SEE ALSO

* [deployah cluster](deployah_cluster.md)  - Manage a local Kubernetes cluster for development
