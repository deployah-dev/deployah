## deployah plan

Inspect changes for an environment

### Synopsis

Render the chart for an environment and compare it with the previous release baseline selected by Helm. For an existing release, also compare that baseline with live cluster state. Plan is read-only and never applies anything.

```text
deployah plan <environment> [flags]
```

### Options

```text
      --detailed-exitcode   Exit 2 when the plan has effects (resource changes, tasks that change or run, chart CRDs Helm will process), 0 when it has none, 1 on error; drift alone exits 0
  -o, --output string       Output format: human or json (default "human")
      --show-secrets        Reveal Kubernetes Secret data and stringData values in the selected output format (human or json); values are redacted by default
```

### Options inherited from parent commands

```text
      --context string         Kubernetes context to use (overrides the current context and any environment 'context' field)
  -C, --cwd string             Run as if deployah was started in this directory instead of the current working directory
  -d, --debug                  Enable debug mode (verbose logging and keep temporary files)
  -h, --help                   show help for this command
  -k, --kubeconfig string      Path to the kubeconfig file to use (defaults to standard kubeconfig resolution)
  -n, --namespace string       Kubernetes namespace to use for Deployah operations (defaults to current context namespace)
      --platform-file string   Path to the platform config file (overrides DEPLOYAH_PLATFORM_FILE and the default same-directory lookup)
  -s, --spec string            Path to the Deployah spec file (YAML or JSON) (default "deployah.yaml")
  -t, --timeout duration       Timeout for Deployah operations (install/upgrade, list, status, logs, delete, run) (default 10m0s)
```

### SEE ALSO

* [deployah](deployah.md)  - Deployah turns a spec into a running release on Kubernetes (Spec-to-Release)
