// Package ui provides UI components for interactive flows
package ui

import (
	"fmt"

	"github.com/charmbracelet/huh"
)

// CreateInputGroup creates an input group for a form
func CreateInputGroup(title, placeholder, description string, validator func(string) error, value *string) *huh.Group {
	input := huh.NewInput().
		Title(title).
		Placeholder(placeholder).
		Description(description).
		Value(value)

	if validator != nil {
		input.Validate(validator)
	}

	return huh.NewGroup(input)
}

// CreateConfirmGroup creates a confirm group for a form
func CreateConfirmGroup(title, description, affirmative, negative string, value *bool) *huh.Group {
	return huh.NewGroup(
		huh.NewConfirm().
			Title(title).
			Description(description).
			Affirmative(affirmative).
			Negative(negative).
			Value(value),
	)
}

// CreateSelectGroup creates a select group for a form
func CreateSelectGroup(title, description string, options []huh.Option[string], value *string) *huh.Group {
	return huh.NewGroup(
		huh.NewSelect[string]().
			Title(title).
			Description(description).
			Options(options...).
			Value(value),
	)
}

// CreateNoteGroup creates a note group for a form
func CreateNoteGroup(title, description string) *huh.Group {
	return huh.NewGroup(
		huh.NewNote().
			Title(title).
			Description(description),
	)
}

// CreateInputForm creates an input form
func CreateInputForm(title, placeholder, description string, validator func(string) error, value *string) *huh.Form {
	return huh.NewForm(CreateInputGroup(title, placeholder, description, validator, value))
}

// CreateConfirmForm creates a confirm form
func CreateConfirmForm(title, description, affirmative, negative string, value *bool) *huh.Form {
	return huh.NewForm(CreateConfirmGroup(title, description, affirmative, negative, value))
}

// CreateSelectForm creates a select form
func CreateSelectForm(title, description string, options []huh.Option[string], value *string) *huh.Form {
	return huh.NewForm(CreateSelectGroup(title, description, options, value))
}

// CreateMultiSelectForm creates a multi-select form
func CreateMultiSelectForm(title, description string, options []huh.Option[string], value *[]string) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title(title).
				Description(description).
				Options(options...).
				Value(value),
		),
	)
}

// CreateNoteForm creates a note form
func CreateNoteForm(title, description string) *huh.Form {
	return huh.NewForm(CreateNoteGroup(title, description))
}

// CollectWithForm is a generic form collection helper to reduce code duplication
func CollectWithForm(form *huh.Form, errorMsg string) error {
	if err := form.Run(); err != nil {
		return fmt.Errorf("%s: %w", errorMsg, err)
	}
	return nil
}
