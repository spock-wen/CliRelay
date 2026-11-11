package usage

import (
	"fmt"
	"strings"
)

// DataSourceMember is one member row for the TokenHub data-source members
// endpoint. Rows come from the default tenant's end_users, plus usage-only
// names that have request logs but no end_users row.
type DataSourceMember struct {
	ID          string
	DisplayName string
	Status      string
}

// usageOnlyDataSourceMember is a person reported from request logs who is not
// stored in end_users. username is the sort key, matching username_normalized.
type usageOnlyDataSourceMember struct {
	name     string
	username string
}

// usageOnlyDataSourceMembers are the usage-log names absent from end_users.
// TokenHub matches usage events to members by email, so these people have to
// appear on the member list as well. A name that later gains an end_users row
// is reported from that row instead of this list.
var usageOnlyDataSourceMembers = []usageOnlyDataSourceMember{
	{"蔡泽洲", "cai_zezhou"},
	{"陈浩熙", "chen_haoxi"},
	{"董晶晶", "dong_jingjing"},
	{"杜子龙", "du_zilong"},
	{"侯雨锋", "hou_yufeng"},
	{"李旺儒", "li_wangru"},
	{"齐云涛", "qi_yuntao"},
	{"田嵩伟", "tian_songwei"},
	{"王艺舟", "wang_yizhou"},
	{"熊林", "xiong_lin"},
	{"杨浩然", "yang_haoran"},
	{"杨嘉豪", "yang_jiahao"},
	{"张慧", "zhang_hui"},
	{"张倩", "zhang_qian"},
	{"张艺菲", "zhang_yifei"},
	{"赵晨", "zhao_chen"},
	{"赵静萱", "zhao_jingxuan"},
	{"周思源", "zhou_siyuan"},
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
// tenants and removed accounts are omitted. Usage-only names are included
// unless the default tenant already has that display name. It returns an
// empty slice (never nil) when no read database is available.
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

	// Blank display names cannot yield a member email, and removed accounts
	// must not be reported. Filter both at the SQL layer so total matches the
	// rows actually returned. Usage-only names share that query so pagination
	// stays aligned with total.
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
		"SELECT id, display_name, status FROM ("+source+") AS data_source_members ORDER BY username_normalized LIMIT ? OFFSET ?",
		listArgs...,
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

func dataSourceMemberFilter() (string, []any) {
	placeholders := make([]string, len(excludedDataSourceMemberNames))
	args := make([]any, 0, 1+len(excludedDataSourceMemberNames))
	args = append(args, systemTenantID)
	for i, name := range excludedDataSourceMemberNames {
		placeholders[i] = "?"
		args = append(args, name)
	}
	filter := " WHERE tenant_id = ? AND length(trim(display_name)) > 0 AND display_name NOT IN (" + strings.Join(placeholders, ",") + ")"
	return filter, args
}

// dataSourceMemberSource is the default-tenant end_users rows unioned with
// usage-only names that are not already present under that tenant.
func dataSourceMemberSource() (string, []any) {
	filter, filterArgs := dataSourceMemberFilter()
	excluded := make(map[string]struct{}, len(excludedDataSourceMemberNames))
	for _, name := range excludedDataSourceMemberNames {
		excluded[name] = struct{}{}
	}

	var b strings.Builder
	b.WriteString("SELECT id, display_name, status, username_normalized FROM end_users")
	b.WriteString(filter)
	args := make([]any, 0, len(filterArgs)+len(usageOnlyDataSourceMembers)*5)
	args = append(args, filterArgs...)
	for _, m := range usageOnlyDataSourceMembers {
		if _, skip := excluded[m.name]; skip || strings.TrimSpace(m.name) == "" {
			continue
		}
		b.WriteString(" UNION ALL SELECT ?, ?, 'active', ? WHERE NOT EXISTS (SELECT 1 FROM end_users WHERE tenant_id = ? AND display_name = ?)")
		args = append(args, "usage:"+m.username, m.name, m.username, systemTenantID, m.name)
	}
	return b.String(), args
}
