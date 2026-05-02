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

// PanelProxyConfig holds fields for the TLS reverse proxy nginx server block
// that fronts the admin panel (tgproxy-panel) on a domain install.
type PanelProxyConfig struct {
	ListenPort  int    // public HTTPS panel port, e.g. 8443
	Domain      string // operator's domain name, e.g. proxy.example.com
	CertPath    string // path to the TLS certificate, e.g. /etc/lego/certificates/proxy.example.com.crt
	KeyPath     string // path to the TLS private key, e.g. /etc/lego/certificates/proxy.example.com.key
	BackendAddr string // panel backend address, e.g. 127.0.0.1:18080
}

// Render returns the nginx TLS reverse proxy server block as bytes.
func (c PanelProxyConfig) Render() []byte {
	var buf bytes.Buffer
	if err := panelProxyTmpl.Execute(&buf, c); err != nil {
		panic(fmt.Sprintf("nginx panel proxy config render: %v", err))
	}
	return buf.Bytes()
}

// mozillaIntermediateCiphers is the Mozilla Intermediate compatibility cipher list
// for nginx 1.18.0 on Ubuntu 22.04 (TLS 1.2 + 1.3).
const mozillaIntermediateCiphers = "ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384"

var panelProxyTmpl = template.Must(template.New("nginx-panel-proxy").Parse(`server {
    listen 0.0.0.0:{{.ListenPort}} ssl;
    server_name {{.Domain}};
    server_tokens off;

    ssl_certificate     {{.CertPath}};
    ssl_certificate_key {{.KeyPath}};
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305:DHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384;
    ssl_prefer_server_ciphers off;

    add_header Strict-Transport-Security "max-age=63072000; includeSubDomains; preload" always;
    add_header X-Frame-Options DENY always;
    add_header X-Content-Type-Options nosniff always;
    add_header Content-Security-Policy "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self' wss:; frame-ancestors 'none';" always;

    location / {
        proxy_pass http://{{.BackendAddr}};
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }
}
`))
