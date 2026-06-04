package teleproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
)

// UserEntry holds a Teleproxy user label and their hex secret.
// The label is safe to log; the secret is not.
type UserEntry struct {
	Label  string `json:"label"`
	Secret string `json:"secret"` // hex-encoded, 32 chars
}

type usersFile struct {
	Users []UserEntry `json:"users"`
}

// MarshalUsersJSON serialises entries to the users.json format consumed by reconcile.
func MarshalUsersJSON(entries []UserEntry) ([]byte, error) {
	return json.Marshal(usersFile{Users: entries})
}

// UnmarshalUsersJSON parses the users.json format written by install and reloadTeleproxy.
func UnmarshalUsersJSON(data []byte) ([]UserEntry, error) {
	var uf usersFile
	if err := json.Unmarshal(data, &uf); err != nil {
		return nil, err
	}
	return uf.Users, nil
}

// Config holds all fields required to render teleproxy.toml.
// When SOCKS5Addr is empty the output is Single mode (direct to Telegram DCs).
// When SOCKS5Addr is set the output is Bridge mode (traffic via local sing-box).
type Config struct {
	Port         int
	MaskHost     string
	TLSBackend   string
	WildcardMask string
	StatsPort    int
	SOCKS5Addr   string
	MSSClamp     bool
	JA4Log       bool
	Users        []UserEntry
}

func (c Config) DomainName() string {
	if c.WildcardMask != "" {
		return c.WildcardMask
	}
	return c.MaskHost
}

// SOCKS5URL returns the SOCKS5 upstream value for the teleproxy `socks5`
// directive. Teleproxy requires a scheme; callers may pass a bare host:port,
// so the socks5:// scheme is added when absent. An already-schemed value
// (socks5:// or socks5h://) is returned unchanged. Empty input yields "".
func (c Config) SOCKS5URL() string {
	if c.SOCKS5Addr == "" {
		return ""
	}
	if strings.HasPrefix(c.SOCKS5Addr, "socks5://") || strings.HasPrefix(c.SOCKS5Addr, "socks5h://") {
		return c.SOCKS5Addr
	}
	return "socks5://" + c.SOCKS5Addr
}

// Render produces a deterministic teleproxy.toml byte slice.
// Precondition: all UserEntry.Label values must have passed secrets.ValidateUserLabel,
// and MaskHost/SOCKS5Addr must not contain quotes or newlines. Render trusts its caller.
func (c Config) Render() []byte {
	var buf bytes.Buffer
	if err := teleproxyTmpl.Execute(&buf, c); err != nil {
		panic(fmt.Sprintf("teleproxy config render: %v", err))
	}
	return buf.Bytes()
}

// teleproxyTmpl renders teleproxy.toml. Field names match Teleproxy v4.11.0.
// - direct=true: Direct-to-DC mode (no ME relay); valid with or without socks5.
// - socks5: optional Bridge-mode upstream added in v4.11.0; absent in Single mode.
var teleproxyTmpl = template.Must(template.New("teleproxy").Parse(`port = {{.Port}}
stats_port = {{.StatsPort}}
http_stats = true
direct = true
mss_clamp = {{.MSSClamp}}
{{- if .TLSBackend}}
domain = [{ name = "{{.DomainName}}", backend = "{{.TLSBackend}}" }]
{{- else}}
domain = "{{.DomainName}}"
{{- end}}
{{- if .SOCKS5Addr}}
socks5 = "{{.SOCKS5URL}}"
{{- end}}
{{- if .JA4Log}}

[stats]
ja4_log = true
{{- end}}
{{range .Users}}
[[secret]]
key = "{{.Secret}}"
label = "{{.Label}}"
{{end}}`))
