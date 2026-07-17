## deployah

Deployah turns a spec into a running release on Kubernetes (Spec-to-Release)

### Synopsis

Deployah is a Spec-to-Release tool. You write a short spec; Deployah renders and installs the Helm release, so you do not write Helm charts or Kubernetes YAML.

```text
deployah [flags]
```

### Options

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
* [deployah delete](deployah_delete.md)  - Delete a deployed project in an environment
* [deployah deploy](deployah_deploy.md)  - Deploy a project to a Kubernetes cluster on a given environment
* [deployah init](deployah_init.md)  - Create a new Deployah spec for a project
* [deployah list](deployah_list.md)  - List deployed projects
* [deployah logs](deployah_logs.md)  - View logs for a deployed project
* [deployah plan](deployah_plan.md)  - Preview the changes a deploy would make
* [deployah resolve](deployah_resolve.md)  - Show the fully resolved configuration for an environment
* [deployah shell](deployah_shell.md)  - Connect to a shell in a container
* [deployah status](deployah_status.md)  - Display the status of a project
* [deployah validate](deployah_validate.md)  - Validate a Deployah spec
* [deployah version](deployah_version.md)  - Print version information
