package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// Color constants for consistent styling
const (
	ColorBrightCyan  = "14"
	ColorRed         = "9"
	ColorYellow      = "11"
	ColorGreen       = "10"
	ColorGray        = "7"
	ColorBrightGray  = "8"
	ColorBrightWhite = "15"
)

// Column represents a table column definition
type Column struct {
	Title     string
	Key       string
	Width     int
	MinWidth  int
	MaxWidth  int
	Truncate  bool
	StyleFunc func(value string) lipgloss.Style
	Condition bool
}

// Row represents a table row with data
type Row map[string]string

// Table represents a configurable table renderer
type Table struct {
	columns        []Column
	rows           []Row
	headerStyle    lipgloss.Style
	separatorStyle lipgloss.Style
	maxWidth       int
}

// NewTable creates a new table with default styling
func NewTable() *Table {
	return &Table{
		headerStyle:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorBrightCyan)).Padding(0, 1),
		separatorStyle: lipgloss.NewStyle().Foreground(lipgloss.Color(ColorBrightGray)),
		maxWidth:       getTerminalWidth(),
	}
}

// SetColumns sets the table columns
func (t *Table) SetColumns(columns []Column) *Table {
	t.columns = columns
	return t
}

// SetRows sets the table data
func (t *Table) SetRows(rows []Row) *Table {
	t.rows = rows
	return t
}

// SetHeaderStyle sets the header styling
func (t *Table) SetHeaderStyle(style lipgloss.Style) *Table {
	t.headerStyle = style
	return t
}

// SetSeparatorStyle sets the separator line styling
func (t *Table) SetSeparatorStyle(style lipgloss.Style) *Table {
	t.separatorStyle = style
	return t
}

// SetMaxWidth sets the maximum table width
func (t *Table) SetMaxWidth(width int) *Table {
	t.maxWidth = width
	return t
}

// getVisibleColumns returns only the columns that should be displayed
func (t *Table) getVisibleColumns() []Column {
	var visible []Column
	for _, col := range t.columns {
		if col.Condition {
			visible = append(visible, col)
		}
	}
	return visible
}

// calculateColumnWidths dynamically calculates optimal column widths
func (t *Table) calculateColumnWidths() []int {
	visibleColumns := t.getVisibleColumns()
	if len(visibleColumns) == 0 {
		return nil
	}

	widths := make([]int, len(visibleColumns))

	// Start with minimum widths (title length or specified min)
	for i, col := range visibleColumns {
		widths[i] = max(runewidth.StringWidth(col.Title), col.MinWidth)
		if col.Width > 0 {
			widths[i] = col.Width
		}
	}

	// Calculate data-driven widths
	for _, row := range t.rows {
		for i, col := range visibleColumns {
			if value, exists := row[col.Key]; exists {
				contentWidth := runewidth.StringWidth(value)
				if col.Width == 0 { // Only auto-size if width not fixed
					widths[i] = max(widths[i], contentWidth)
				}
			}
		}
	}

	// Apply max width constraints and distribute available space
	totalFixed := 0
	flexibleCols := 0

	for i, col := range visibleColumns {
		if col.MaxWidth > 0 && widths[i] > col.MaxWidth {
			widths[i] = col.MaxWidth
		}
		if col.Width > 0 {
			totalFixed += widths[i]
		} else {
			flexibleCols++
		}
	}

	// Distribute remaining space among flexible columns
	if flexibleCols > 0 && t.maxWidth > 0 {
		padding := len(visibleColumns) * 2 // 2 spaces padding per column
		availableSpace := t.maxWidth - totalFixed - padding
		if availableSpace > 0 {
			spacePerCol := availableSpace / flexibleCols
			for i, col := range visibleColumns {
				if col.Width == 0 {
					widths[i] = min(widths[i], spacePerCol)
				}
			}
		}
	}

	return widths
}

// Render renders the table as a string
func (t *Table) Render() string {
	visibleColumns := t.getVisibleColumns()
	if len(visibleColumns) == 0 {
		return ""
	}

	var sb strings.Builder
	widths := t.calculateColumnWidths()

	// Render header
	headerCells := make([]string, len(visibleColumns))
	for i, col := range visibleColumns {
		header := lipgloss.NewStyle().
			Width(widths[i]).
			MaxWidth(widths[i]).
			Inline(true).
			Render(truncateText(col.Title, widths[i]))
		headerCells[i] = t.headerStyle.Render(header)
	}
	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Left, headerCells...))
	sb.WriteString("\n")

	// Render separator line
	totalWidth := 0
	for _, width := range widths {
		totalWidth += width + 2 // +2 for padding
	}
	sb.WriteString(t.separatorStyle.Render(strings.Repeat("─", totalWidth)))
	sb.WriteString("\n")

	// Render data rows
	for _, row := range t.rows {
		cells := make([]string, len(visibleColumns))
		for i, col := range visibleColumns {
			value := row[col.Key]
			if value == "" {
				value = "-"
			}

			// Apply custom styling if provided
			var cellStyle lipgloss.Style
			if col.StyleFunc != nil {
				cellStyle = col.StyleFunc(value)
			} else {
				cellStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorBrightWhite))
			}

			// Truncate if needed
			cellContent := value
			if col.Truncate || len(value) > widths[i] {
				cellContent = truncateText(value, widths[i])
			}

			cell := cellStyle.
				Width(widths[i]).
				MaxWidth(widths[i]).
				Inline(true).
				Render(cellContent)
			cells[i] = lipgloss.NewStyle().Padding(0, 1).Render(cell)
		}
		sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Left, cells...))
		sb.WriteString("\n")
	}

	return sb.String()
}

// Print renders and prints the table
func (t *Table) Print() {
	fmt.Print(t.Render())
}

// truncateText truncates text with smart ellipsis
func truncateText(text string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if runewidth.StringWidth(text) <= maxWidth {
		return text
	}
	if maxWidth <= 3 {
		return strings.Repeat(".", maxWidth)
	}
	return runewidth.Truncate(text, maxWidth-1, "…")
}

// getTerminalWidth returns the current terminal width
func getTerminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 120 // fallback width
	}
	return width
}

// isTerminal checks if the output is going to a terminal
func IsTerminal() bool {
	fileInfo, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}

// Helper function for max
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Common style functions for reuse
func GetEnvironmentStyle(environment string) lipgloss.Style {
	env := strings.ToLower(environment)
	switch {
	case contains([]string{"prod", "production"}, env):
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorRed)).Bold(true)
	case contains([]string{"acc", "acceptance", "staging", "stg"}, env):
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorYellow)).Bold(true)
	case contains([]string{"dev", "development", "test", "testing", "qa", "review"}, env):
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGreen)).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGray))
	}
}

func GetStatusStyle(status string) lipgloss.Style {
	statusLower := strings.ToLower(status)
	switch {
	case contains([]string{v1.StatusDeployed.String(), v1.StatusSuperseded.String()}, statusLower):
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorGreen)).Bold(true)
	case contains([]string{v1.StatusPendingInstall.String(), v1.StatusPendingUpgrade.String(), v1.StatusPendingRollback.String(), v1.StatusUninstalling.String(), v1.StatusUnknown.String()}, statusLower):
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorYellow)).Bold(true)
	case contains([]string{v1.StatusFailed.String()}, statusLower):
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorRed)).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorBrightGray)).Bold(true)
	}
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
