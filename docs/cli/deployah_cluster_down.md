## deployah cluster down

Delete the local cluster and stop the cloud provider

### Synopsis

Stop the cloud-provider-kind container (if running) and delete the local cluster. This is destructive and removes all workloads running in the cluster.

```text
deployah cluster down [flags]
```

### Options

```text
      --force   Delete without confirmation
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

* [deployah cluster](deployah_cluster.md)  - Manage a local Kubernetes cluster for development
