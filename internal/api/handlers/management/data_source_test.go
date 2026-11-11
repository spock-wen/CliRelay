package management

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	_ "modernc.org/sqlite"
)

// testEndUsersDDL mirrors the production end_users table subset. end_users is
// not part of the bootstrapped SQLite schema, so handler tests create it via a
// second connection to the same SQLite file.
const testEndUsersDDL = `
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

func newDataSourceTestEnv(t *testing.T) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "usage.db")
	if err := usage.InitDB(dbPath, config.RequestLogStorageConfig{}, time.UTC); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() {
		usage.CloseDB()
		_ = os.Remove(dbPath)
		_ = os.Remove(dbPath + "-wal")
		_ = os.Remove(dbPath + "-shm")
	})
	return dbPath
}

func seedEndUsersDirect(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(testEndUsersDDL); err != nil {
		t.Fatalf("create end_users: %v", err)
	}
	rows := []struct {
		id, tenant, username, display, status string
	}{
		{"u1", "00000000-0000-0000-0000-000000000001", "wen_guorong", "文国荣", "active"},
		{"u2", "00000000-0000-0000-0000-000000000001", "yan_peng", "闫鹏", "disabled"},
		{"u3", "00000000-0000-0000-0000-000000000001", "mac", "Mac", "active"},
		// Blank display_name cannot yield a member email; it must be filtered at
		// the SQL layer so total stays consistent with returned rows.
		{"u4", "00000000-0000-0000-0000-000000000001", "empty_display", "", "active"},
	}
	for _, r := range rows {
		if _, err := db.Exec(
			"INSERT INTO end_users (id, tenant_id, username, username_normalized, display_name, status) VALUES (?, ?, ?, ?, ?, ?)",
			r.id, r.tenant, r.username, r.username, r.display, r.status,
		); err != nil {
			t.Fatalf("insert end_user %s: %v", r.username, err)
		}
	}
}

func TestGetDataSourceMembers(t *testing.T) {
	dbPath := newDataSourceTestEnv(t)
	seedEndUsersDirect(t, dbPath)

	h := &Handler{cfg: &config.Config{}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/data-source/members?page=1&pageSize=100", nil)
	h.GetDataSourceMembers(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		Members []struct {
			ID     string `json:"id"`
			Email  string `json:"email"`
			Name   string `json:"name"`
			Role   string `json:"role"`
			Status string `json:"status"`
		} `json:"members"`
		Total    int64 `json:"total"`
		Page     int   `json:"page"`
		PageSize int   `json:"pageSize"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Total != 21 || payload.Page != 1 || payload.PageSize != 100 {
		t.Errorf("paging = total=%d page=%d size=%d, want 21/1/100", payload.Total, payload.Page, payload.PageSize)
	}
	if len(payload.Members) != 21 {
		t.Fatalf("members len = %d, want 21; body=%s", len(payload.Members), rec.Body.String())
	}
	byName := map[string]int{}
	for i, m := range payload.Members {
		byName[m.Name] = i
	}
	member := func(name string) (id, email, role, status string, ok bool) {
		i, ok := byName[name]
		if !ok {
			return "", "", "", "", false
		}
		m := payload.Members[i]
		return m.ID, m.Email, m.Role, m.Status, true
	}
	_, macEmail, macRole, macStatus, macOK := member("Mac")
	if !macOK || macEmail != "mac@hoperun.com" || macRole != "member" || macStatus != "active" {
		t.Errorf("Mac email=%q role=%q status=%q ok=%v", macEmail, macRole, macStatus, macOK)
	}
	_, wenEmail, _, _, wenOK := member("文国荣")
	if !wenOK || wenEmail != "wen_guorong@hoperun.com" {
		t.Errorf("文国荣 email=%q ok=%v", wenEmail, wenOK)
	}
	zhangID, zhangEmail, zhangRole, _, zhangOK := member("张倩")
	if !zhangOK || zhangEmail != "zhang_qian6@hoperun.com" || zhangID != "usage:zhang_qian" || zhangRole != "member" {
		t.Errorf("张倩 id=%q email=%q role=%q ok=%v", zhangID, zhangEmail, zhangRole, zhangOK)
	}
}

