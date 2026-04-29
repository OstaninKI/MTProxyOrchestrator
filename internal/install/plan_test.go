package install_test

import (
	"encoding/hex"
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

func TestSinglePlanTeleproxyDownloadHasSHA256(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plan.Steps {
		if s.Kind != install.StepDownloadBinary || !strings.Contains(s.Target, "teleproxy") {
			continue
		}
		if len(s.SHA256) != 64 {
			t.Fatalf("teleproxy download SHA256 length: got %d, want 64", len(s.SHA256))
		}
		if _, err := hex.DecodeString(s.SHA256); err != nil {
			t.Fatalf("teleproxy download SHA256 must be hex: %v", err)
		}
		return
	}
	t.Fatal("Single plan must include a download-binary step for teleproxy")
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

func TestSinglePlanPanelUnitUsesGeneratedPath(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range plan.Steps {
		if s.Kind != install.StepWriteFile || s.Target != config.DefaultPaths().PanelService {
			continue
		}
		unit := string(s.Content)
		if !strings.Contains(unit, "tgproxy-panel serve") {
			t.Fatalf("panel unit must run serve subcommand:\n%s", unit)
		}
		if !strings.Contains(unit, "--path "+plan.Creds.PanelPath) {
			t.Fatalf("panel unit must use generated panel path %q:\n%s", plan.Creds.PanelPath, unit)
		}
		if !strings.Contains(unit, "--db "+config.DefaultPaths().PanelDB) {
			t.Fatalf("panel unit must use panel DB path:\n%s", unit)
		}
		if !strings.Contains(unit, "--listen 127.0.0.1:8443") {
			t.Fatalf("panel unit must use requested panel port:\n%s", unit)
		}
		if !strings.Contains(unit, "--mtproto-port 443") || !strings.Contains(unit, "--mask-host www.microsoft.com") || !strings.Contains(unit, "--stats-port 9091") {
			t.Fatalf("panel unit must pass teleproxy render settings:\n%s", unit)
		}
		return
	}
	t.Fatal("Single plan must write panel systemd unit")
}

func TestSinglePlanStartsPanelService(t *testing.T) {
	plan, err := install.BuildSinglePlan(config.Default(), config.DefaultPaths(), 8443)
	if err != nil {
		t.Fatal(err)
	}
	var enables, starts []string
	for _, s := range plan.Steps {
		switch s.Kind {
		case install.StepEnableService:
			enables = append(enables, s.Target)
		case install.StepStartService:
			starts = append(starts, s.Target)
		}
	}
	if !slices.Contains(enables, "tgproxy-panel") {
		t.Fatalf("Single plan must enable tgproxy-panel, got %v", enables)
	}
	if !slices.Contains(starts, "tgproxy-panel") {
		t.Fatalf("Single plan must start tgproxy-panel, got %v", starts)
	}
}
