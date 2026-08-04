package management

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/identity"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
)

// TokenHub 平台数据采集接口（成员列表 + 用量事件）。
// 鉴权由现有 h.Middleware() 处理（cps_* session token + RBAC，复用 request_logs.read）。

const (
	tokenHubEmailDomain       = "hihope.com"
	dataSourceDefaultPage     = 1
	dataSourceDefaultPageSize = 100
	dataSourceMaxPageSize     = 500
)

type dataSourceMember struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

type dataSourceMembersResponse struct {
	Members  []dataSourceMember `json:"members"`
	Total    int64              `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"pageSize"`
}

type dataSourceUsageEvent struct {
	Timestamp        time.Time `json:"timestamp"`
	UserEmail        string    `json:"userEmail"`
	Model            string    `json:"model"`
	Source           string    `json:"source"`
	Operation        string    `json:"operation"`
	Credits          int       `json:"credits"`
	Cost             float64   `json:"cost"`
	CostCurrency     string    `json:"costCurrency"`
	InputTokens      int64     `json:"inputTokens"`
	OutputTokens     int64     `json:"outputTokens"`
	CacheReadTokens  int64     `json:"cacheReadTokens"`
	CacheWriteTokens int64     `json:"cacheWriteTokens"`
}

type dataSourceUsageEventsResponse struct {
	Usages   []dataSourceUsageEvent `json:"usages"`
	Total    int64                  `json:"total"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"pageSize"`
}

// GetDataSourceMembers returns the platform member list for TokenHub polling.
func (h *Handler) GetDataSourceMembers(c *gin.Context) {
	page, pageSize := parseDataSourcePage(c)
	members, total, err := usage.ListDataSourceMembers(page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items := make([]dataSourceMember, 0, len(members))
	for _, m := range members {
		email := tokenHubEmailFor(m.DisplayName)
		if email == "" {
			continue
		}
		items = append(items, dataSourceMember{ID: m.ID, Email: email, Name: m.DisplayName, Role: "member", Status: m.Status})
	}
	c.JSON(http.StatusOK, dataSourceMembersResponse{
		Members:  items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// GetDataSourceUsageEvents returns usage events in the half-open [startDate,
// endDate) window, one event per request log row, mapped to the TokenHub
// usage-event shape. Rows without an api_key_name are skipped.
func (h *Handler) GetDataSourceUsageEvents(c *gin.Context) {
	start, end, ok := parseDataSourceTimeRange(c)
	if !ok {
		return
	}
	page, pageSize := parseDataSourcePage(c)

	result, err := usage.QueryLogs(usage.LogQueryParams{
		// Data-source serves platform-wide system-tenant request logs; the
		// TokenHub endpoint intentionally ignores the caller's effective tenant.
		TenantID:            identity.SystemTenantID,
		StartTime:           &start,
		EndTime:             &end,
		Page:                page,
		Size:                pageSize,
		SkipEmptyAPIKeyName: true,
		// Failed requests are not real usage; exclude them so TokenHub only
		// receives successful calls.
		Statuses: []string{"success"},
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	items := make([]dataSourceUsageEvent, 0, len(result.Items))
	for _, row := range result.Items {
		name := strings.TrimSpace(row.APIKeyName)
		if name == "" {
			continue
		}
		items = append(items, dataSourceUsageEvent{
			Timestamp:        row.Timestamp,
			UserEmail:        tokenHubEmailFor(name),
			Model:            row.Model,
			Source:           "CLI",
			Operation:        "Agent",
			Credits:          0,
			Cost:             row.Cost,
			CostCurrency:     "USD",
			InputTokens:      row.InputTokens,
			OutputTokens:     row.OutputTokens,
			CacheReadTokens:  row.CachedTokens,
			CacheWriteTokens: 0,
		})
	}
	c.JSON(http.StatusOK, dataSourceUsageEventsResponse{
		Usages:   items,
		Total:    result.Total,
		Page:     page,
		PageSize: pageSize,
	})
}

func tokenHubEmailFor(displayName string) string {
	if strings.TrimSpace(displayName) == "" {
		return ""
	}
	return util.NameToPinyin(displayName) + "@" + tokenHubEmailDomain
}

// parseDataSourceTimeRange parses the required ISO-8601 startDate/endDate pair
// and returns a 400 response (with ok=false) on any error. A literal '+' in a
// query string decodes to a space; replacing it back makes un-encoded timezone
// offsets (e.g. 2026-08-01T00:00:00+08:00) parse correctly.
func parseDataSourceTimeRange(c *gin.Context) (time.Time, time.Time, bool) {
	rawStart := strings.ReplaceAll(strings.TrimSpace(c.Query("startDate")), " ", "+")
	rawEnd := strings.ReplaceAll(strings.TrimSpace(c.Query("endDate")), " ", "+")
	if rawStart == "" || rawEnd == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "startDate and endDate are required"})
		return time.Time{}, time.Time{}, false
	}
	start, err := time.Parse(time.RFC3339, rawStart)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid startDate: expected ISO 8601 (RFC3339)"})
		return time.Time{}, time.Time{}, false
	}
	end, err := time.Parse(time.RFC3339, rawEnd)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid endDate: expected ISO 8601 (RFC3339)"})
		return time.Time{}, time.Time{}, false
	}
	if !end.After(start) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "endDate must be after startDate"})
		return time.Time{}, time.Time{}, false
	}
	return start, end, true
}

// parseDataSourcePage parses page/pageSize with data-source defaults and clamps
// pageSize to dataSourceMaxPageSize.
func parseDataSourcePage(c *gin.Context) (int, int) {
	page := parseQueryIntDefault(c.Query("page"), dataSourceDefaultPage)
	pageSize := parseQueryIntDefault(c.Query("pageSize"), dataSourceDefaultPageSize)
	if page < 1 {
		page = dataSourceDefaultPage
	}
	if pageSize < 1 {
		pageSize = dataSourceDefaultPageSize
	}
	if pageSize > dataSourceMaxPageSize {
		pageSize = dataSourceMaxPageSize
	}
	return page, pageSize
}

func parseQueryIntDefault(raw string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return def
	}
	return n
}
