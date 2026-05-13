package panel

import (
	"net/http"
)

type auditEntry struct {
	ID        int64
	Action    string
	Target    string
	Detail    string
	IP        string
	CreatedAt string
}

func (s *Server) handleAuditLog(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(
		`SELECT id, action, target, detail, ip, created_at FROM audit_log ORDER BY id DESC LIMIT 200`,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var entries []auditEntry
	for rows.Next() {
		var e auditEntry
		if err := rows.Scan(&e.ID, &e.Action, &e.Target, &e.Detail, &e.IP, &e.CreatedAt); err != nil {
			continue
		}
		entries = append(entries, e)
	}

	tok, err := NewCSRFToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	SetCSRFCookie(w, tok, s.Secure, s.PanelPath)
	setStrictPanelCSP(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	auditPage(w, s.PanelPath, entries, tok)
}
