package install_test

import (
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/install"
)

// FakePrompter returns pre-configured answers without a terminal.
type FakePrompter struct {
	Strings  map[string]string
	Selects  map[string]string
	Confirms map[string]bool
}

func (f *FakePrompter) AskString(label, defaultVal string) (string, error) {
	if v, ok := f.Strings[label]; ok {
		return v, nil
	}
	return defaultVal, nil
}

func (f *FakePrompter) AskSelect(label string, options []string) (string, error) {
	if v, ok := f.Selects[label]; ok {
		return v, nil
	}
	return options[0], nil
}

func (f *FakePrompter) AskConfirm(label string, defaultVal bool) (bool, error) {
	if v, ok := f.Confirms[label]; ok {
		return v, nil
	}
	return defaultVal, nil
}

// FakePrompter satisfies the Prompter interface.
var _ install.Prompter = (*FakePrompter)(nil)

func TestFakePrompterReturnsConfiguredAnswers(t *testing.T) {
	p := &FakePrompter{
		Strings:  map[string]string{"Domain": "example.com"},
		Selects:  map[string]string{"Mode": "bridge"},
		Confirms: map[string]bool{"Enable TLS": true},
	}

	got, err := p.AskString("Domain", "localhost")
	if err != nil || got != "example.com" {
		t.Fatalf("AskString: got %q, err %v", got, err)
	}

	got, err = p.AskSelect("Mode", []string{"single", "bridge"})
	if err != nil || got != "bridge" {
		t.Fatalf("AskSelect: got %q, err %v", got, err)
	}

	ok, err := p.AskConfirm("Enable TLS", false)
	if err != nil || !ok {
		t.Fatalf("AskConfirm: got %v, err %v", ok, err)
	}
}

func TestFakePrompterFallsBackToDefaults(t *testing.T) {
	p := &FakePrompter{}

	got, _ := p.AskString("Domain", "localhost")
	if got != "localhost" {
		t.Fatalf("expected default %q, got %q", "localhost", got)
	}

	got, _ = p.AskSelect("Mode", []string{"single", "bridge"})
	if got != "single" {
		t.Fatalf("expected first option %q, got %q", "single", got)
	}

	ok, _ := p.AskConfirm("Enable TLS", false)
	if ok {
		t.Fatal("expected default false")
	}
}
