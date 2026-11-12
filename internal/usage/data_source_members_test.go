package usage

import (
	"fmt"
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
		{"u1", systemTenantID, "wen_guorong@hoperun.com", "文国荣", "active"},
		{"u2", systemTenantID, "yan_peng@hoperun.com", "闫鹏", "disabled"},
		{"u3", systemTenantID, "mac@hoperun.com", "Mac", "active"},
		// Another tenant, and a removed account still stored as active, must
		// stay out of the TokenHub member list.
		{"u4", "tenant-other", "other_user", "其他租户", "active"},
		{"u5", systemTenantID, "cong_yue", "丛悦", "active"},
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
	// mac, 文国荣, 闫鹏. 丛悦 and the other tenant stay out.
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(members) != 2 {
		t.Fatalf("page size 2 -> %d members", len(members))
	}
	// Ordered by username. mac@ sorts before wen_guorong@.
	if members[0].DisplayName != "Mac" || members[0].Username != "mac@hoperun.com" || members[0].ID != "u3" {
		t.Errorf("first member = %+v, want Mac", members[0])
	}
	if members[1].DisplayName != "文国荣" || members[1].Username != "wen_guorong@hoperun.com" {
		t.Errorf("second member = %+v, want 文国荣", members[1])
	}

	all, _, err := ListDataSourceMembers(1, 100)
	if err != nil {
		t.Fatalf("ListDataSourceMembers all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("all members = %d, want 3", len(all))
	}
	seen := map[string]DataSourceMember{}
	for _, m := range all {
		if _, ok := seen[m.DisplayName]; ok {
			t.Errorf("duplicate member %q", m.DisplayName)
		}
		seen[m.DisplayName] = m
		if m.DisplayName == "丛悦" || m.DisplayName == "其他租户" {
			t.Errorf("reported excluded member %+v", m)
		}
	}
	if got := seen["Mac"]; got.ID != "u3" || got.Status != "active" || got.Username != "mac@hoperun.com" {
		t.Errorf("Mac = %+v, want u3/active/mac@hoperun.com", got)
	}
	if got := seen["文国荣"]; got.ID != "u1" || got.Username != "wen_guorong@hoperun.com" {
		t.Errorf("文国荣 = %+v, want u1 and the mailbox username", got)
	}
	if got := seen["闫鹏"]; got.ID != "u2" || got.Status != "disabled" {
		t.Errorf("闫鹏 = %+v, want u2/disabled", got)
	}
}

func TestListDataSourceMembersReportsUsername(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	seedEndUsers(t)
	if _, err := usageDB.Exec(
		"INSERT INTO end_users (id, tenant_id, username, username_normalized, display_name, status) VALUES (?, ?, ?, ?, ?, ?)",
		"u-zhang", systemTenantID, "zhang_qian6@hoperun.com", "zhang_qian6@hoperun.com", "张倩", "active",
	); err != nil {
		t.Fatalf("insert 张倩: %v", err)
	}

	members, total, err := ListDataSourceMembers(1, 100)
	if err != nil {
		t.Fatalf("ListDataSourceMembers: %v", err)
	}
	if total != 4 {
		t.Errorf("total = %d, want 4", total)
	}
	var got []DataSourceMember
	for _, m := range members {
		if m.DisplayName == "张倩" {
			got = append(got, m)
		}
	}
	if len(got) != 1 || got[0].ID != "u-zhang" || got[0].Username != "zhang_qian6@hoperun.com" {
		t.Fatalf("张倩 rows = %+v, want username zhang_qian6@hoperun.com", got)
	}
}

func TestListDataSourceMembersClampsPageSize(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := usageDB.Exec(endUsersDDL); err != nil {
		t.Fatalf("create end_users: %v", err)
	}
	// Seed 501 rows directly (not via seedEndUsers) so the pageSize>500 clamp
	// is observable: page 2 is the rows past the first 500, not an empty page.
	const totalRows = 501
	for i := 1; i <= totalRows; i++ {
		username := fmt.Sprintf("user_%04d", i)
		if _, err := usageDB.Exec(
			"INSERT INTO end_users (id, tenant_id, username, username_normalized, display_name, status) VALUES (?, ?, ?, ?, ?, ?)",
			username, systemTenantID, username, username, username, "active",
		); err != nil {
			t.Fatalf("insert end_user %s: %v", username, err)
		}
	}

	members, total, err := ListDataSourceMembers(2, 9999)
	if err != nil {
		t.Fatalf("ListDataSourceMembers: %v", err)
	}
	if total != totalRows {
		t.Errorf("total = %d, want %d", total, totalRows)
	}
	// pageSize 9999 clamps to 500, so page 2 is the single row past user_0500.
	// Without the clamp the offset would be 9999 and the page would be empty.
	if len(members) != 1 {
		t.Fatalf("page 2 with clamped pageSize = %d members, want 1", len(members))
	}
	if members[0].DisplayName != "user_0501" || members[0].Username != "user_0501" {
		t.Errorf("page 2 first = %+v, want user_0501", members[0])
	}
}
