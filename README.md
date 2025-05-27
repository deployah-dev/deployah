# Deployah

> **WARNING:** Deployah is currently in an experimental phase and undergoing active development

Deployah – A CLI tool that makes deploying applications effortless by leveraging Helm without needing Kubernetes or Helm expertise.

## Environments: Configuration Best Practices

Deployah supports environments to help you manage different settings for different deployment targets (e.g., dev, staging, prod). The approach is designed to be minimal, explicit, and predictable:

### Minimal Configuration
- The `environments` section is **optional**.
- If omitted, Deployah assumes a single, built-in environment (named `default`).
- This allows you to get started with the simplest possible manifest:

```yaml
components:
  - name: my-app
    # ...
# environments is optional!
```

### Multiple Environments
- If you define more than one environment, you **must** specify which one to use via the `--env` flag.
- There is **no default environment** chosen automatically if multiple are present.
- Example:

```yaml
environments:
  - name: dev
    variables:
      FOO: bar
  - name: prod
    variables:
      FOO: baz
components:
  - name: my-app
    # ...
```

### Selection Rules
- If `environments` is omitted, Deployah uses a built-in `default` environment.
- If only one environment is defined, it is used automatically.
- If multiple environments are defined, you must specify `--env <name>`.
- If you do not specify `--env` and multiple environments exist, Deployah will show an error listing available environments.

### Variable Resolution Order for .deployah.yaml Template Variables

When Deployah substitutes template variables in your `.deployah.yaml`, it resolves their values in the following order (highest to lowest priority):

| Priority | Source                                      | Description                                                                                   |
|----------|---------------------------------------------|-----------------------------------------------------------------------------------------------|
| 1        | OS environment variables (`DPY_VAR_` prefix)| **Always available for substitution, even if no env file is found.**                          |
| 2        | Variables from the resolved env file        | `.env.<envName>` in the project root, filtered for `DPY_VAR_` prefix                          |
| 3        | Variables from the environment definition   | The `variables` block in the selected environment in `.deployah.yaml`                         |

- If `envFile` is set in the manifest for the environment, Deployah will use it. If it does not exist, Deployah will error out.
- If not set, Deployah will look for `.env.<envName>` in the project root.
- If no file is found, Deployah proceeds without an env file.

#### Example: Explicit envFile

```yaml
environments:
  - name: production
    envFile: .env.production
    variables:
      IMAGE_TAG: latest
```
If `.env.production` does not exist, Deployah will error and stop.

#### Example: Convention-based env file

```yaml
environments:
  - name: staging
    # no envFile set
    variables:
      IMAGE_TAG: dev
```
Deployah will look for `.env.staging` in the project root.

### Variable Substitution Precedence (Summary)

When substituting variables in your manifest, Deployah uses the above order. This means OS environment variables can override env file and manifest variables, and env file variables can override manifest variables. Even if no env file is found, OS environment variables with the `DPY_VAR_` prefix will still be used for substitution.

#### Example: Variable Precedence

Suppose you have:
- In manifest: `IMAGE_TAG: latest`
- In `.env.production`: `DPY_VAR_IMAGE_TAG=prod`
- In your shell: `DPY_VAR_IMAGE_TAG=from-shell`

Then `${IMAGE_TAG}` in your manifest will resolve to `from-shell` if set, otherwise `prod`, otherwise `latest`.

### Why This Approach?
- **Minimal for beginners, powerful for experts**
- **No accidental deployments to the wrong environment**
- **No magic or hidden defaults**
- **Easy to document and reason about**

For more details, see the documentation or run `deployah --help`.

---

## File Usage: Deployah vs. Application

Deployah distinguishes between its own configuration and your application's configuration files:

| File                      | Used by         | Purpose                                 |
|---------------------------|-----------------|-----------------------------------------|
| `.deployah.yaml`          | Deployah        | Main Deployah manifest/config           |
| `.env`                    | Deployah & App  | Variable substitution for both; Deployah only uses variables starting with `DPY_VAR_` |
| `config.yaml`             | Application     | App-specific config, ignored by Deployah|
| `config.{env_name}.yaml`  | Application     | App-specific config for named environments, ignored by Deployah|

- **Deployah only reads `.deployah.yaml` and `.env`**
- **Deployah only uses variables from `.env` that start with `DPY_VAR_`**
- **Variables in `.env` (or `.env.{env_name}`) that do NOT start with `DPY_VAR_` are available for your application, but are ignored by Deployah**
- **`config.yaml` and `config.{env_name}.yaml` are ignored by Deployah** (they're for your app)
- **You can omit `environments` in `.deployah.yaml` for minimal config**

### Environment File Conventions

- **Default environment:**
  - Deployah uses `.env` for variable substitution (only variables starting with `DPY_VAR_`)
  - The application uses `config.yaml` for its configuration
  - The application can also use any other variables in `.env` (those not starting with `DPY_VAR_`)
- **Named environments:**
  - Deployah uses `.env.{env_name}` for variable substitution (e.g., `.env.production` for the `production` environment)
  - The application uses `config.{env_name}.yaml` for its configuration (e.g., `config.production.yaml`)
  - The application can also use any other variables in `.env.{env_name}` (those not starting with `DPY_VAR_`)

> **Note:** When referencing variables in `.deployah.yaml`, **omit the `DPY_VAR_` prefix**. For example, if your `.env` file contains `DPY_VAR_IMAGE=my-image:latest`, you reference it as `${IMAGE}` in `.deployah.yaml`.

#### Example: Default Environment

**.deployah.yaml**
```yaml
components:
  - name: my-app
    image: my-image:${IMAGE}
    # ...
# environments is optional!
```

**.env**
```
DPY_VAR_IMAGE=my-image:latest
BAR=baz  # Available for your application, ignored by Deployah
```

**config.yaml**
```yaml
# Used by your application, not Deployah
someAppSetting: true
```

#### Example: Named Environment (e.g., production)

**.env.production**
```
DPY_VAR_IMAGE=my-image:prod
DPY_VAR_API_URL=https://api.example.com
APP_SECRET=supersecret  # Available for your application, ignored by Deployah
```

**config.production.yaml**
```yaml
# Used by your application, not Deployah
someAppSetting: false
apiUrl: https://api.example.com
```

When you run Deployah with `--env production`, it will use variables from `.env.production` (only those starting with `DPY_VAR_`), and your application can use `config.production.yaml` and any other variables in `.env.production` (those not starting with `DPY_VAR_`). The same convention applies for any other environment name.
