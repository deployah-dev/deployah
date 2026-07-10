## deployah init

Create a new Deployah spec for a project

### Synopsis

Create a new Deployah spec for a project

```text
deployah init [flags]
```

### Options

```text
      --defaults               Skip every prompt and use built-in defaults
      --dry-run                Preview the generated spec without saving it
      --environments strings   Comma-separated environment names, e.g. local,production (skips the environments prompt) (env: DEPLOYAH_ENVIRONMENTS)
      --force                  Overwrite the output file if it already exists
  -o, --output string          The output file path. (default "deployah.yaml")
      --project string         Project name (skips the project name prompt) (env: DEPLOYAH_PROJECT)
      --set strings            Set a value on the generated spec using a Helm-style dotted path, e.g. components.web.image=nginx:1.25 or components.web.port=8080 (repeatable). Values are coerced to int/number/bool only where the manifest schema declares that field's type; everything else stays a string.
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
