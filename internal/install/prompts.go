package install

import (
	"github.com/charmbracelet/huh"
)

// Prompter abstracts interactive terminal prompts so install logic can be tested without a real TTY.
type Prompter interface {
	AskString(label, defaultVal string) (string, error)
	AskSelect(label string, options []string) (string, error)
	AskConfirm(label string, defaultVal bool) (bool, error)
}

// HuhPrompter is the production Prompter backed by charmbracelet/huh.
type HuhPrompter struct{}

func (HuhPrompter) AskString(label, defaultVal string) (string, error) {
	var result string
	err := huh.NewInput().
		Title(label).
		Value(&result).
		Placeholder(defaultVal).
		Run()
	if err != nil {
		return "", err
	}
	if result == "" {
		result = defaultVal
	}
	return result, nil
}

func (HuhPrompter) AskSelect(label string, options []string) (string, error) {
	var result string
	opts := make([]huh.Option[string], len(options))
	for i, o := range options {
		opts[i] = huh.NewOption(o, o)
	}
	err := huh.NewSelect[string]().
		Title(label).
		Options(opts...).
		Value(&result).
		Run()
	return result, err
}

func (HuhPrompter) AskConfirm(label string, defaultVal bool) (bool, error) {
	result := defaultVal
	err := huh.NewConfirm().
		Title(label).
		Value(&result).
		Run()
	return result, err
}
