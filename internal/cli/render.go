package cli

import (
	"fmt"

	"nabat.dev/nabat"
)

// Render outputs data in the given format using Nabat structured output.
// headers and rows are used for table format; data is used for JSON and YAML.
func Render(c *nabat.Context, format string, headers []string, rows [][]string, data any) error {
	switch format {
	case OutputFormatTable:
		c.Table(headers, rows, nabat.WithTableBorder(nabat.BorderRounded()))
		return nil
	case OutputFormatJSON:
		return c.JSON(data)
	case OutputFormatYAML:
		return c.YAML(data)
	default:
		return fmt.Errorf("unsupported output format: %s", format)
	}
}
