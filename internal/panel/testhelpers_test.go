package panel_test

import (
	"testing"

	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}
