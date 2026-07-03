## deployah resolve

Show the fully resolved configuration for an environment

### Synopsis

Show the fully resolved configuration for a given environment.

resolve is offline: it never contacts a Kubernetes cluster. It loads the
platform file (deployah.platform.yaml) when present and performs full
resolution, including FQDN construction and TLS mode selection. When the
platform file is absent the output is partial and includes PLATFORM_NOT_FOUND.

Use --output json for byte-stable, machine-readable output.

```text
deployah resolve <environment> [flags]
```

### Options

```text
  -o, --output string   Output format: text or json (default "text")
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
