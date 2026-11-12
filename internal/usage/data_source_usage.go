package usage

import (
	"fmt"
	"time"
)

// DataSourceUsageRow is one successful request log whose api_key_name is a
// default-tenant portal display name. TokenHub reports that account's username.
type DataSourceUsageRow struct {
	Timestamp    time.Time
	APIKeyName   string
	Model        string
	Cost         float64
	InputTokens  int64
	OutputTokens int64
	CachedTokens int64
}

// QueryDataSourceUsageEvents returns successful request logs in the half-open
// [start, end) window for the default tenant. Removed accounts, blank key
// names, and other tenants are left out. Total is the matching row count.
//
// This is separate from QueryLogs. That query only has a relative day window
// and is shared with the management log page.
func QueryDataSourceUsageEvents(start, end time.Time, page, pageSize int) ([]DataSourceUsageRow, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 100
	}
	if pageSize > 500 {
		pageSize = 500
	}
	db := getReadDB()
	if db == nil {
		return []DataSourceUsageRow{}, 0, nil
	}

	where, args := dataSourceUsageWhere(start, end)
	var total int64
	if err := db.QueryRow("SELECT COUNT(*) FROM request_logs"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("usage: count data-source events: %w", err)
	}

	offset := (page - 1) * pageSize
	listArgs := make([]any, 0, len(args)+2)
	listArgs = append(listArgs, args...)
	listArgs = append(listArgs, pageSize, offset)
	rows, err := db.Query(
		"SELECT timestamp, api_key_name, model, cost, input_tokens, output_tokens, cached_tokens FROM request_logs"+
			where+" ORDER BY timestamp DESC, id DESC LIMIT ? OFFSET ?",
		listArgs...,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("usage: list data-source events: %w", err)
	}
	defer rows.Close()

	items := make([]DataSourceUsageRow, 0, pageSize)
	for rows.Next() {
		var row DataSourceUsageRow
		var ts storedTime
		if err := rows.Scan(&ts, &row.APIKeyName, &row.Model, &row.Cost, &row.InputTokens, &row.OutputTokens, &row.CachedTokens); err != nil {
			return nil, 0, fmt.Errorf("usage: scan data-source event: %w", err)
		}
		if ts.Valid {
			row.Timestamp = ts.Time
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("usage: iterate data-source events: %w", err)
	}
	return items, total, nil
}

func dataSourceUsageWhere(start, end time.Time) (string, []any) {
	filter, filterArgs := dataSourceMemberFilter()
	args := make([]any, 0, 3+len(filterArgs))
	args = append(args, systemTenantID, start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano))
	args = append(args, filterArgs...)
	where := " WHERE tenant_id = ? AND timestamp >= ? AND timestamp < ? AND failed = 0" +
		" AND trim(coalesce(api_key_name, '')) IN (SELECT display_name FROM end_users" + filter + ")"
	return where, args
}
