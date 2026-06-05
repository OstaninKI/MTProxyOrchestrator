package nginx

import (
	"regexp"
	"strconv"
)

// Main nginx.conf tuning targets.
//
// The default Ubuntu nginx.conf ships with `worker_connections 768;` in the
// events{} block and no worker_rlimit_nofile, which floods error.log with
// "768 worker_connections are not enough" once the panel proxy and WebSocket
// log streaming push concurrent connections past that ceiling. Both directives
// live in contexts (events{} and the main context) that cannot be set from a
// sites-enabled/ or conf.d/ drop-in, so this project patches the distro-owned
// /etc/nginx/nginx.conf directly.
const (
	// MainConfWorkerConnections is the floor this project enforces for the
	// events{} worker_connections directive.
	MainConfWorkerConnections = 4096
	// MainConfWorkerRlimitNofile is the floor for the main-context
	// worker_rlimit_nofile directive so each worker can open enough file
	// descriptors for the raised connection count.
	MainConfWorkerRlimitNofile = 16384
	// mainConfMarker is appended to lines this project manages so operators
	// can see the change and so the patch stays idempotent on re-run.
	mainConfMarker = "# managed by tgproxy"
)

var (
	workerConnectionsRe = regexp.MustCompile(`(?m)^([ \t]*)worker_connections[ \t]+(\d+)[ \t]*;.*$`)
	workerRlimitRe      = regexp.MustCompile(`(?m)^([ \t]*)worker_rlimit_nofile[ \t]+(\d+)[ \t]*;.*$`)
	eventsOpenRe        = regexp.MustCompile(`(?m)^[ \t]*events[ \t]*\{[ \t]*$`)
	workerProcessesRe   = regexp.MustCompile(`(?m)^[ \t]*worker_processes[ \t]+\S+[ \t]*;.*$`)
)

// PatchMainConf raises worker_connections and worker_rlimit_nofile in an
// existing nginx.conf to the project's floors. It is idempotent and
// non-destructive:
//
//   - worker_connections is only raised when the current value is below the
//     floor; an operator-set higher value is left untouched. When the directive
//     is missing it is inserted into the events{} block.
//   - worker_rlimit_nofile is only raised when below the floor; when missing it
//     is inserted in the main context right after worker_processes.
//
// Managed lines are tagged with a marker comment. When nothing needs changing
// (already at/above the floors, or no recognizable events{} block), the input
// is returned unchanged so callers writing with writeFileIfChanged avoid a
// spurious nginx reload.
func PatchMainConf(existing []byte) []byte {
	s := string(existing)
	s = patchDirective(s, workerConnectionsRe, "worker_connections", MainConfWorkerConnections)
	if loc := workerConnectionsRe.FindStringIndex(s); loc == nil {
		// No worker_connections directive at all: add one inside events{}.
		if ev := eventsOpenRe.FindStringIndex(s); ev != nil {
			insert := "\n    worker_connections " + strconv.Itoa(MainConfWorkerConnections) + "; " + mainConfMarker
			s = s[:ev[1]] + insert + s[ev[1]:]
		}
	}

	s = patchDirective(s, workerRlimitRe, "worker_rlimit_nofile", MainConfWorkerRlimitNofile)
	if loc := workerRlimitRe.FindStringIndex(s); loc == nil {
		// No worker_rlimit_nofile: add it in the main context after worker_processes.
		if wp := workerProcessesRe.FindStringIndex(s); wp != nil {
			insert := "\nworker_rlimit_nofile " + strconv.Itoa(MainConfWorkerRlimitNofile) + "; " + mainConfMarker
			s = s[:wp[1]] + insert + s[wp[1]:]
		}
	}

	return []byte(s)
}

// patchDirective rewrites the first match of re to floor when the captured
// numeric value is below floor, preserving the original indentation and adding
// the marker comment. Values at or above floor are left untouched.
func patchDirective(s string, re *regexp.Regexp, name string, floor int) string {
	loc := re.FindStringSubmatchIndex(s)
	if loc == nil {
		return s
	}
	indent := s[loc[2]:loc[3]]
	val, err := strconv.Atoi(s[loc[4]:loc[5]])
	if err != nil || val >= floor {
		return s
	}
	replacement := indent + name + " " + strconv.Itoa(floor) + "; " + mainConfMarker
	return s[:loc[0]] + replacement + s[loc[1]:]
}