func TestGetDataSourceUsageEvents(t *testing.T) {
	newDataSourceTestEnv(t)

	now := time.Now().UTC()
	usage.InsertLog(
		"sk-1", "文国荣", "deepseek-v4-flash", "cli", "", "",
		false, now, 100, 50,
		usage.TokenStats{InputTokens: 100, OutputTokens: 20, CachedTokens: 30, TotalTokens: 150},
		"", "",
	)
	// Empty api_key_name: counted in total but skipped in output.
	usage.InsertLog(
		"sk-2", "", "gpt-5", "cli", "", "",
		false, now.Add(-time.Hour), 200, 100,
		usage.TokenStats{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
		"", "",
	)
	// Outside the window: excluded entirely.
	usage.InsertLog(
		"sk-3", "闫鹏", "gemini-2.5", "cli", "", "",
		false, now.Add(-72*time.Hour), 300, 150,
		usage.TokenStats{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
		"", "",
	)
	// Failed request inside the window: must be excluded (only successful calls
	// are reported to TokenHub).
	usage.InsertLog(
		"sk-4", "方珍", "glm-5.2", "cli", "", "",
		true, now.Add(-2*time.Hour), 300, 150,
		usage.TokenStats{InputTokens: 50, OutputTokens: 10, TotalTokens: 60},
		"", "",
	)
	// Removed account inside the window: must not be reported.
	usage.InsertLog(
		"sk-5", "丛悦", "glm-5.2", "cli", "", "",
		false, now.Add(-time.Minute), 10, 5,
		usage.TokenStats{InputTokens: 1, OutputTokens: 1, TotalTokens: 2},
		"", "",
	)

	start := now.Add(-24 * time.Hour).Format(time.RFC3339)
	end := now.Add(time.Hour).Format(time.RFC3339)

	h := &Handler{cfg: &config.Config{}}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/data-source/usage-events?startDate="+start+"&endDate="+end, nil)
	h.GetDataSourceUsageEvents(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var payload struct {
		Usages []struct {
			UserEmail        string `json:"userEmail"`
			Model            string `json:"model"`
			Source           string `json:"source"`
			Operation        string `json:"operation"`
			Credits          int    `json:"credits"`
			CostCurrency     string `json:"costCurrency"`
			InputTokens      int64  `json:"inputTokens"`
			OutputTokens     int64  `json:"outputTokens"`
			CacheReadTokens  int64  `json:"cacheReadTokens"`
			CacheWriteTokens int64  `json:"cacheWriteTokens"`
		} `json:"usages"`
		Total    int64 `json:"total"`
		Page     int   `json:"page"`
		PageSize int   `json:"pageSize"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Window matches 文国荣, an empty name, and removed account 丛悦. The latter
	// two are excluded at the SQL layer so total matches the returned rows.
	if payload.Total != 1 {
		t.Fatalf("total = %d, want 1; body=%s", payload.Total, rec.Body.String())
	}
	if len(payload.Usages) != 1 {
		t.Fatalf("usages len = %d, want 1; body=%s", len(payload.Usages), rec.Body.String())
	}
	first := payload.Usages[0]
	if first.UserEmail != "wen_guorong@hoperun.com" {
		t.Errorf("userEmail = %q, want wen_guorong@hoperun.com", first.UserEmail)
	}
	if first.Source != "CLI" || first.Operation != "Agent" || first.Credits != 0 || first.CostCurrency != "USD" {
		t.Errorf("fixed fields = %+v", first)
	}
	if first.InputTokens != 100 || first.OutputTokens != 20 || first.CacheReadTokens != 30 || first.CacheWriteTokens != 0 {
		t.Errorf("token fields = %+v", first)
	}
	if first.Model != "deepseek-v4-flash" {
		t.Errorf("model = %q", first.Model)
	}
}

func TestTokenHubEmailFor(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"文国荣", "wen_guorong@hoperun.com"},
		{"闫鹏", "yan_peng1@hoperun.com"},
		{"单文泽", "shan_wenze@hoperun.com"},
		{"汪伟", "wang_wei10@hoperun.com"},
		{"王威", "wang_wei02@hoperun.com"},
		{"王永海", "wang_yonghai@runkaihong.com.cn"},
		{"  付思丽  ", "fu_sili1@hoperun.com"},
		{"Mac", "mac@hoperun.com"},
		{"测试用户", "ce_shiyonghu@hoperun.com"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := tokenHubEmailFor(tc.name); got != tc.want {
			t.Errorf("tokenHubEmailFor(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestGetDataSourceUsageEventsParsesUnencodedTimezoneOffset(t *testing.T) {
	newDataSourceTestEnv(t)
	// A literal + in a query string decodes to a space; the handler must recover it.
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet,
		"/data-source/usage-events?startDate=2026-08-01T00:00:00+08:00&endDate=2026-08-02T00:00:00+08:00", nil)
	(&Handler{cfg: &config.Config{}}).GetDataSourceUsageEvents(c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetDataSourceUsageEventsRejectsInvalidWindow(t *testing.T) {
	newDataSourceTestEnv(t)
	paths := []string{
		"/data-source/usage-events",
		"/data-source/usage-events?startDate=2026-08-01T00:00:00Z",
		"/data-source/usage-events?startDate=notadate&endDate=2026-08-02T00:00:00Z",
		"/data-source/usage-events?startDate=2026-08-02T00:00:00Z&endDate=2026-08-01T00:00:00Z",
	}
	for _, path := range paths {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, path, nil)
		(&Handler{cfg: &config.Config{}}).GetDataSourceUsageEvents(c)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d, want 400; body=%s", path, rec.Code, rec.Body.String())
		}
	}
}
