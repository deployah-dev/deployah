## deployah run

Run a spec task as a one-off Job

### Synopsis

Create a Kubernetes Job for a task from the spec. Works for preDeploy, postDeploy, and manual tasks. Runs only the named task; tasks listed in its after field are not run. Waits for completion unless --detach is set.

```text
deployah run <task> <environment> [flags]
```

### Options

```text
      --count int         Override fanout count for this run
      --detach            Return after creating the Job without waiting for completion
      --parallelism int   Override how many copies may run at once
  -y, --yes               Run without an interactive confirmation prompt
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
