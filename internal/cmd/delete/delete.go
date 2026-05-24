package delete

import (
	"errors"
	"fmt"
	"strings"

	"deployah.dev/deployah/internal/cli"
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/runtime"
	v1 "helm.sh/helm/v4/pkg/release/v1"
	"nabat.dev/nabat"
	"sigs.k8s.io/yaml"
)

type Options struct {
	Project       string `nabat:"project"`
	Environment   string `nabat:"environment"`
	Force         bool   `nabat:"force"`
	DryRun        bool   `nabat:"dry-run"`
	ShowResources bool   `nabat:"show-resources"`
	Output        string `nabat:"output"`
}

// ResourceInfo holds parsed metadata about a single Kubernetes resource from the Helm manifest.
type ResourceInfo struct {
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	Kind       string `json:"kind" yaml:"kind"`
	Name       string `json:"name" yaml:"name"`
	Detail     string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

// DeletePreview is the structured representation of a dry-run delete operation,
// used for JSON/YAML output formats.
type DeletePreview struct {
	Project      string         `json:"project" yaml:"project"`
	Environment  string         `json:"environment" yaml:"environment"`
	Release      string         `json:"release" yaml:"release"`
	Namespace    string         `json:"namespace" yaml:"namespace"`
	Status       string         `json:"status" yaml:"status"`
	Revision     int            `json:"revision" yaml:"revision"`
	LastDeployed string         `json:"lastDeployed" yaml:"lastDeployed"`
	Resources    []ResourceInfo `json:"resources,omitempty" yaml:"resources,omitempty"`
}

func Register(app *nabat.App) {
	app.MustCommand("delete",
		nabat.WithDescription("Delete a deployed project in an environment"),
		nabat.WithLongDescription("Delete (uninstall) a deployed project in an environment from the Kubernetes cluster."),
		nabat.WithAliases("uninstall", "remove"),
		nabat.WithArg("project", "", nabat.WithRequired(), nabat.WithUsage("Project name to delete"), nabat.WithPrompt("Project name", "", nabat.WithHint("e.g. my-app"))),
		nabat.WithArg("environment", "", nabat.WithRequired(), nabat.WithUsage("Environment to delete from"), nabat.WithPrompt("Environment", "", nabat.WithHint("e.g. production"))),
		nabat.WithFlag("force", false, nabat.WithUsage("Force deletion without confirmation")),
		nabat.WithFlag("dry-run", false, nabat.WithUsage("Simulate the deletion without actually removing the project")),
		nabat.WithFlag("show-resources", false, nabat.WithUsage("Show detailed resources that would be deleted (implies --dry-run)")),
		nabat.WithSelectFlag("output", cli.OutputFormatTree, cli.DeleteOutputFormats, nabat.WithShort('o'), nabat.WithUsage("Output format for dry-run preview")),
		nabat.WithExample(`
# Delete a project in an environment
deployah delete my-app production

# Force deletion without confirmation
deployah delete my-app production --force

# Dry run to see what would be deleted
deployah delete my-app production --dry-run

# Show detailed resources that would be deleted
deployah delete my-app production --show-resources

# Output dry-run preview as JSON
deployah delete my-app production --dry-run --output json`),
		nabat.WithRun(runDelete),
	)
}

func runDelete(c *nabat.Context) error {
	opts := &Options{}
	if err := c.Bind(opts); err != nil {
		return fmt.Errorf("binding options: %w", err)
	}

	// show-resources implies dry-run
	if opts.ShowResources {
		opts.DryRun = true
	}

	rt := runtime.FromContext(c)
	helmClient, err := rt.Helm()
	if err != nil {
		return fmt.Errorf("helm client: %w", err)
	}

	c.Logger().Debug("checking project status", "project", opts.Project, "environment", opts.Environment)
	release, err := helmClient.GetRelease(c, opts.Project, opts.Environment)
	if err != nil {
		if errors.Is(err, helm.ErrReleaseNotFound) {
			c.Warn("Project not found", "project", opts.Project, "environment", opts.Environment)
			if !opts.Force {
				return fmt.Errorf("project '%s' in environment '%s': %w — use --force to ignore", opts.Project, opts.Environment, helm.ErrReleaseNotFound)
			}
			c.Info("Continuing with --force despite missing project", "project", opts.Project)
		} else {
			return fmt.Errorf("check project status: %w", err)
		}
	}

	if opts.DryRun {
		return renderDryRunPreview(c, opts.Project, opts.Environment, release, opts.ShowResources, opts.Output)
	}

	if !opts.Force {
		confirmed, confirmErr := c.Confirm(
			fmt.Sprintf("Delete project '%s' in environment '%s'?", opts.Project, opts.Environment),
			nabat.WithAffirmative("Yes, delete it"),
			nabat.WithNegative("No, cancel"),
		)
		if confirmErr != nil {
			return fmt.Errorf("confirmation: %w", confirmErr)
		}
		if !confirmed {
			c.Info("Delete cancelled")
			return nil
		}
	}

	err = c.Spinner(
		fmt.Sprintf("Deleting '%s' in '%s'...", opts.Project, opts.Environment),
		func() error {
			return helmClient.DeleteRelease(c, opts.Project, opts.Environment)
		},
	)
	if err != nil {
		return fmt.Errorf("delete release: %w", err)
	}

	c.Success("Deleted", "project", opts.Project, "environment", opts.Environment)
	return nil
}

func renderDryRunPreview(c *nabat.Context, project, environment string, release *v1.Release, showResources bool, format string) error {
	if release == nil {
		c.Warn("DRY RUN: Project not found — nothing to delete", "project", project, "environment", environment)
		return nil
	}

	preview := buildPreview(project, environment, release, showResources)

	switch format {
	case cli.OutputFormatJSON:
		return c.JSON(preview)
	case cli.OutputFormatYAML:
		return c.YAML(preview)
	default:
		return renderTree(c, project, environment, preview)
	}
}

func buildPreview(project, environment string, release *v1.Release, showResources bool) *DeletePreview {
	p := &DeletePreview{
		Project:     project,
		Environment: environment,
		Release:     release.Name,
		Namespace:   release.Namespace,
		Status:      "unknown",
		LastDeployed: "unknown",
	}
	if release.Info != nil {
		p.Status = release.Info.Status.String()
		if !release.Info.LastDeployed.IsZero() {
			p.LastDeployed = release.Info.LastDeployed.Format("2006-01-02 15:04:05 MST")
		}
	}
	if release.Version > 0 {
		p.Revision = int(release.Version)
	}
	if showResources && release.Manifest != "" {
		p.Resources = parseResources(release.Manifest)
	}
	return p
}

func renderTree(c *nabat.Context, project, environment string, preview *DeletePreview) error {
	c.Warn("DRY RUN — no changes will be made")

	children := []nabat.TreeNode{
		{Value: fmt.Sprintf("Release: %s", preview.Release)},
		{Value: fmt.Sprintf("Namespace: %s", preview.Namespace)},
		{Value: fmt.Sprintf("Status: %s", preview.Status)},
		{Value: fmt.Sprintf("Revision: %d", preview.Revision)},
		{Value: fmt.Sprintf("Last Deployed: %s", preview.LastDeployed)},
	}

	if len(preview.Resources) > 0 {
		children = append(children, buildResourceNodes(preview.Resources))
	}

	root := fmt.Sprintf("%s (%s)", project, environment)
	c.Tree(root, children, nabat.WithTreeEnumerator(nabat.TreeRoundedEnumerator()))

	c.Warn("This permanently deletes all resources and Helm release history")
	c.Info("To perform the actual deletion, run without --dry-run",
		"command", fmt.Sprintf("deployah delete %s %s", project, environment),
	)
	return nil
}

func buildResourceNodes(resources []ResourceInfo) nabat.TreeNode {
	// Group by kind, preserving the order of first appearance.
	type kindGroup struct {
		kind  string
		items []ResourceInfo
	}
	var order []string
	grouped := make(map[string]*kindGroup)
	for _, r := range resources {
		if _, seen := grouped[r.Kind]; !seen {
			order = append(order, r.Kind)
			grouped[r.Kind] = &kindGroup{kind: r.Kind}
		}
		grouped[r.Kind].items = append(grouped[r.Kind].items, r)
	}

	kindNodes := make([]nabat.TreeNode, 0, len(order))
	for _, kind := range order {
		group := grouped[kind]
		nameLeaves := make([]nabat.TreeNode, 0, len(group.items))
		for _, r := range group.items {
			label := r.Name
			if r.Detail != "" {
				label = fmt.Sprintf("%s (%s)", r.Name, r.Detail)
			}
			nameLeaves = append(nameLeaves, nabat.TreeNode{Value: label})
		}
		kindNodes = append(kindNodes, nabat.TreeNode{
			Value:    kind,
			Children: nameLeaves,
		})
	}

	return nabat.TreeNode{
		Value:    fmt.Sprintf("Resources (%d)", len(resources)),
		Children: kindNodes,
	}
}

// parseResources splits a Helm manifest into individual YAML documents and
// extracts kind-specific detail for each resource.
func parseResources(manifest string) []ResourceInfo {
	var resources []ResourceInfo
	for _, doc := range strings.Split(manifest, "---") {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		var base struct {
			APIVersion string `yaml:"apiVersion"`
			Kind       string `yaml:"kind"`
			Metadata   struct {
				Name string `yaml:"name"`
			} `yaml:"metadata"`
		}
		if err := yaml.Unmarshal([]byte(doc), &base); err != nil || base.Kind == "" {
			continue
		}

		resources = append(resources, ResourceInfo{
			APIVersion: base.APIVersion,
			Kind:       base.Kind,
			Name:       base.Metadata.Name,
			Detail:     extractDetail(base.Kind, doc),
		})
	}
	return resources
}

// extractDetail returns a short human-readable attribute string for well-known
// Kubernetes resource kinds. Returns an empty string for unknown kinds.
func extractDetail(kind, doc string) string {
	switch kind {
	case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet":
		var obj struct {
			Spec struct {
				Replicas *int `yaml:"replicas"`
			} `yaml:"spec"`
		}
		if err := yaml.Unmarshal([]byte(doc), &obj); err == nil && obj.Spec.Replicas != nil {
			return fmt.Sprintf("replicas: %d", *obj.Spec.Replicas)
		}
	case "Service":
		var obj struct {
			Spec struct {
				Type  string `yaml:"type"`
				Ports []struct {
					Port int `yaml:"port"`
				} `yaml:"ports"`
			} `yaml:"spec"`
		}
		if err := yaml.Unmarshal([]byte(doc), &obj); err == nil {
			svcType := obj.Spec.Type
			if svcType == "" {
				svcType = "ClusterIP"
			}
			if len(obj.Spec.Ports) > 0 {
				return fmt.Sprintf("%s, port: %d", svcType, obj.Spec.Ports[0].Port)
			}
			return svcType
		}
	case "Ingress":
		var obj struct {
			Spec struct {
				Rules []struct {
					Host string `yaml:"host"`
				} `yaml:"rules"`
			} `yaml:"spec"`
		}
		if err := yaml.Unmarshal([]byte(doc), &obj); err == nil && len(obj.Spec.Rules) > 0 && obj.Spec.Rules[0].Host != "" {
			return fmt.Sprintf("host: %s", obj.Spec.Rules[0].Host)
		}
	case "Secret":
		var obj struct {
			Type string `yaml:"type"`
		}
		if err := yaml.Unmarshal([]byte(doc), &obj); err == nil && obj.Type != "" {
			return obj.Type
		}
		return "Opaque"
	case "PersistentVolumeClaim":
		var obj struct {
			Spec struct {
				Resources struct {
					Requests map[string]string `yaml:"requests"`
				} `yaml:"resources"`
			} `yaml:"spec"`
		}
		if err := yaml.Unmarshal([]byte(doc), &obj); err == nil {
			if storage, ok := obj.Spec.Resources.Requests["storage"]; ok {
				return fmt.Sprintf("storage: %s", storage)
			}
		}
	}
	return ""
}
