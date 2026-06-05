package nginx_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/nginx"
)

func TestPatchMainConf_UbuntuDefaultGolden(t *testing.T) {
	in, err := os.ReadFile(filepath.Join("testdata", "nginx.conf.ubuntu-default"))
	if err != nil {
		t.Fatalf("read input: %v", err)
	}
	got := nginx.PatchMainConf(in)

	path := filepath.Join("testdata", "nginx.conf.patched")
	if updateGolden {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		t.Logf("updated golden file %s", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (set updateGolden=true to generate)", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("patch mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestPatchMainConf_Idempotent(t *testing.T) {
	in, err := os.ReadFile(filepath.Join("testdata", "nginx.conf.ubuntu-default"))
	if err != nil {
		t.Fatalf("read input: %v", err)
	}
	once := nginx.PatchMainConf(in)
	twice := nginx.PatchMainConf(once)
	if !bytes.Equal(once, twice) {
		t.Errorf("patch is not idempotent:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
}

func TestPatchMainConf_PreservesHigherOperatorValues(t *testing.T) {
	in := []byte("worker_processes auto;\nworker_rlimit_nofile 65535;\n\nevents {\n\tworker_connections 8192;\n}\n")
	got := nginx.PatchMainConf(in)
	if !bytes.Equal(got, in) {
		t.Errorf("operator values above the floor must be left untouched:\n--- in ---\n%s\n--- got ---\n%s", in, got)
	}
}

func TestPatchMainConf_RaisesLowOperatorValues(t *testing.T) {
	in := []byte("worker_processes auto;\nworker_rlimit_nofile 1024;\n\nevents {\n\tworker_connections 512;\n}\n")
	got := string(nginx.PatchMainConf(in))
	if !strings.Contains(got, "worker_connections 4096;") {
		t.Errorf("worker_connections below floor must be raised to 4096:\n%s", got)
	}
	if !strings.Contains(got, "worker_rlimit_nofile 16384;") {
		t.Errorf("worker_rlimit_nofile below floor must be raised to 16384:\n%s", got)
	}
}

func TestPatchMainConf_InsertsMissingDirectives(t *testing.T) {
	in := []byte("worker_processes auto;\n\nevents {\n}\n")
	got := string(nginx.PatchMainConf(in))
	if !strings.Contains(got, "worker_connections 4096;") {
		t.Errorf("missing worker_connections must be inserted into events{}:\n%s", got)
	}
	if !strings.Contains(got, "worker_rlimit_nofile 16384;") {
		t.Errorf("missing worker_rlimit_nofile must be inserted after worker_processes:\n%s", got)
	}
	// The inserted worker_connections must sit inside the events{} block.
	evIdx := strings.Index(got, "events {")
	wcIdx := strings.Index(got, "worker_connections")
	closeIdx := strings.Index(got, "}")
	if !(evIdx < wcIdx && wcIdx < closeIdx) {
		t.Errorf("worker_connections must be inserted inside events{}:\n%s", got)
	}
}

func TestPatchMainConf_NoEventsBlockLeavesConnectionsUnset(t *testing.T) {
	in := []byte("worker_processes auto;\n")
	got := string(nginx.PatchMainConf(in))
	if strings.Contains(got, "worker_connections") {
		t.Errorf("without an events{} block worker_connections must not be added:\n%s", got)
	}
	// worker_rlimit_nofile is a main-context directive and can still be added.
	if !strings.Contains(got, "worker_rlimit_nofile 16384;") {
		t.Errorf("worker_rlimit_nofile should be inserted after worker_processes:\n%s", got)
	}
}
