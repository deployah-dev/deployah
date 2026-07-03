## deployah cluster up

Create the local cluster and start the cloud provider

### Synopsis

Create the local cluster if it does not exist, write its kubeconfig, and start the cloud-provider-kind container so that LoadBalancer Services and Ingress work. By default this command returns immediately after starting the cloud provider in the background.

Use --attach to stay in the foreground and stream the cloud provider logs; the container is stopped when you press Ctrl-C.

Use --no-cloud-provider to only create the cluster without starting the cloud provider.

```text
deployah cluster up [flags]
```

### Options

```text
      --attach                      Stay in the foreground and stream cloud provider logs (Ctrl-C stops the container)
      --kubernetes-version string   Kubernetes version for the cluster (e.g. 1.31 or v1.31.2)
      --no-cloud-provider           Only create the cluster; do not start the cloud provider
      --runtime string              Host container engine to use (default "auto")
      --sync-registry-auth          Copy host registry credentials into the cluster as a Kubernetes Secret and patch the default ServiceAccount to use them
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
