## deployah cluster

Manage a local Kubernetes cluster for development

### Synopsis

Manage a single local Kubernetes cluster (backed by Kind) for development.
The cluster lifecycle is independent of deployah environments. Bring the cluster up, then point a deploy at it with the global --context flag or an environment's "context" field (the cluster's context is "kind-deployah").

```text
deployah cluster [flags]
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
* [deployah cluster down](deployah_cluster_down.md)  - Delete the local cluster and stop the cloud provider
* [deployah cluster kubeconfig](deployah_cluster_kubeconfig.md)  - Print the local cluster kubeconfig path or contents
* [deployah cluster status](deployah_cluster_status.md)  - Show the local cluster status and access info
* [deployah cluster up](deployah_cluster_up.md)  - Create the local cluster and start the cloud provider
