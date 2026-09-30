package dto

import "encoding/json"

// The admin audit view's cases (TKT-01M29CP2R4), registered into the shared
// corpus as admin_fixtures_test.go's are. The log is an admin's alone, so
// there is no other viewer class.

func init() {
	corpus["audit."+tAdmin] = auditPage()
	corpus["audit-filtered."+tAdmin] = auditFiltered()
}

// auditActions stands for audit.Actions, which the answer carries whole; a
// few are enough to pin the shape.
var auditActions = []string{"blob.deleted", "post.auto_hidden", "setting.changed", "thread.moved", "user.renamed",
	"user.suspended"}

// auditPage is the unfiltered first page: a moved thread, with both forums
// in related and no address, since the projection writer wrote it; the
// auto-hide, which no account did; a blob a hard redaction deleted, which
// has no page; and a setting an admin changed from a request. More rows
// follow.
func auditPage() AuditPage {
	return AuditPage{
		Entries: []AuditEntry{
			{
				ID: 412, At: 1757610000000, Actor: &bob, Action: "thread.moved",
				Target: AuditTarget{Type: "thread", ID: "3yymw47g00001", Label: "The dinner bill",
					Href: "/t/3yymw47g00001/the-dinner-bill"},
				Related: []AuditTarget{
					{Type: "forum", ID: "1", Label: "General", Href: "/f/general"},
					{Type: "forum", ID: "2", Label: "Money", Href: "/f/money"},
				},
				Details: json.RawMessage(`{"from_forum":1,"log_offset":4096,"thread":"3yymw47g00001","to_forum":2}`),
			},
			{
				ID: 411, At: 1757609000000, Actor: nil, Action: "post.auto_hidden",
				Target: AuditTarget{Type: "post", ID: "3yymw47g00001/63", Label: "The dinner bill #63",
					Href: "/t/3yymw47g00001/the-dinner-bill/63"},
				Related: []AuditTarget{},
				Details: json.RawMessage(`{"log_offset":3968,"thread":"3yymw47g00001","via":"thread-log"}`),
			},
			{
				ID: 410, At: 1757608000000, Actor: &bob, Action: "blob.deleted",
				Target: AuditTarget{Type: "blob",
					ID: "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", Label: "9f86d081884c"},
				Related: []AuditTarget{},
				Details: json.RawMessage(`{"journal":"redaction.journal","via":"cli"}`),
			},
			{
				ID: 409, At: 1757607000000, Actor: &bob, Action: "setting.changed",
				Target:  AuditTarget{Type: "setting", ID: "registration.mode", Label: "registration.mode", Href: "/admin/settings"},
				Related: []AuditTarget{},
				Details: json.RawMessage(`{"from":"open","to":"approval"}`),
				IP:      "203.0.113.7",
			},
		},
		Actions:    auditActions,
		NextCursor: "NDA5",
	}
}

// auditFiltered is ?actor=bob&action=user., the last page: alice's rename,
// and the suspension of an account the database no longer holds, which
// reads as a deleted member with no link.
func auditFiltered() AuditPage {
	return AuditPage{
		Entries: []AuditEntry{
			{
				ID: 388, At: 1757500000000, Actor: &bob, Action: "user.renamed",
				Target:  AuditTarget{Type: "user", ID: "17", Label: "alice", Href: "/u/alice"},
				Related: []AuditTarget{},
				Details: json.RawMessage(`{"from":"alice-","to":"alice"}`),
				IP:      "198.51.100.4",
			},
			{
				ID: 204, At: 1757400000000, Actor: &bob, Action: "user.suspended",
				Target:  AuditTarget{Type: "user", ID: "23", Label: "deleted-user-23"},
				Related: []AuditTarget{},
				Details: json.RawMessage(`{"from":"active","reason":"spam","until":null}`),
				IP:      "198.51.100.9",
			},
		},
		Actions: auditActions,
	}
}
