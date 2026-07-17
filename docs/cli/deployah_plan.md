## deployah plan

Preview the changes a deploy would make

### Synopsis

Render the chart for an environment and show what would change compared to the last successful release, without applying anything.

```text
deployah plan <environment> [flags]
```

### Options

```text
      --detailed-exitcode   Exit 2 when the plan has pending changes, 0 when it does not, 1 on error (for CI)
      --drift               Detect drift between the rendered manifests and the live cluster state (requires cluster access; not compatible with --offline)
      --offline             Render and validate the chart without contacting the cluster
      --output string       Output format (default "text")
      --raw                 Show raw Kubernetes field paths instead of the compact Deployah vocabulary
      --show-secrets        Reveal masked secret values in text output (requires an interactive terminal; refused with --output json)
      --yaml                Show changed fields as YAML blocks instead of a single line
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
