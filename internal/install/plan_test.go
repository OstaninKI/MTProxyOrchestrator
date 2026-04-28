package install_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/config"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/install"
)

func TestSinglePlanNoSingBox(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plan.Steps {
		if strings.Contains(s.Target, "sing-box") || strings.Contains(string(s.URL), "sing-box") {
			t.Errorf("Single plan must not reference sing-box, got step: %+v", s)
		}
	}
}

func TestSinglePlanHasTeleproxy(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plan.Steps {
		if s.Kind == install.StepDownloadBinary && strings.Contains(s.Target, "teleproxy") {
			return // found
		}
	}
	t.Error("Single plan must include a download-binary step for teleproxy")
}

func TestSinglePlanHasApt(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443)
	if err != nil {
		t.Fatal(err)
	}
	var aptTargets []string
	for _, s := range plan.Steps {
		if s.Kind == install.StepAptInstall {
			aptTargets = append(aptTargets, s.Target)
		}
	}
	if !slices.Contains(aptTargets, "nginx") {
		t.Error("Single plan must include apt-install for nginx")
	}
}
