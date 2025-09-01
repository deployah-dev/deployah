package list

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/deployah-dev/deployah/internal/cli"
	"github.com/deployah-dev/deployah/internal/k8s"
	"github.com/deployah-dev/deployah/internal/logging"
	"github.com/deployah-dev/deployah/internal/ui"
	"github.com/spf13/cobra"
	v1 "helm.sh/helm/v4/pkg/release/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

// New creates the list sub-command for the CLI.
func New() *cobra.Command {
	listCommand := &cobra.Command{
		Use:   "list",
		Short: "List deployed projects",
		Long:  `List all deployed projects in the current namespace.`,
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
		RunE: runList,
		Example: `
# List all deployed projects
deployah list

# List projects by project name
deployah list --project my-app

# List projects by environment name
deployah list --environment prod

# List projects filtered by both project and environment
deployah list --project my-app --environment prod`,
	}

	listCommand.Flags().StringP("output", "o", cli.OutputFormatTable, "Output format: table, json")
	listCommand.Flags().StringP("project", "p", "", "Filter by project name")
	listCommand.Flags().StringP("environment", "e", "", "Filter by environment name")

	return listCommand
}

func runList(cmd *cobra.Command, args []string) error {
	logger := logging.GetLogger(cmd)

	helmClient, err := cli.GetHelmClient(cmd.Context())
	if err != nil {
		return fmt.Errorf("failed to initialize helm client: %w", err)
	}

	outputFormat, err := cmd.Flags().GetString("output")
	if err != nil {
		return fmt.Errorf("failed to get output format: %w", err)
	}

	// Get filtering flags
	project, err := cmd.Flags().GetString("project")
	if err != nil {
		return fmt.Errorf("failed to get project filter: %w", err)
	}

	environment, err := cmd.Flags().GetString("environment")
	if err != nil {
		return fmt.Errorf("failed to get environment filter: %w", err)
	}

	selector := labels.NewSelector()
	if project != "" {
		req, err := labels.NewRequirement(k8s.ProjectLabel, selection.Equals, []string{project})
		if err != nil {
			return fmt.Errorf("failed to create project label requirement: %w", err)
		}
		selector = selector.Add(*req)
	}

	if environment != "" {
		req, err := labels.NewRequirement(k8s.EnvironmentLabel, selection.Equals, []string{environment})
		if err != nil {
			return fmt.Errorf("failed to create environment label requirement: %w", err)
		}
		selector = selector.Add(*req)
	}

	releases, err := helmClient.ListReleases(cmd.Context(), selector)
	if err != nil {
		return fmt.Errorf("failed to list releases: %w", err)
	}

	// Filter out nil releases
	validReleases := make([]*v1.Release, 0, len(releases))
	for _, release := range releases {
		if release != nil {
			validReleases = append(validReleases, release)
		}
	}

	if len(validReleases) == 0 {
		errorMsg := "no releases found"
		if project != "" || environment != "" {
			errorMsg += " matching the specified filters"
			if project != "" {
				errorMsg += fmt.Sprintf(" (project: %s)", project)
			}
			if environment != "" {
				errorMsg += fmt.Sprintf(" (environment: %s)", environment)
			}
		}
		logger.Info(errorMsg)
		return nil
	}

	switch outputFormat {
	case cli.OutputFormatTable:
		printReleasesTable(validReleases)
	case cli.OutputFormatJSON:
		viewModels := make([]cli.ReleaseViewModel, len(validReleases))
		for i, release := range validReleases {
			viewModels[i] = cli.ReleaseToViewModel(release)
		}

		out, err := json.MarshalIndent(viewModels, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to serialize JSON: %w", err)
		}

		// Apply syntax highlighting if supported
		colorized, err := cli.ColorizeJSONWithChroma(out)
		if err != nil {
			// Fallback to plain JSON if colorization fails
			fmt.Println(string(out))
			return nil
		}

		fmt.Print(colorized)
	default:
		return fmt.Errorf("unsupported output format: %s", outputFormat)
	}

	return nil
}

func printReleasesTable(releases []*v1.Release) {
	table := ui.NewTable().SetColumns(cli.GetTableColumns(false))

	// Convert releases to table rows
	rows := make([]ui.Row, 0, len(releases))
	for _, rel := range releases {
		if rel == nil {
			continue
		}

		vm := cli.ReleaseToViewModel(rel)

		// Format status with indicator dot
		statusText := fmt.Sprintf("● %s", vm.Status)

		rows = append(rows, ui.Row{
			"project":     vm.Project,
			"environment": vm.Environment,
			"status":      statusText,
			"revision":    fmt.Sprintf("%d", vm.Revision),
			"age":         vm.Age,
			"namespace":   vm.Namespace,
		})
	}

	table.SetRows(rows).Print()
}
