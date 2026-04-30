package install_test

import (
	"os"
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/install"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/panel"
)

func TestBootstrapPanelDB(t *testing.T) {
	path := t.TempDir() + "/panel.db"
	bootstrap := install.PanelBootstrap{
		AdminLogin:    "admin123",
		AdminPassword: "Password123456",
		UserLabel:     "user1",
		UserSecretHex: "00112233445566778899aabbccddeeff",
	}

	if err := install.BootstrapPanelDB(path, bootstrap); err != nil {
		t.Fatal(err)
	}

	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	var login, hash string
	if err := d.QueryRow(`SELECT login, password_hash FROM admin WHERE id=1`).Scan(&login, &hash); err != nil {
		t.Fatal(err)
	}
	if login != bootstrap.AdminLogin {
		t.Fatalf("admin login: got %s want %s", login, bootstrap.AdminLogin)
	}
	if !panel.CheckPassword(hash, bootstrap.AdminPassword) {
		t.Fatal("stored admin password hash does not match bootstrap password")
	}

	var label, secret string
	if err := d.QueryRow(`SELECT label, secret_hex FROM users WHERE deleted_at IS NULL`).Scan(&label, &secret); err != nil {
		t.Fatal(err)
	}
	if label != bootstrap.UserLabel {
		t.Fatalf("user label: got %s want %s", label, bootstrap.UserLabel)
	}
	if secret != bootstrap.UserSecretHex {
		t.Fatalf("user secret: got %s want %s", secret, bootstrap.UserSecretHex)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("panel.db mode: got %o want 600", info.Mode().Perm())
	}
}
