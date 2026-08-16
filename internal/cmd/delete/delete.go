package delete

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"nabat.dev/nabat"
	"sigs.k8s.io/yaml"

	"deployah.dev/deployah/internal/cli"
	"deployah.dev/deployah/internal/cmd/cmdopts"
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/k8s"
	"deployah.dev/deployah/internal/session"
	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// Options holds command-line flags for delete.
type Options struct {
	Project              string `nabat:"project"`
	Environment          string `nabat:"environment"`
	Yes                  bool   `nabat:"yes"`
	DryRun               bool   `nabat:"dry-run"`
	ShowResources        bool   `nabat:"show-resources"`
	Output               string `nabat:"output"`
	Wait                 bool   `nabat:"wait"`
	AllowMissingPlatform bool   `nabat:"allow-missing-platform"`
}

// ResourceInfo holds parsed metadata about a single Kubernetes resource
// from the Helm manifest.
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
	// Jobs are leftover CLI or hook Jobs labeled for this project and
	// environment. Live delete removes them even when the Helm release is
	// already gone.
	Jobs []string `json:"jobs,omitempty" yaml:"jobs,omitempty"`
}

// Register adds the delete command to app.
func Register(app *nabat.App) {
	app.MustCommand("delete",
		nabat.WithDescription("Delete a deployed project in an environment"),
		nabat.WithLongDescription("Delete (uninstall) a deployed project in an environment from the Kubernetes cluster. Also deletes leftover Jobs labeled for the project and environment, including CLI runs. --dry-run lists those Jobs even when the Helm release is already gone."),
		nabat.WithAliases("uninstall", "remove"),
		nabat.WithArg("project", "", nabat.WithRequired(), nabat.WithUsage("Project name to delete"), nabat.WithPrompt("Project name", "", nabat.WithHint("e.g. my-app"))),
		nabat.WithArg("environment", "", nabat.WithRequired(), nabat.WithUsage("Environment to delete from"), nabat.WithPrompt("Environment", "", nabat.WithHint("e.g. production"))),
		nabat.WithFlag("yes", false, nabat.WithShort('y'), nabat.WithUsage("Skip confirmation prompt")),
		nabat.WithFlag("dry-run", false, nabat.WithUsage("Simulate the deletion without actually removing the project")),
		nabat.WithFlag("show-resources", false, nabat.WithUsage("Show detailed resources that would be deleted (implies --dry-run)")),
		nabat.WithSelectFlag("output", cli.OutputFormatTree, cli.DeleteOutputFormats, nabat.WithShort('o'), nabat.WithUsage("Output format for dry-run preview")),
		nabat.WithFlag("wait", false, nabat.WithUsage("Wait until all Kubernetes resources are fully deleted before returning (uses stable legacy polling; suitable for CI)")),
		nabat.WithFlag("allow-missing-platform", false, nabat.WithUsage("Allow deletion to proceed even when no platform file is found (uses default kubeconfig context; requires --project and --context or a resolved kubeconfig)")),
		nabat.WithExample(`
# Delete a project in an environment
deployah delete my-app production

# Skip confirmation prompt
deployah delete my-app production --yes

# Skip confirmation (shorthand)
deployah delete my-app production -y

# Dry run to see what would be deleted
deployah delete my-app production --dry-run

# Show detailed resources that would be deleted
deployah delete my-app production --show-resources

# Output dry-run preview as JSON
deployah delete my-app production --dry-run --output json

# Wait until all resources are fully removed (useful in CI)
deployah delete my-app production --wait`),
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

	rt := session.FromContext(c)

	// Fail closed when no platform file is found, unless the escape hatch is
	// active. Without the platform file the delete targets the kubeconfig's
	// default context, which may be the wrong cluster.
	if !opts.AllowMissingPlatform {
		platform, platformErr := rt.Platform()
		if platformErr != nil {
			return fmt.Errorf("load platform file: %w", platformErr)
		}
		if platform == nil {
			return fmt.Errorf(
				"no platform file found (%s or DEPLOYAH_PLATFORM_FILE); "+
					"pass --platform-file to provide the authoritative cluster context, "+
					"or --allow-missing-platform to proceed with the default kubeconfig context",
				spec.DefaultPlatformPath,
			)
		}
	}

	cluster, err := rt.Target(c, opts.Environment)
	if err != nil {
		return fmt.Errorf("target cluster: %w", err)
	}
	cmdopts.WarnContextFallback(c, cluster, opts.Environment)
	helmClient, err := cluster.Helm()
	if err != nil {
		return fmt.Errorf("helm client: %w", err)
	}

	c.Logger().Debug("checking project status", "project", opts.Project, "environment", opts.Environment)
	release, err := helmClient.GetRelease(c, opts.Project, opts.Environment)
	if err != nil {
		if !errors.Is(err, helm.ErrReleaseNotFound) {
			return fmt.Errorf("check project status: %w", err)
		}
		release = nil
	}

	jobs, jobErr := listLabeledJobNames(c, cluster, opts.Project, opts.Environment)
	if jobErr != nil {
		return jobErr
	}

	if opts.DryRun {
		return renderDryRunPreview(c, opts.Project, opts.Environment, release, jobs, opts.ShowResources, opts.Output)
	}

	if nothingToDelete(release, jobs) {
		c.Warn("Project not found, nothing to delete", "project", opts.Project, "environment", opts.Environment)
		return nil
	}

	targetCtx := cluster.Context()
	if fallback, current := cluster.ContextFallback(); fallback {
		targetCtx = current
	}
	prompt := deleteConfirmPrompt(opts.Project, opts.Environment, targetCtx, release, jobs)
	confirmed, confirmErr := c.Confirm(
		prompt,
		nabat.WithAffirmative("Yes, delete it"),
		nabat.WithNegative("No, cancel"),
		nabat.WithYes(opts.Yes),
		nabat.WithBypassHint("--yes"),
	)
	if confirmErr != nil {
		return confirmErr
	}
	if !confirmed {
		c.Info("Delete cancelled")
		return nil
	}

	if release != nil {
		err = c.Spinner(
			func(_ *nabat.Spinner) error {
				return helmClient.DeleteRelease(c, opts.Project, opts.Environment, opts.Wait)
			},
			nabat.WithTitle(fmt.Sprintf("Deleting '%s' in '%s'...", opts.Project, opts.Environment)),
		)
		if err != nil {
			return fmt.Errorf("delete release: %w", err)
		}
	}

	if delErr := deleteLabeledJobs(c, cluster, opts.Project, opts.Environment); delErr != nil {
		return delErr
	}

	c.Success("Deleted", "project", opts.Project, "environment", opts.Environment)
	return nil
}

func deleteLabeledJobs(c *nabat.Context, cluster *session.Cluster, project, environment string) error {
	cs, err := cluster.Kubernetes()
	if err != nil {
		c.Warn("Kubernetes client unavailable; leftover Jobs were not deleted", "err", err)
		return nil
	}
	if err = k8s.DeleteJobs(c, cs, cluster.Namespace(), project, environment); err != nil {
		return fmt.Errorf("delete leftover jobs: %w", err)
	}
	return nil
}

func listLabeledJobNames(c *nabat.Context, cluster *session.Cluster, project, environment string) ([]string, error) {
	cs, err := cluster.Kubernetes()
	if err != nil {
		c.Warn("Kubernetes client unavailable; leftover Jobs were not listed", "err", err)
		return nil, nil
	}
	jobs, err := k8s.ListJobs(c, cs, cluster.Namespace(), project, environment)
	if err != nil {
		return nil, fmt.Errorf("list leftover jobs: %w", err)
	}
	names := make([]string, 0, len(jobs))
	for i := range jobs {
		names = append(names, jobs[i].Name)
	}
	slices.Sort(names)
	return names, nil
}

func renderDryRunPreview(c *nabat.Context, project, environment string, release *v1.Release, jobs []string, showResources bool, format string) error {
	if nothingToDelete(release, jobs) {
		c.Warn("DRY RUN: Project not found, nothing to delete", "project", project, "environment", environment)
		return nil
	}

	preview := buildPreview(project, environment, release, jobs, showResources)

	switch format {
	case cli.OutputFormatJSON:
		return c.JSON(preview)
	case cli.OutputFormatYAML:
		return c.YAML(preview)
	default:
		return renderTree(c, project, environment, preview)
	}
}

func buildPreview(project, environment string, release *v1.Release, jobs []string, showResources bool) *DeletePreview {
	p := &DeletePreview{
		Project:     project,
		Environment: environment,
		Jobs:        jobs,
	}
	if release == nil {
		p.Status = "not found"
		p.LastDeployed = "unknown"
		return p
	}
	p.Release = release.Name
	p.Namespace = release.Namespace
	p.Status = "unknown"
	p.LastDeployed = "unknown"
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

func nothingToDelete(release *v1.Release, jobs []string) bool {
	return release == nil && len(jobs) == 0
}

func deleteConfirmPrompt(project, environment, targetCtx string, release *v1.Release, jobs []string) string {
	var b strings.Builder
	if release != nil {
		fmt.Fprintf(&b, "Delete project '%s' in environment '%s'", project, environment)
	} else {
		fmt.Fprintf(&b, "Delete leftover Jobs for project '%s' in environment '%s': %s",
			project, environment, strings.Join(jobs, ", "))
	}
	if targetCtx != "" {
		fmt.Fprintf(&b, " (context: %s)", targetCtx)
	}
	b.WriteByte('?')
	return b.String()
}

func renderTree(c *nabat.Context, project, environment string, preview *DeletePreview) error {
	c.Warn("DRY RUN, no changes will be made")

	var children []nabat.TreeNode
	if preview.Release != "" {
		children = []nabat.TreeNode{
			{Value: fmt.Sprintf("Release: %s", preview.Release)},
			{Value: fmt.Sprintf("Namespace: %s", preview.Namespace)},
			{Value: fmt.Sprintf("Status: %s", preview.Status)},
			{Value: fmt.Sprintf("Revision: %d", preview.Revision)},
			{Value: fmt.Sprintf("Last Deployed: %s", preview.LastDeployed)},
		}
	} else {
		children = []nabat.TreeNode{
			{Value: "Helm release: not found"},
		}
	}

	if len(preview.Resources) > 0 {
		children = append(children, buildResourceNodes(preview.Resources))
	}
	if len(preview.Jobs) > 0 {
		leaves := make([]nabat.TreeNode, 0, len(preview.Jobs))
		for _, name := range preview.Jobs {
			leaves = append(leaves, nabat.TreeNode{Value: name})
		}
		children = append(children, nabat.TreeNode{
			Value:    fmt.Sprintf("Leftover Jobs (%d)", len(preview.Jobs)),
			Children: leaves,
		})
	}

	root := fmt.Sprintf("%s (%s)", project, environment)
	c.Tree(root, children, nabat.WithTreeEnumerator(nabat.TreeRoundedEnumerator()))

	if preview.Release != "" {
		c.Warn("This permanently deletes all resources and Helm release history")
	} else {
		c.Warn("This deletes leftover Jobs labeled for this project and environment")
	}
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
	for doc := range strings.SplitSeq(manifest, "---") {
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
