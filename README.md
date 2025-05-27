# Deployah

> **WARNING:** Deployah is experimental and under active development.

Deployah is a CLI tool that makes deploying applications effortless by leveraging Helm—no Kubernetes or Helm expertise required.

---

## Quick Start

1. **Create a `.deployah.yaml` manifest** in your project root.
2. **(Optional) Add a `.env` or `.env.<envName>` file** for environment-specific variables.
3. **Run Deployah:**
   ```sh
   deployah --env production
   ```

---

## Environments & Configuration

Deployah supports multiple environments (e.g., dev, staging, prod) for flexible deployments.

### Minimal Example

You can omit the `environments` section for a single default environment:

```yaml
components:
  my-app:
    image: my-image:${IMAGE}
```

### Multiple Environments

If you define more than one environment, you **must** specify which one to use:

```yaml
environments:
  - name: dev
    variables:
      FOO: bar
  - name: prod
    variables:
      FOO: baz
components:
  my-app:
    image: my-image:${IMAGE}
```

**Selection rules:**
- If `environments` is omitted, Deployah uses a built-in `default` environment.
- If only one environment is defined, it is used automatically.
- If multiple environments are defined, you must specify `--env <name>`.
- If you do not specify `--env` and multiple environments exist, Deployah will show an error listing available environments.

---

## Variable Substitution Precedence

When substituting variables in your `.deployah.yaml` manifest, Deployah uses the following precedence (lowest to highest):

1. **Environment Definition Variables:**  
   Defined in the `variables` field of the selected environment in your manifest.
2. **Env File Variables:**  
   Loaded from the resolved environment file (e.g., `.env.production`).
3. **OS Environment Variables:**  
   From your shell, with the `DPY_VAR_` prefix (these override all others).

**Example:**
```yaml
# In .deployah.yaml
environments:
  - name: production
    variables:
      APP_ENV: manifest
```
```env
# In .env.production
DPY_VAR_APP_ENV=envfile
```
```sh
# In your shell
export DPY_VAR_APP_ENV=osenv
```
The value used for `${APP_ENV}` will be `osenv`.

> **Note:** In `.deployah.yaml`, reference variables without the `DPY_VAR_` prefix (e.g., `${IMAGE}`).

---

## File Usage: Deployah vs. Application

| File                      | Used by         | Purpose                                 |
|---------------------------|-----------------|-----------------------------------------|
| `.deployah.yaml`          | Deployah        | Main Deployah manifest/config           |
| `.env` / `.env.<envName>` | Deployah & App  | Variable substitution for both; Deployah only uses variables starting with `DPY_VAR_` |
| `config.yaml`             | Application     | App-specific config, ignored by Deployah|
| `config.<envName>.yaml`   | Application     | App-specific config for named environments, ignored by Deployah|

- **Deployah only reads `.deployah.yaml` and `.env` files.**
- **Deployah only uses variables from `.env` that start with `DPY_VAR_`.**
- **Variables in `.env` (or `.env.<envName>`) that do NOT start with `DPY_VAR_` are available for your application, but are ignored by Deployah.**
- **`config.yaml` and `config.<envName>.yaml` are ignored by Deployah** (they're for your app).

---

## Environment File Conventions

- **Default environment:**  
  - Deployah uses `.env` for variable substitution (only variables starting with `DPY_VAR_`).
  - The application uses `config.yaml` for its configuration.
- **Named environments:**  
  - Deployah uses `.env.<envName>` for variable substitution (e.g., `.env.production`).
  - The application uses `config.<envName>.yaml` for its configuration.

---

## Example: Default and Named Environments

**Default:**
```yaml
# .deployah.yaml
components:
  my-app:
    image: my-image:${IMAGE}
```
```
# .env
DPY_VAR_IMAGE=my-image:latest
BAR=baz  # For your app, ignored by Deployah
```
```yaml
# config.yaml (for your app)
someAppSetting: true
```

**Named (production):**
```
# .env.production
DPY_VAR_IMAGE=my-image:prod
DPY_VAR_API_URL=https://api.example.com
APP_SECRET=supersecret  # For your app, ignored by Deployah
```
```yaml
# config.production.yaml (for your app)
someAppSetting: false
apiUrl: https://api.example.com
```

---

## Why This Approach?

- **Minimal for beginners, powerful for experts**
- **No accidental deployments to the wrong environment**
- **No magic or hidden defaults**
- **Easy to document and reason about**

---

## Help

For more details, see the documentation or run:
```sh
deployah --help
```
