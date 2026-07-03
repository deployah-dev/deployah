## deployah logs

View logs for a deployed project

### Synopsis

View logs from pods associated with a deployed project. This command connects to Kubernetes to stream logs from the pods.

```text
deployah logs <project> [flags]
```

### Options

```text
      --component string       Filter by component name (e.g., api, web, worker)
      --container string       Container name (if pod has multiple containers)
  -e, --environment string     Filter by environment name (e.g., dev, staging, prod)
      --no-follow              Do not follow log output
      --only-log-lines         Only output the log message lines (suppresses headers)
      --resource string        Kubernetes resource to tail (e.g., deployment/myapp)
      --since duration         Show logs since duration (e.g., 10s, 1m, 1h) (default 48h0m0s)
      --tail int               Number of lines to show from the end of the logs (-1 shows all) (default -1)
      --template string        Go template for each log line
      --template-file string   Path to a file containing a Go template for each log line
      --timestamps             Include timestamps in log output
      --timezone string        Timezone for timestamps (e.g., Europe/Amsterdam) (default "Local")
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

* [deployah](deployah.md)  - Deployah turns a spec into a running release on Kubernetes (Spec-to-Release)
