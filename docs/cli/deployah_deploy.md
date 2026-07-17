## deployah deploy

Deploy a project to a Kubernetes cluster on a given environment

### Synopsis

Deploy a project to a Kubernetes cluster on a given environment. Shows what would change and asks for confirmation before applying, unless --yes is set.

```text
deployah deploy <environment> [flags]
```

### Options

```text
      --explain                 Print the resolution report before cluster checks (visible even when cluster is unreachable)
      --force-hostname-change   Allow changing the resolved hostname even though it may break existing traffic (skips the hostname guard)
      --reapply                 Upgrade the release even when the plan shows no changes
  -y, --yes                     Apply without an interactive confirmation prompt
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
