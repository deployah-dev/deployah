## deployah delete

Delete a deployed project in an environment

### Synopsis

Delete (uninstall) a deployed project in an environment from the Kubernetes cluster. Also deletes leftover Jobs labeled for the project and environment, including CLI runs. --dry-run lists those Jobs even when the Helm release is already gone.

```text
deployah delete <project> <environment> [flags]
```

### Options

```text
      --allow-missing-platform   Allow deletion to proceed even when no platform file is found (uses default kubeconfig context; requires --project and --context or a resolved kubeconfig)
      --dry-run                  Simulate the deletion without actually removing the project
  -o, --output string            Output format for dry-run preview (default "tree")
      --show-resources           Show detailed resources that would be deleted (implies --dry-run)
      --wait                     Wait until all Kubernetes resources are fully deleted before returning (uses stable legacy polling; suitable for CI)
  -y, --yes                      Skip confirmation prompt
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
  -t, --timeout duration       Timeout for Deployah operations (install/upgrade, list, status, logs, delete, run) (default 10m0s)
```

### SEE ALSO

* [deployah](deployah.md)  - Deployah turns a spec into a running release on Kubernetes (Spec-to-Release)
