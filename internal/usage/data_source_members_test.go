package usage

import (
	"testing"
)

// endUsersDDL mirrors the production end_users table (subset of columns the
// data-source endpoint reads). It is created per-test because it is not part
// of the bootstrapped SQLite schema.
const endUsersDDL = `
CREATE TABLE end_users (
  id                  TEXT PRIMARY KEY,
  tenant_id           TEXT NOT NULL,
  username            TEXT NOT NULL,
  username_normalized TEXT NOT NULL UNIQUE,
  display_name        TEXT NOT NULL,
  status              TEXT NOT NULL DEFAULT 'active',
  password_hash       TEXT NOT NULL DEFAULT ''
);
`

func seedEndUsers(t *testing.T) {
	t.Helper()
	if _, err := usageDB.Exec(endUsersDDL); err != nil {
		t.Fatalf("create end_users: %v", err)
	}
	rows := []struct {
		id, tenant, username, display, status string
	}{
		{"u1", systemTenantID, "wen_guorong", "文国荣", "active"},
		{"u2", systemTenantID, "yan_peng", "闫鹏", "disabled"},
		{"u3", systemTenantID, "mac", "Mac", "active"},
	}
	for _, r := range rows {
		if _, err := usageDB.Exec(
			"INSERT INTO end_users (id, tenant_id, username, username_normalized, display_name, status) VALUES (?, ?, ?, ?, ?, ?)",
			r.id, r.tenant, r.username, r.username, r.display, r.status,
		); err != nil {
			t.Fatalf("insert end_user %s: %v", r.username, err)
		}
	}
}

func TestListDataSourceMembers(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	seedEndUsers(t)

	members, total, err := ListDataSourceMembers(1, 2)
	if err != nil {
		t.Fatalf("ListDataSourceMembers: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(members) != 2 {
		t.Fatalf("page size 2 -> %d members", len(members))
	}
	// Ordered by username_normalized: mac, wen_guorong, yan_peng.
	if members[0].DisplayName != "Mac" || members[0].ID != "u3" || members[0].Status != "active" {
		t.Errorf("first member = %+v, want Mac/u3/active", members[0])
	}
	if members[1].DisplayName != "文国荣" {
		t.Errorf("second member = %+v, want 文国荣", members[1])
	}

	page2, _, err := ListDataSourceMembers(2, 2)
	if err != nil {
		t.Fatalf("ListDataSourceMembers page 2: %v", err)
	}
	if len(page2) != 1 || page2[0].DisplayName != "闫鹏" || page2[0].Status != "disabled" {
		t.Errorf("page 2 = %+v, want [闫鹏/disabled]", page2)
	}
}

func TestListDataSourceMembersClampsPageSize(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	seedEndUsers(t)

	_, total, err := ListDataSourceMembers(1, 9999)
	if err != nil {
		t.Fatalf("ListDataSourceMembers: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
}
