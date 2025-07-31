package helm

import (
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/cli"
)

type Client struct {
	settings *cli.EnvSettings
	config   *action.Configuration
}

func NewClient() (*Client, error) {
	settings := cli.New()
	config := new(action.Configuration)

	if err := config.Init(settings.RESTClientGetter(), settings.Namespace(), ""); err != nil {
		return nil, err
	}

	return &Client{
		settings: settings,
		config:   config,
	}, nil
}

func (c *Client) InstallApp(name, chart string, values map[string]interface{}) error {

	client := action.NewInstall(c.config)
	client.ReleaseName = name
	client.Namespace = c.settings.Namespace()
	client.CreateNamespace = true
	client.Timeout = 10 * time.Minute

	// Use Helmet's common library as default chart repository
	client.ChartPathOptions.RepoURL = "https://helm.github.io/helmet"

	return nil
}
