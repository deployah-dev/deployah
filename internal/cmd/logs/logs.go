package logs

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/deployah-dev/deployah/internal/k8s"
	"github.com/deployah-dev/deployah/internal/runtime"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/stern/stern/stern"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
)

// New creates the logs sub-command for the CLI.
func New() *cobra.Command {
	logsCommand := &cobra.Command{
		Use:   "logs <project>",
		Short: "View logs for a deployed project",
		Long: `View logs from pods associated with a deployed project. This command connects to Kubernetes to stream logs from the pods.
`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("project name is required")
			}

			if strings.TrimSpace(args[0]) == "" {
				return fmt.Errorf("project name cannot be empty")
			}

			return nil
		},
		RunE: runLogs,
		Example: `
# All pods in the "myproject" project
deployah logs myproject

# Only "api" component pods in the "myproject" project
deployah logs myproject --component=api

# Only "prod" environment pods in the "myproject" project
deployah logs myproject --environment=prod

# Custom log format in the "myproject" project
deployah logs myproject --template="{{.Message}}"

# Template from file in the "myproject" project
deployah logs myproject --template-file=log.tmpl

# Timezone in the "myproject" project
deployah logs myproject --timezone=Asia/Tehran
`,
	}

	logsCommand.Flags().Bool("no-follow", false, "Do not follow log output")
	logsCommand.Flags().String("container", "", "Container name (if pod has multiple containers)")
	logsCommand.Flags().Duration("since", 48*time.Hour, "Show logs since duration (e.g., 10s, 1m, 1h)")
	logsCommand.Flags().Int64("tail", -1, "Number of lines to show from the end of the logs (-1 shows all)")
	logsCommand.Flags().Bool("timestamps", false, "Include timestamps in log output")
	logsCommand.Flags().String("resource", "", "Kubernetes resource to tail (e.g., deployment/myapp)")
	logsCommand.Flags().String("component", "", "Filter by component name (e.g., api, web, worker)")
	logsCommand.Flags().String("environment", "", "Filter by environment name (e.g., dev, staging, prod)")

	logsCommand.Flags().Bool("only-log-lines", false, "Only output the log message lines (suppresses headers; if no template is provided, prints raw messages)")
	logsCommand.Flags().String("template", "", "Go template for each log line")
	logsCommand.Flags().String("template-file", "", "Path to a file containing a Go template for each log line")
	logsCommand.Flags().String("timezone", time.Local.String(), "Timezone for timestamps (e.g., Europe/Amsterdam)")

	return logsCommand
}

