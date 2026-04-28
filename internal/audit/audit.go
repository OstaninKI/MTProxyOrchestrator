package audit

import (
	"github.com/mtproto-orchestrator/mtproto-orchestrator/internal/db"
)

// Log writes one audit entry. adminID 0 means system action (no admin context).
// action is a short verb like "login", "logout", "user.create".
// target identifies the subject (e.g. user label). detail is extra safe context — no raw secrets.
func Log(d *db.DB, adminID int64, action, target, detail, ip string) error {
	var aid interface{}
	if adminID != 0 {
		aid = adminID
	}
	_, err := d.Exec(
		`INSERT INTO audit_log(admin_id, action, target, detail, ip) VALUES(?,?,?,?,?)`,
		aid, action, target, detail, ip,
	)
	return err
}
