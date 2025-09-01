package status

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/deployah-dev/deployah/internal/cli"
	"github.com/deployah-dev/deployah/internal/k8s"
	"github.com/deployah-dev/deployah/internal/runtime"
	"github.com/deployah-dev/deployah/internal/ui"
	"github.com/spf13/cobra"
	v1 "helm.sh/helm/v4/pkg/release/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

// New creates the status sub-command for the CLI.
func New() *cobra.Command {
	statusCommand := &cobra.Command{
		Use:   "status <project>",
		Short: "Display the status of a project",
		Long:  `Display detailed status information about a deployed project, including its current state, revision, and resources.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("exactly one project name is required")
			}

			if strings.TrimSpace(args[0]) == "" {
				return fmt.Errorf("project name cannot be empty")
			}

			return nil
		},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := cmd.Flags().GetString("output")
			if err != nil {
				return fmt.Errorf("failed to get output format: %w", err)
			}

			if !slices.Contains(cli.OutputFormats, outputFormat) {
				return fmt.Errorf("invalid output format: '%s', must be one of: %s", outputFormat, strings.Join(cli.OutputFormats, ", "))
			}

			return nil
		},
		RunE: runStatus,
		Example: `
# Display status for a specific project
deployah status my-app

# Display status for a specific project in a specific environment
deployah status my-app --environment prod

# Show detailed pod information
deployah status my-app --detailed`,
	}

	statusCommand.Flags().String("output", cli.OutputFormatTable, "Output format: table, json")
	statusCommand.Flags().String("environment", "", "Environment to display status for")
	statusCommand.Flags().Bool("detailed", false, "Show detailed pod information")

	return statusCommand
}

func runStatus(cmd *cobra.Command, args []string) error {
	project := args[0]

	outputFormat, err := cmd.Flags().GetString("output")
	if err != nil {
		return fmt.Errorf("failed to get output format: %w", err)
	}

	environment, err := cmd.Flags().GetString("environment")
	if err != nil {
		return fmt.Errorf("failed to get environment: %w", err)
	}

	detailed, err := cmd.Flags().GetBool("detailed")
	if err != nil {
		return fmt.Errorf("failed to get detailed flag: %w", err)
	}

	helmClient, err := cli.GetHelmClient(cmd.Context())
	if err != nil {
		return fmt.Errorf("failed to initialize helm client: %w", err)
	}

	releases, err := getReleasesForProject(cmd.Context(), project, environment, helmClient)
	if err != nil {
		return fmt.Errorf("failed to get releases: %w", err)
	}

	// Get Kubernetes client for pod information if needed
	var k8sClient *k8s.Client
	if detailed {
		rt := runtime.FromRuntime(cmd.Context())
		if rt == nil {
			return fmt.Errorf("runtime not available in context - cannot get detailed pod information")
		}
		k8sClient, err = k8s.NewClientFromRuntime(cmd.Context(), rt)
		if err != nil {
			return fmt.Errorf("failed to create k8s client: %w", err)
		}
	}

	return outputReleases(cmd.Context(), releases, outputFormat, detailed, k8sClient)
}

// getReleasesForProject retrieves releases for the specified project and environment.
// If environment is empty, returns releases for all environments of the project.
func getReleasesForProject(ctx context.Context, project, environment string, helmClient runtime.HelmClient) ([]*v1.Release, error) {
	selector := labels.NewSelector()

	req, err := labels.NewRequirement(k8s.ProjectLabel, selection.Equals, []string{project})
	if err != nil {
		return nil, fmt.Errorf("failed to create project label requirement: %w", err)
	}
	selector = selector.Add(*req)

	if environment != "" {
		req, err := labels.NewRequirement(k8s.EnvironmentLabel, selection.Equals, []string{environment})
		if err != nil {
			return nil, fmt.Errorf("failed to create environment label requirement: %w", err)
		}
		selector = selector.Add(*req)
	}

	releases, err := helmClient.ListReleases(ctx, selector)
	if err != nil {
		return nil, fmt.Errorf("failed to list releases: %w", err)
	}

	if len(releases) == 0 {
		errorMsg := fmt.Sprintf("no releases found for project '%s'", project)
		if environment != "" {
			errorMsg += fmt.Sprintf(" in environment '%s'", environment)
		}
		errorMsg += "\n\nHint: Use 'deployah list' to see all available projects and environments"
		return nil, fmt.Errorf("%s", errorMsg)
	}

	return releases, nil
}

// outputReleases handles the case when multiple environments are found
func outputReleases(ctx context.Context, releases []*v1.Release, outputFormat string, detailed bool, k8sClient *k8s.Client) error {
	// Sort releases by environment name for consistent output
	sort.Slice(releases, func(i, j int) bool {
		return releases[i].Name < releases[j].Name
	})

	switch outputFormat {
	case cli.OutputFormatJSON:
		return outputJSON(ctx, releases, detailed, k8sClient)
	case cli.OutputFormatTable:
		printTable(ctx, releases, detailed, k8sClient)
		return nil
	default:
		return fmt.Errorf("unsupported output format: %s", outputFormat)
	}
}

// outputJSON outputs multiple releases in JSON format
func outputJSON(ctx context.Context, releases []*v1.Release, detailed bool, k8sClient *k8s.Client) error {
	viewModels := make([]cli.ReleaseViewModel, len(releases))
	for i, release := range releases {
		if detailed && k8sClient != nil {
			viewModels[i] = cli.ReleaseToViewModelWithPods(ctx, k8sClient, release)
		} else {
			viewModels[i] = cli.ReleaseToViewModel(release)
		}
	}

	out, err := json.MarshalIndent(viewModels, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize json: %w", err)
	}

	// Apply syntax highlighting if supported
	colorized, err := cli.ColorizeJSONWithChroma(out)
	if err != nil {
		// Fallback to plain JSON if colorization fails
		fmt.Println(string(out))
		return nil
	}

	fmt.Print(colorized)
	return nil
}

// printTable prints multiple releases in a table format using the new UI table component
func printTable(ctx context.Context, releases []*v1.Release, detailed bool, k8sClient *k8s.Client) {
	// Choose columns based on detailed flag
	columns := cli.GetTableColumns(detailed)

	table := ui.NewTable().SetColumns(columns)

	// Convert releases to table rows
	rows := make([]ui.Row, len(releases))
	for i, release := range releases {
		var vm cli.ReleaseViewModel
		if detailed && k8sClient != nil {
			vm = cli.ReleaseToViewModelWithPods(ctx, k8sClient, release)
		} else {
			vm = cli.ReleaseToViewModel(release)
		}

		// Format status with indicator dot
		statusText := fmt.Sprintf("● %s", vm.Status)

		row := ui.Row{
			"project":     vm.Project,
			"environment": vm.Environment,
			"status":      statusText,
			"revision":    fmt.Sprintf("%d", vm.Revision),
			"age":         vm.Age,
			"namespace":   vm.Namespace,
		}

		// Add pod information only if detailed mode is enabled
		if detailed && vm.PodCount > 0 {
			row["pods"] = fmt.Sprintf("%d", vm.PodCount)
			row["ready"] = vm.PodStatus
		}

		rows[i] = row
	}

	table.SetRows(rows).Print()
}
