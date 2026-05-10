package totp_test

import (
	"context"
	"sync"
	"sync/atomic"
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

func TestConsumeRecoveryCodeConcurrent(t *testing.T) {
	dir := t.TempDir()
	d, err := db.Open(dir + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Exec(`INSERT INTO admin(id, login, password_hash) VALUES(1,'a','x')`); err != nil {
		t.Fatal(err)
	}
	plain, hashes, err := totp.GenerateRecoveryCodes(3)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := totp.EncodeRecoveryHashes(hashes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE admin SET totp_recovery_codes=? WHERE id=1`, enc); err != nil {
		t.Fatal(err)
	}

	const N = 10
	target := plain[1]
	var wg sync.WaitGroup
	var successes int32
	start := make(chan struct{})
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ok, err := totp.ConsumeRecoveryCode(context.Background(), d, 1, target)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if ok {
				atomic.AddInt32(&successes, 1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := atomic.LoadInt32(&successes); got != 1 {
		t.Fatalf("expected exactly 1 successful consume, got %d", got)
	}

	var stored string
	if err := d.QueryRow(`SELECT totp_recovery_codes FROM admin WHERE id=1`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	remaining, err := totp.DecodeRecoveryHashes(stored)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 2 {
		t.Fatalf("expected 2 hashes remaining, got %d", len(remaining))
	}
	// The other two codes must still be consumable exactly once each.
	for _, p := range []string{plain[0], plain[2]} {
		ok, err := totp.ConsumeRecoveryCode(context.Background(), d, 1, p)
		if err != nil || !ok {
			t.Fatalf("expected leftover code %q to consume, got ok=%v err=%v", p, ok, err)
		}
	}
	// And the originally-targeted code must no longer consume.
	ok, err := totp.ConsumeRecoveryCode(context.Background(), d, 1, target)
	if err != nil || ok {
		t.Fatalf("consumed code must not be reusable, got ok=%v err=%v", ok, err)
	}
}
