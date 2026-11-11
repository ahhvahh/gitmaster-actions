package gitmasteractions

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
)

// LegacyRecord preserves one original row verbatim as typed JSON, including BLOBs.
// It does not silently pretend the old GitMaster schema is the current schema.
type LegacyRecord struct {
	ID         int64           `json:"id"`
	Source     string          `json:"source"`
	Table      string          `json:"table"`
	Key        string          `json:"key"`
	Data       json.RawMessage `json:"data"`
	ImportedAt string          `json:"imported_at"`
}

var legacyTables = []string{"action_runs", "action_jobs", "action_job_commands", "action_command_responses", "action_runtime_messages"}

func ensureLegacyArchive(db *sql.DB) error {
	for _, s := range []string{
		`CREATE TABLE IF NOT EXISTS legacy_archive(id INTEGER PRIMARY KEY AUTOINCREMENT,source TEXT NOT NULL,table_name TEXT NOT NULL,source_key TEXT NOT NULL,row_json BLOB NOT NULL,imported_at TEXT NOT NULL,UNIQUE(source,table_name,source_key))`,
		`CREATE INDEX IF NOT EXISTS idx_legacy_archive_lookup ON legacy_archive(source,table_name,id)`,
	} {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

// ImportLegacyHistory snapshots five Action-owned tables from a *read-only*
// legacy SQLite file into the independent actions.db without modifying source.
// A typed archive preserves columns that do not exist in the new data model.
func (e *Engine) ImportLegacyHistory(ctx context.Context, source string) (int, error) {
	if err := e.assertOpen(); err != nil {
		return 0, err
	}
	if !filepath.IsAbs(source) {
		return 0, errors.New("legacy SQLite path must be absolute")
	}
	original, err := filepath.Abs(source)
	if err != nil {
		return 0, err
	}
	target, err := filepath.Abs(e.options.DatabasePath)
	if err != nil {
		return 0, err
	}
	if original == target {
		return 0, errors.New("legacy database must not be the actions database")
	}
	info, err := os.Lstat(source)
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, errors.New("legacy SQLite must be regular file")
	}
	u := url.URL{Scheme: "file", Path: original, RawQuery: "mode=ro"}
	sourceDB, err := sql.Open("sqlite", u.String())
	if err != nil {
		return 0, err
	}
	defer sourceDB.Close()
	sourceDB.SetMaxOpenConns(1)
	read, err := sourceDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return 0, err
	}
	defer read.Rollback()
	written, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer written.Rollback()
	total := 0
	for _, table := range legacyTables {
		rows, err := read.QueryContext(ctx, "SELECT * FROM "+table)
		if err != nil {
			return total, fmt.Errorf("legacy table %s: %w", table, err)
		}
		names, err := rows.Columns()
		if err != nil {
			rows.Close()
			return total, err
		}
		kinds, err := rows.ColumnTypes()
		if err != nil {
			rows.Close()
			return total, err
		}
		primary := map[string]string{"action_runs": "action_run_id", "action_jobs": "id", "action_job_commands": "command_id", "action_command_responses": "id", "action_runtime_messages": "id"}[table]
		for rows.Next() {
			values := make([]any, len(names))
			ptrs := make([]any, len(names))
			for i := range ptrs {
				ptrs[i] = &values[i]
			}
			if err = rows.Scan(ptrs...); err != nil {
				break
			}
			record := map[string]any{}
			key := ""
			for i, name := range names {
				value := values[i]
				if blob, ok := value.([]byte); ok {
					// Every byte is retained even when a legacy column contains non-UTF8 data.
					value = map[string]any{"encoding": "base64", "value": base64.StdEncoding.EncodeToString(blob)}
				}
				record[name] = map[string]any{"sqlite_type": kinds[i].DatabaseTypeName(), "value": value}
				if name == primary {
					key = fmt.Sprint(values[i])
				}
			}
			if key == "" {
				err = fmt.Errorf("legacy %s row has no primary key", table)
				break
			}
			b, marshalErr := json.Marshal(record)
			if marshalErr != nil {
				err = marshalErr
				break
			}
			_, err = written.ExecContext(ctx, `INSERT INTO legacy_archive(source,table_name,source_key,row_json,imported_at) VALUES(?,?,?,?,?) ON CONFLICT(source,table_name,source_key) DO UPDATE SET row_json=excluded.row_json,imported_at=excluded.imported_at`, original, table, key, b, timestamp())
			if err != nil {
				break
			}
			total++
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return total, err
		}
	}
	if err = read.Commit(); err != nil {
		return total, err
	}
	if err = written.Commit(); err != nil {
		return total, err
	}
	return total, nil
}
func (e *Engine) ListLegacyRecords(ctx context.Context, source, table string, page Page) ([]LegacyRecord, error) {
	if err := e.assertOpen(); err != nil {
		return nil, err
	}
	allowed := false
	for _, name := range legacyTables {
		if name == table {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, errors.New("unknown legacy table")
	}
	limit, err := normalizePage(page)
	if err != nil {
		return nil, err
	}
	query := `SELECT id,source,table_name,source_key,row_json,imported_at FROM legacy_archive WHERE table_name=? AND id>?`
	args := []any{table, page.AfterID}
	if source != "" {
		query += " AND source=?"
		args = append(args, source)
	}
	query += " ORDER BY id LIMIT ?"
	args = append(args, limit)
	rows, err := e.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := []LegacyRecord{}
	for rows.Next() {
		var r LegacyRecord
		if err = rows.Scan(&r.ID, &r.Source, &r.Table, &r.Key, &r.Data, &r.ImportedAt); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// LegacyPrimaryKey can inspect the original typed value without SQL access.
func LegacyPrimaryKey(r LegacyRecord) string { return strconv.FormatInt(r.ID, 10) + ":" + r.Key }