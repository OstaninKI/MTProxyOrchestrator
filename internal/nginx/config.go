package nginx

import (
	"bytes"
	"fmt"
	"text/template"
)

// StubConfig holds fields for the loopback stub nginx server block.
type StubConfig struct {
	ListenPort int    // typically 80, since 443 is taken by Teleproxy
	ServerName string // use "_" for catch-all
	StubRoot   string // e.g. /var/www/tgproxy-stub
}

// Render returns the nginx server block as bytes.
func (c StubConfig) Render() []byte {
	var buf bytes.Buffer
	if err := stubTmpl.Execute(&buf, c); err != nil {
		panic(fmt.Sprintf("nginx stub config render: %v", err))
	}
	return buf.Bytes()
}

var stubTmpl = template.Must(template.New("nginx-stub").Parse(`server {
    listen 127.0.0.1:{{.ListenPort}};
    server_name {{.ServerName}};
    server_tokens off;

    root {{.StubRoot}};
    index index.html;

    location / {
        try_files $uri $uri/ =404;
    }
}
`))
