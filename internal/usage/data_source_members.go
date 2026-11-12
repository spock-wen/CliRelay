package usage

import (
	"fmt"
	"strings"
)

// DataSourceMember is one member row for the TokenHub data-source members
// endpoint. Rows are the default tenant's end_users. Username is the login
// name and, for this deployment, the hoperun mailbox TokenHub should receive.
type DataSourceMember struct {
	ID          string
	DisplayName string
	Username    string
	Status      string
}

// excludedDataSourceMemberNames are accounts removed from the roster. Their
// end_users rows may still be active, but TokenHub must not receive them.
var excludedDataSourceMemberNames = []string{
	"丛悦",
	"吕世祺",
	"张霖",
	"方浩",
	"皇甫秉顺",
	"虞瑞娴",
	"贺旺隆",
	"陈瑞",
	"饶明友",
	"高明",
}

// DataSourceExcludedMemberNames returns the display names omitted from the
// TokenHub member list and usage events.
func DataSourceExcludedMemberNames() []string {
	out := make([]string, len(excludedDataSourceMemberNames))
	copy(out, excludedDataSourceMemberNames)
	return out
}

// ListDataSourceMembers returns one page of members from the default (system)
// tenant, ordered by username_normalized, plus the total member count. Other
// tenants and removed accounts are omitted. It returns an empty slice (never
// nil) when no read database is available.
func ListDataSourceMembers(page, pageSize int) ([]DataSourceMember, int64, error) {
	db := getReadDB()
	if db == nil {
		return []DataSourceMember{}, 0, nil
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 100
	}
	if pageSize > 500 {
		pageSize = 500
	}

	// Blank names cannot be reported, and removed accounts must stay out.
	// Filter at the SQL layer so total matches the rows actually returned.
	source, sourceArgs := dataSourceMemberSource()

	var total int64
	if err := db.QueryRow("SELECT COUNT(*) FROM ("+source+") AS data_source_members", sourceArgs...).Scan(&total); err != nil {
		return []DataSourceMember{}, 0, fmt.Errorf("usage: count end_users: %w", err)
	}

	offset := (page - 1) * pageSize
	listArgs := make([]any, 0, len(sourceArgs)+2)
	listArgs = append(listArgs, sourceArgs...)
	listArgs = append(listArgs, pageSize, offset)
	rows, err := db.Query(
		"SELECT id, display_name, username, status FROM ("+source+") AS data_source_members ORDER BY username_normalized LIMIT ? OFFSET ?",
		listArgs...,
	)
	if err != nil {
		return []DataSourceMember{}, 0, fmt.Errorf("usage: list end_users: %w", err)
	}
	defer rows.Close()

	members := make([]DataSourceMember, 0, pageSize)
	for rows.Next() {
		var m DataSourceMember
		if err := rows.Scan(&m.ID, &m.DisplayName, &m.Username, &m.Status); err != nil {
			return []DataSourceMember{}, 0, fmt.Errorf("usage: scan end_user: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return []DataSourceMember{}, 0, fmt.Errorf("usage: iterate end_users: %w", err)
	}
	return members, total, nil
}

func dataSourceMemberFilter() (string, []any) {
	placeholders := make([]string, len(excludedDataSourceMemberNames))
	args := make([]any, 0, 1+len(excludedDataSourceMemberNames))
	args = append(args, systemTenantID)
	for i, name := range excludedDataSourceMemberNames {
		placeholders[i] = "?"
		args = append(args, name)
	}
	filter := " WHERE tenant_id = ? AND length(trim(display_name)) > 0 AND length(trim(username)) > 0 AND display_name NOT IN (" + strings.Join(placeholders, ",") + ")"
	return filter, args
}

// dataSourceMemberSource is the default-tenant end_users rows TokenHub reports.
// Username is selected as stored; callers report that value as the mailbox.
func dataSourceMemberSource() (string, []any) {
	filter, args := dataSourceMemberFilter()
	return "SELECT id, display_name, status, username, username_normalized FROM end_users" + filter, args
}

// DataSourceUsernamesByDisplayName maps a portal display name to its login
// username for the default tenant. TokenHub usage events store the key name,
// which matches display_name, and report the username as userEmail.
func DataSourceUsernamesByDisplayName() (map[string]string, error) {
	db := getReadDB()
	if db == nil {
		return map[string]string{}, nil
	}
	filter, args := dataSourceMemberFilter()
	rows, err := db.Query("SELECT display_name, username FROM end_users"+filter, args...)
	if err != nil {
		return nil, fmt.Errorf("usage: list data-source usernames: %w", err)
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var name, username string
		if err := rows.Scan(&name, &username); err != nil {
			return nil, fmt.Errorf("usage: scan data-source username: %w", err)
		}
		out[name] = username
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage: iterate data-source usernames: %w", err)
	}
	return out, nil
}
