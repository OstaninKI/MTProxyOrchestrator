package teleproxy

import (
	"bytes"
	"fmt"
	"text/template"
)

// UserEntry holds a Teleproxy user label and their hex secret.
// The label is safe to log; the secret is not.
type UserEntry struct {
	Label  string
	Secret string // hex-encoded, 32 chars
}

// Config holds all fields required to render teleproxy.toml.
// When SOCKS5Addr is empty the output is Single mode (direct to Telegram DCs).
// When SOCKS5Addr is set the output is Bridge mode (traffic via local sing-box).
type Config struct {
	Port       int
	MaskHost   string
	StatsPort  int
	SOCKS5Addr string
	Users      []UserEntry
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
domain = "{{.MaskHost}}"
{{- if .SOCKS5Addr}}
socks5 = "{{.SOCKS5Addr}}"
{{- end}}
{{range .Users}}
[[secret]]
key = "{{.Secret}}"
label = "{{.Label}}"
{{end}}`))
