package usage

import (
	"database/sql"
	"strings"

	sqlapikey "github.com/router-for-me/CLIProxyAPI/v6/internal/storage/sqlite/apikey"
	log "github.com/sirupsen/logrus"
)

type APIKeyIdentity struct {
	ID   string
	Key  string
	Name string
}

func ResolveAPIKeyIdentity(key string) *APIKeyIdentity {
	row := GetAPIKey(strings.TrimSpace(key))
	if row == nil || strings.TrimSpace(row.ID) == "" {
		return nil
	}
	return &APIKeyIdentity{
		ID:   strings.TrimSpace(row.ID),
		Key:  strings.TrimSpace(row.Key),
		Name: strings.TrimSpace(row.Name),
	}
}

func currentAPIKeyRowsByID() map[string]APIKeyRow {
	rows := ListAPIKeys()
	result := make(map[string]APIKeyRow, len(rows))
	for _, row := range rows {
		id := strings.TrimSpace(row.ID)
		if id == "" {
			continue
		}
		result[id] = row
	}
	return result
}

func uniqueAPIKeyIDByName() map[string]string {
	return uniqueAPIKeyIDByNameFromRows(ListAPIKeys())
}

func uniqueAPIKeyIDByNameFromDB(db *sql.DB) map[string]string {
	if db == nil {
		return nil
	}
	return uniqueAPIKeyIDByNameFromRows(sqlapikey.NewStore(db).List())
}

func uniqueAPIKeyIDByNameFromRows(rows []APIKeyRow) map[string]string {
	counts := make(map[string]int)
	ids := make(map[string]string)
	for _, row := range rows {
		id := strings.TrimSpace(row.ID)
		name := strings.ToLower(strings.TrimSpace(row.Name))
		if id == "" || name == "" {
			continue
		}
		counts[name]++
		ids[name] = id
	}

	result := make(map[string]string)
	for name, id := range ids {
		if counts[name] == 1 {
			result[name] = id
		}
	}
	return result
}

func backfillRequestLogAPIKeyIDs(db *sql.DB) {
	if db == nil {
		return
	}

	result, err := db.Exec(`
		UPDATE request_logs
		SET api_key_id = (
			SELECT id FROM api_keys WHERE api_keys.key = request_logs.api_key
		)
		WHERE trim(coalesce(api_key_id, '')) = ''
		  AND EXISTS (
			SELECT 1
			FROM api_keys
			WHERE api_keys.key = request_logs.api_key
			  AND trim(coalesce(api_keys.id, '')) <> ''
		  )
	`)
	if err != nil {
		log.Warnf("usage: backfill request_logs api_key_id by key failed: %v", err)
	} else if rows, rowsErr := result.RowsAffected(); rowsErr == nil && rows > 0 {
		log.Infof("usage: backfilled api_key_id for %d request_logs by exact key match", rows)
	}

	nameToID := uniqueAPIKeyIDByNameFromDB(db)
	if len(nameToID) == 0 {
		return
	}
	for lowerName, id := range nameToID {
		result, err := db.Exec(`
			UPDATE request_logs
			SET api_key_id = ?
			WHERE trim(coalesce(api_key_id, '')) = ''
			  AND lower(trim(coalesce(api_key_name, ''))) = ?
			  AND trim(coalesce(api_key, '')) <> ''
		`, id, lowerName)
		if err != nil {
			log.Warnf("usage: backfill request_logs api_key_id by name failed for %q: %v", lowerName, err)
			continue
		}
		if rows, rowsErr := result.RowsAffected(); rowsErr == nil && rows > 0 {
			log.Infof("usage: backfilled api_key_id for %d request_logs by unique api_key_name=%q", rows, lowerName)
		}
	}
}
