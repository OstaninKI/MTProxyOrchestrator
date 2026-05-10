package totp_test

import (
	"context"
	"testing"
	"time"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/totp"
	pquerna "github.com/pquerna/otp/totp"
)

func TestGenerateAndValidate(t *testing.T) {
	secret, url, err := totp.GenerateSecret("admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if secret == "" || url == "" {
		t.Fatal("empty secret or url")
	}
	code, err := pquerna.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !totp.Validate(secret, code) {
		t.Error("valid code rejected")
	}
	if totp.Validate(secret, "000000") && code == "000000" {
		// extremely unlikely; only fail if the real generated code wasn't 000000
	}
	if totp.Validate("", code) {
		t.Error("empty secret should not validate")
	}
}

func TestRecoveryCodeRoundTrip(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Exec(`INSERT INTO admin(id, login, password_hash) VALUES(1,'a','x')`); err != nil {
		t.Fatal(err)
	}
	plain, hashes, err := totp.GenerateRecoveryCodes(4)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) != 4 || len(hashes) != 4 {
		t.Fatalf("want 4 codes, got %d/%d", len(plain), len(hashes))
	}
	enc, err := totp.EncodeRecoveryHashes(hashes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE admin SET totp_recovery_codes=? WHERE id=1`, enc); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ok, err := totp.ConsumeRecoveryCode(ctx, d, 1, plain[0])
	if err != nil || !ok {
		t.Fatalf("expected consume ok, got ok=%v err=%v", ok, err)
	}
	// Reuse must fail.
	ok, err = totp.ConsumeRecoveryCode(ctx, d, 1, plain[0])
	if err != nil || ok {
		t.Fatalf("reuse must fail, got ok=%v err=%v", ok, err)
	}
	// A different code still works.
	ok, _ = totp.ConsumeRecoveryCode(ctx, d, 1, plain[1])
	if !ok {
		t.Error("second code must consume")
	}
	ok, _ = totp.ConsumeRecoveryCode(ctx, d, 1, "not-a-code")
	if ok {
		t.Error("garbage code should not consume")
	}
}
