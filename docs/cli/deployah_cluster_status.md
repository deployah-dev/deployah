## deployah cluster status

Show the local cluster status and access info

### Synopsis

Show the local cluster's health, metadata, whether the cloud provider is running, and how to reach LoadBalancer Services and Ingresses (including suggested /etc/hosts entries).

```text
deployah cluster status [flags]
```

### Options

```text
  -o, --output string   Output format (default "table")
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