func runLogs(cmd *cobra.Command, args []string) error {
	// Extract all flag values with proper error handling
	noFollow, err := cmd.Flags().GetBool("no-follow")
	if err != nil {
		return fmt.Errorf("failed to get no-follow flag: %w", err)
	}
	container, err := cmd.Flags().GetString("container")
	if err != nil {
		return fmt.Errorf("failed to get container flag: %w", err)
	}
	since, err := cmd.Flags().GetDuration("since")
	if err != nil {
		return fmt.Errorf("failed to get since flag: %w", err)
	}
	tail, err := cmd.Flags().GetInt64("tail")
	if err != nil {
		return fmt.Errorf("failed to get tail flag: %w", err)
	}
	timestamps, err := cmd.Flags().GetBool("timestamps")
	if err != nil {
		return fmt.Errorf("failed to get timestamps flag: %w", err)
	}
	resource, err := cmd.Flags().GetString("resource")
	if err != nil {
		return fmt.Errorf("failed to get resource flag: %w", err)
	}
	component, err := cmd.Flags().GetString("component")
	if err != nil {
		return fmt.Errorf("failed to get component flag: %w", err)
	}
	environment, err := cmd.Flags().GetString("environment")
	if err != nil {
		return fmt.Errorf("failed to get environment flag: %w", err)
	}
	onlyLogLines, err := cmd.Flags().GetBool("only-log-lines")
	if err != nil {
		return fmt.Errorf("failed to get only-log-lines flag: %w", err)
	}
	templateStr, err := cmd.Flags().GetString("template")
	if err != nil {
		return fmt.Errorf("failed to get template flag: %w", err)
	}
	templateFile, err := cmd.Flags().GetString("template-file")
	if err != nil {
		return fmt.Errorf("failed to get template-file flag: %w", err)
	}
	timezone, err := cmd.Flags().GetString("timezone")
	if err != nil {
		return fmt.Errorf("failed to get timezone flag: %w", err)
	}

	// Validate mutually exclusive template flags
	if templateStr != "" && templateFile != "" {
		return fmt.Errorf("cannot specify both --template and --template-file")
	}

	// Validate resource format if provided (should be in format "type/name")
	if resource != "" {
		parts := strings.Split(resource, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("resource format must be 'type/name' (e.g., 'deployment/myapp'), got: %s", resource)
		}
	}

	// Initialize runtime and Kubernetes client
	rt := runtime.FromRuntime(cmd.Context())
	if rt == nil {
		return fmt.Errorf("runtime not initialized")
	}

	// Create k8s client
	k8sClient, err := k8s.NewClientFromRuntime(cmd.Context(), rt)
	if err != nil {
		return fmt.Errorf("failed to create k8s client: %w", err)
	}

	// Build label selector using the new k8s package
	labelSelectorStr, err := k8s.BuildSelector(args[0], component, environment)
	if err != nil {
		return fmt.Errorf("failed to build label selector: %w", err)
	}

	// Convert string to labels.Selector for stern
	labelSelector, err := labels.Parse(labelSelectorStr)
	if err != nil {
		return fmt.Errorf("failed to parse label selector: %w", err)
	}

	containerState, err := stern.NewContainerState(stern.RUNNING)
	if err != nil {
		return fmt.Errorf("invalid container-state %q: %w", containerState, err)
	}

	funs := map[string]any{
		"color": func(color color.Color, text string) string {
			return color.SprintFunc()(text)
		},
	}

	// Determine template string to use
	var templateString string
	if templateFile != "" {
		// Read template from file
		templateBytes, err := os.ReadFile(templateFile)
		if err != nil {
			return fmt.Errorf("failed to read template file %q: %w", templateFile, err)
		}
		templateString = string(templateBytes)
	} else if templateStr != "" {
		// Use provided template string
		templateString = templateStr
	} else {
		// Use default template - show component extracted from pod name
		templateString = "{{color .PodColor (printf \"%s/%s\" (index .Labels \"deployah.dev/component\") (index .Labels \"deployah.dev/environment\"))}} [{{trunc -5 .PodName}}] {{.Message}}\n"
	}

	tmpl, err := template.New("logs").Funcs(funs).Funcs(sprig.FuncMap()).Parse(templateString)
	if err != nil {
		return fmt.Errorf("failed to parse log template: %w", err)
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil && timezone != "" {
		return fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}

	// Use Stern for real-time multi-pod/container log tailing via label/resource
	cfg := &stern.Config{
		Namespaces:          []string{rt.Namespace()},
		AllNamespaces:       false,
		EphemeralContainers: false,
		InitContainers:      false,
		Timestamps:          timestamps,
		Location:            loc,
		Since:               since,
		Template:            tmpl,
		LabelSelector:       labelSelector,
		FieldSelector:       fields.Everything(),
		ContainerStates:     []stern.ContainerState{containerState},
		Follow:              !noFollow,
		Resource:            resource,
		OnlyLogLines:        onlyLogLines,
		MaxLogRequests:      50,
		Stdin:               false,
		DiffContainer:       true,
		Out:                 cmd.OutOrStdout(),
		ErrOut:              cmd.ErrOrStderr(),
	}

	// Tail lines mapping (-1 means all)
	if tail >= 0 {
		t := tail
		cfg.TailLines = &t
	}

	// Restrict to a specific container if provided
	if container != "" {
		rx, err := regexp.Compile("^" + regexp.QuoteMeta(container) + "$")
		if err != nil {
			return fmt.Errorf("invalid container name '%s': %w", container, err)
		}
		cfg.ContainerQuery = rx
	} else {
		cfg.ContainerQuery = regexp.MustCompile(".*")
	}

	// Pod query must not be nil; default to match-all
	cfg.PodQuery = regexp.MustCompile(".*")

	if err := stern.Run(cmd.Context(), k8sClient.GetKubernetesClient(), cfg); err != nil {
		return fmt.Errorf("failed to stream logs for project %s: %w", args[0], err)
	}
	return nil
}
