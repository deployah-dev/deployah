# Deployah - Simplified CLI for Application Deployment using Helm

## **Overview**

This proposal outlines the development of a CLI tool named **Deployah** that simplifies the deployment of applications using Helm without requiring any prior knowledge of Kubernetes or Helm. The tool will serve as a wrapper around the Helm Go SDK, abstracting the complexities of Helm charts and Kubernetes manifests, enabling a seamless and user-friendly deployment experience.

### **Pronunciation**
Deployah is pronounced as **"Dee-Ploy-Yah"** (/dɪˈplɔɪ.jə/). The name is a playful and modern take on "Deploy," adding an energetic and approachable feel.

## **Goals & Objectives**

### **Primary Goals:**

- Provide a simple CLI for deploying applications without exposing Kubernetes or Helm concepts.
- Automate Helm chart selection and configuration based on minimal user input.
- Manage application lifecycle (install, upgrade, rollback, delete) with intuitive commands.
- Support declarative configuration via YAML, JSON, or TOML.

### **Non-Goals:**

- Deployah will not replace Helm for advanced Kubernetes users but will act as a simplified abstraction.
- It will not provide a UI since the focus is on a CLI-first experience.

## **Technical Approach**

The CLI will be built in **Go** and utilize the **Helm Go SDK** for managing Helm releases. It will process simple configuration files and translate them into Helm commands, handling complexities like namespaces, values, and deployments automatically.

### **Core Features**

#### **1. Minimal Configuration Deployment**

Users will provide a simple configuration file to define their applications:

```yaml
app: my-app
version: latest
ports:
  - 8080
env:
  DB_URL: postgres://db:5432
scaling:
  min: 2
  max: 5
```

Deployah will automatically map these values to Helm chart parameters.

#### **2. Automatic Chart Selection & Values Mapping**

- Deployah will maintain a set of predefined Helm charts.
- Based on the `app` value, it will determine the appropriate chart and apply the user’s configuration.

#### **3. CLI Commands**

| Command                         | Description                                              |
|---------------------------------|----------------------------------------------------------|
| `deployah deploy <config-file>` | Deploys the application using the defined configuration. |
| `deployah list`                 | Lists all deployed applications.                         |
| `deployah upgrade <app>`        | Upgrades an existing deployment.                         |
| `deployah rollback <app>`       | Rolls back to the previous version.                      |
| `deployah delete <app>`         | Uninstalls an application.                               |

#### **4. Helm SDK Integration**

Deployah will leverage Helm’s Go SDK to interact with Helm programmatically. It will:

- Initialize and configure Helm clients.
- Install and upgrade Helm charts.
- Retrieve and manage release information.

### **Technical Stack**

- **Language:** Go
- **Libraries:** Helm Go SDK, Cobra (CLI framework), Viper (config parsing)
- **Package Structure:**
  ```
  /cmd
    - root.go
    - deploy.go
    - list.go
  /pkg
    /helm
      - client.go  // Helm wrapper
    /config
      - parser.go  // Reads user configs
  /internal
    - helpers.go
  ```

## **Expected Outcomes**

- A fully functional CLI that enables developers to deploy applications without Kubernetes expertise.
- A simplified workflow that reduces deployment complexity.
- A scalable and maintainable codebase that can be extended with additional features in the future.

## **Next Steps**

1. **Project Initialization**: Set up the repository and structure the CLI.
2. **Helm SDK Integration**: Implement Helm wrapper functions.
3. **Config Parsing & Mapping**: Translate user configurations into Helm values.
4. **CLI Commands Implementation**: Develop core commands (`deploy`, `list`, `upgrade`, `rollback`, `delete`).
5. **Testing & Documentation**: Ensure stability and usability with extensive testing and documentation.

## **Conclusion**

Deployah will significantly reduce the barrier to deploying applications using Helm, providing an intuitive experience while leveraging the power of Kubernetes. By abstracting Helm complexities, it will streamline deployments, making it accessible to a broader audience.
