package usage

import "fmt"

// DataSourceMember is one member row read from end_users for the TokenHub
// data-source members endpoint.
type DataSourceMember struct {
	ID          string
	DisplayName string
	Status      string
}

// ListDataSourceMembers returns one page of members from the end_users table,
// ordered by username_normalized, plus the total member count. The data-source
// endpoint serves platform-wide data, so no tenant filter is applied. It
// returns an empty slice (never nil) when no read database is available.
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

	var total int64
	if err := db.QueryRow("SELECT COUNT(*) FROM end_users").Scan(&total); err != nil {
		return []DataSourceMember{}, 0, fmt.Errorf("usage: count end_users: %w", err)
	}

	offset := (page - 1) * pageSize
	rows, err := db.Query(
		"SELECT id, display_name, status FROM end_users ORDER BY username_normalized LIMIT ? OFFSET ?",
		pageSize, offset,
	)
	if err != nil {
		return []DataSourceMember{}, 0, fmt.Errorf("usage: list end_users: %w", err)
	}
	defer rows.Close()

	members := make([]DataSourceMember, 0, pageSize)
	for rows.Next() {
		var m DataSourceMember
		if err := rows.Scan(&m.ID, &m.DisplayName, &m.Status); err != nil {
			return []DataSourceMember{}, 0, fmt.Errorf("usage: scan end_user: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return []DataSourceMember{}, 0, fmt.Errorf("usage: iterate end_users: %w", err)
	}
	return members, total, nil
}
