package cmd

import (
	"fmt"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"regexp"
)

// NewInitCommand creates and returns a new cobra command for initializing Deployah configuration.
func NewInitCommand() *cobra.Command {
	initCommand := &cobra.Command{
		Use:   "init [flags]",
		Short: "Initialize Deployah configuration",
		RunE:  runInit,
	}

	initCommand.Flags().StringP("output", "o", ".deployah.yaml", "The output file path.")

	return initCommand
}

func runInit(_ *cobra.Command, _ []string) error {
	var name string
	regex := regexp.MustCompile("^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$")

	err := huh.NewInput().
		Title("Project name?").
		Placeholder("deployah-project").
		Description("What is the name of your project?").
		Validate(func(s string) error {
			if !regex.MatchString(s) {
				return fmt.Errorf("project name must be lowercase alphanumeric characters or dashes (-) separated and cannot start or end with a dash (-)")
			}
			return nil
		}).
		Value(&name).
		Run()

	if err != nil {
		return err
	}

	fmt.Printf("Hey, %s!\n", name)
	return nil
}
