package logs

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"deployah.dev/deployah/internal/k8s"
	"deployah.dev/deployah/internal/runtime"
	"github.com/fatih/color"
	"github.com/stern/stern/stern"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"nabat.dev/nabat"
)

type Options struct {
	Project      string        `nabat:"project"`
	NoFollow     bool          `nabat:"no-follow"`
	Container    string        `nabat:"container"`
	Since        time.Duration `nabat:"since"`
	Tail         int64         `nabat:"tail"`
	Timestamps   bool          `nabat:"timestamps"`
	Resource     string        `nabat:"resource"`
	Component    string        `nabat:"component"`
	Environment  string        `nabat:"environment"`
	OnlyLogLines bool          `nabat:"only-log-lines"`
	Template     string        `nabat:"template"`
	TemplateFile string        `nabat:"template-file"`
	Timezone     string        `nabat:"timezone"`
}

func Register(app *nabat.App) {
	app.MustCommand("logs",
		nabat.WithDescription("View logs for a deployed project"),
		nabat.WithLongDescription("View logs from pods associated with a deployed project. This command connects to Kubernetes to stream logs from the pods."),
		nabat.WithArg("project", "", nabat.WithRequired(), nabat.WithUsage("Project name to view logs for"), nabat.WithPrompt("Project name", "", nabat.WithHint("e.g. my-app"))),
		nabat.WithFlag("no-follow", false, nabat.WithUsage("Do not follow log output")),
		nabat.WithFlag("container", "", nabat.WithUsage("Container name (if pod has multiple containers)")),
		nabat.WithFlag("since", 48*time.Hour, nabat.WithUsage("Show logs since duration (e.g., 10s, 1m, 1h)")),
		nabat.WithFlag("tail", int64(-1), nabat.WithUsage("Number of lines to show from the end of the logs (-1 shows all)")),
		nabat.WithFlag("timestamps", false, nabat.WithUsage("Include timestamps in log output")),
		nabat.WithFlag("resource", "", nabat.WithUsage("Kubernetes resource to tail (e.g., deployment/myapp)")),
		nabat.WithFlag("component", "", nabat.WithUsage("Filter by component name (e.g., api, web, worker)")),
		nabat.WithFlag("environment", "", nabat.WithShort('e'), nabat.WithUsage("Filter by environment name (e.g., dev, staging, prod)")),
		nabat.WithFlag("only-log-lines", false, nabat.WithUsage("Only output the log message lines (suppresses headers)")),
		nabat.WithFlag("template", "", nabat.WithUsage("Go template for each log line")),
		nabat.WithFlag("template-file", "", nabat.WithUsage("Path to a file containing a Go template for each log line")),
		nabat.WithFlag("timezone", time.Local.String(), nabat.WithUsage("Timezone for timestamps (e.g., Europe/Amsterdam)")),
		nabat.WithExample(`
# All pods in the "myproject" project
deployah logs myproject

# Only "api" component pods
deployah logs myproject --component=api

# Only "prod" environment pods
deployah logs myproject --environment=prod

# Custom log format
deployah logs myproject --template="{{.Message}}"

# Template from file
deployah logs myproject --template-file=log.tmpl

# Timezone
deployah logs myproject --timezone=Asia/Tehran`),
		nabat.WithRun(runLogs),
	)
}

func runLogs(c *nabat.Context) error {
	opts := &Options{}
	if err := c.Bind(opts); err != nil {
		return fmt.Errorf("binding options: %w", err)
	}

	if opts.Template != "" && opts.TemplateFile != "" {
		return fmt.Errorf("cannot specify both --template and --template-file")
	}

	if opts.Resource != "" {
		parts := strings.Split(opts.Resource, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("resource format must be 'type/name' (e.g., 'deployment/myapp'), got: %s", opts.Resource)
		}
	}

	rt := runtime.FromContext(c)

	k8sClient, err := k8s.NewClientFromRuntime(c, rt)
	if err != nil {
		return fmt.Errorf("k8s client: %w", err)
	}

	labelSelectorStr, err := k8s.BuildSelector(opts.Project, opts.Component, opts.Environment)
	if err != nil {
		return fmt.Errorf("build label selector: %w", err)
	}

	labelSelector, err := labels.Parse(labelSelectorStr)
	if err != nil {
		return fmt.Errorf("parse label selector: %w", err)
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

	var templateString string
	if opts.TemplateFile != "" {
		templateBytes, err := os.ReadFile(opts.TemplateFile)
		if err != nil {
			return fmt.Errorf("read template file %q: %w", opts.TemplateFile, err)
		}
		templateString = string(templateBytes)
	} else if opts.Template != "" {
		templateString = opts.Template
	} else {
		templateString = "{{color .PodColor (printf \"%s/%s\" (index .Labels \"deployah.dev/component\") (index .Labels \"deployah.dev/environment\"))}} [{{trunc -5 .PodName}}] {{.Message}}\n"
	}

	tmpl, err := template.New("logs").Funcs(funs).Funcs(sprig.FuncMap()).Parse(templateString)
	if err != nil {
		return fmt.Errorf("parse log template: %w", err)
	}

	loc, err := time.LoadLocation(opts.Timezone)
	if err != nil && opts.Timezone != "" {
		return fmt.Errorf("invalid timezone %q: %w", opts.Timezone, err)
	}

	cfg := &stern.Config{
		Namespaces:          []string{rt.Namespace()},
		AllNamespaces:       false,
		EphemeralContainers: false,
		InitContainers:      false,
		Timestamps:          opts.Timestamps,
		Location:            loc,
		Since:               opts.Since,
		Template:            tmpl,
		LabelSelector:       labelSelector,
		FieldSelector:       fields.Everything(),
		ContainerStates:     []stern.ContainerState{containerState},
		Follow:              !opts.NoFollow,
		Resource:            opts.Resource,
		OnlyLogLines:        opts.OnlyLogLines,
		MaxLogRequests:      50,
		Stdin:               false,
		DiffContainer:       true,
		Out:                 c.IO().Out,
		ErrOut:              c.IO().ErrOut,
	}

	if opts.Tail >= 0 {
		t := opts.Tail
		cfg.TailLines = &t
	}

	if opts.Container != "" {
		rx, err := regexp.Compile("^" + regexp.QuoteMeta(opts.Container) + "$")
		if err != nil {
			return fmt.Errorf("invalid container name '%s': %w", opts.Container, err)
		}
		cfg.ContainerQuery = rx
	} else {
		cfg.ContainerQuery = regexp.MustCompile(".*")
	}

	cfg.PodQuery = regexp.MustCompile(".*")

	if err := stern.Run(c, k8sClient.GetKubernetesClient(), cfg); err != nil {
		return fmt.Errorf("stream logs for project %s: %w", opts.Project, err)
	}
	return nil
}
