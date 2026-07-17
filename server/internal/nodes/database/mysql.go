package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
	_ "github.com/go-sql-driver/mysql"
)

var (
	mysqlDBMu    sync.Mutex
	mysqlDBCache = make(map[string]*sql.DB)
	mysqlOpen    = func(dsn string) (*sql.DB, error) {
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			return nil, err
		}
		if err := db.Ping(); err != nil {
			return nil, fmt.Errorf("mysql: ping failed: %w", err)
		}
		return db, nil
	}
)

// MySQLNode returns the definition for the MySQL node.
func MySQLNode() schema.NodeDefinition {
	return schema.NodeDefinition{
		Type:        "database.mysql",
		Label:       "MySQL",
		Description: "Query and execute commands on a MySQL database.",
		Group:       "storage",
		Icon:        "Database",
		Inputs:      []schema.Port{{ID: "main"}},
		Outputs:     []schema.Port{{ID: "main", Label: "Results"}, {ID: "error", Label: "Error"}},
		Credentials: []string{"mysqlApi"},
		Params: []schema.ParamSchema{
			{Name: "credential", Label: "Credential", Type: "credential", Required: true, CredentialType: "mysqlApi"},
			{Name: "operation", Label: "Operation", Type: "select", Required: true, Default: "query:many", Options: []schema.ParamOption{
				{Label: "Query (multiple rows)", Value: "query:many"},
				{Label: "Query (single row)", Value: "query:one"},
				{Label: "Execute (Insert, Update, Delete)", Value: "exec"},
				{Label: "Execute Stored Procedure", Value: "storedProcedure:exec"},
				{Label: "Trigger: New Row", Value: "trigger:newRow"},
			}},
			{Name: "query", Label: "SQL Query", Type: "code:sql", Required: true,
				Description: "SQL statement. Use ? for parameterized queries.",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"query:many", "query:one", "exec"}}},
			{Name: "procedure", Label: "Stored Procedure Name", Type: "string", Required: true,
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"storedProcedure:exec"}}},
			{Name: "params", Label: "Parameters (JSON array)", Type: "json",
				Description: "Array of parameter values for ? placeholders."},
			{Name: "table", Label: "Table Name", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"trigger:newRow"}}},
			{Name: "idColumn", Label: "ID Column", Type: "string", Default: "id",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{"trigger:newRow"}}},
		},
		Execute: executeMySQL,
	}
}

// resolveMySQLDSN builds a MySQL DSN from the credential.
func resolveMySQLDSN(ctx *schema.ExecContext) (string, error) {
	if ctx.Credential != nil {
		cred, err := ctx.Credential("credential")
		if err != nil {
			return "", fmt.Errorf("mysql: failed to get credentials: %w", err)
		}
		if len(cred) > 0 {
			if dsn, ok := cred["dsn"].(string); ok && dsn != "" {
				return dsn, nil
			}
			host, _ := cred["host"].(string)
			port, _ := cred["port"].(string)
			user, _ := cred["user"].(string)
			password, _ := cred["password"].(string)
			database, _ := cred["database"].(string)
			if host != "" && database != "" {
				if port == "" {
					port = "3306"
				}
				auth := ""
				if user != "" {
					auth = user
					if password != "" {
						auth += ":" + password
					}
					auth += "@"
				}
				return fmt.Sprintf("%stcp(%s:%s)/%s?parseTime=true", auth, host, port, database), nil
			}
		}
	}
	return "", fmt.Errorf("mysql: no credential configured")
}

// getOrCreateMySQLDB returns a cached connection pool for the given DSN.
func getOrCreateMySQLDB(dsn string) (*sql.DB, error) {
	mysqlDBMu.Lock()
	if db, ok := mysqlDBCache[dsn]; ok {
		mysqlDBMu.Unlock()
		return db, nil
	}
	mysqlDBMu.Unlock()

	db, err := mysqlOpen(dsn)
	if err != nil {
		return nil, fmt.Errorf("mysql: failed to open: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	mysqlDBMu.Lock()
	defer mysqlDBMu.Unlock()
	if existing, ok := mysqlDBCache[dsn]; ok {
		db.Close()
		return existing, nil
	}
	mysqlDBCache[dsn] = db
	return db, nil
}

// executeMySQL is the execution function for the MySQL node.
func executeMySQL(ctx *schema.ExecContext) (schema.NodeResult, error) {
	dsn, err := resolveMySQLDSN(ctx)
	if err != nil {
		return schema.NodeResult{}, err
	}

	db, err := getOrCreateMySQLDB(dsn)
	if err != nil {
		return schema.NodeResult{}, err
	}

	operation, _ := ctx.Params["operation"].(string)
	execCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch operation {
	case "query:many":
		return mysqlQueryMany(execCtx, db, ctx)
	case "query:one":
		return mysqlQueryOne(execCtx, db, ctx)
	case "exec":
		return mysqlExec(execCtx, db, ctx)
	case "storedProcedure:exec":
		return mysqlStoredProc(execCtx, db, ctx)
	case "trigger:newRow":
		return mysqlTriggerNewRow(execCtx, db, ctx)
	default:
		return schema.NodeResult{}, fmt.Errorf("mysql: unknown operation %q", operation)
	}
}

// parseParams extracts an array of parameter values from the params input.
func parseParams(raw any) []any {
	switch v := raw.(type) {
	case []any:
		return v
	case string:
		var arr []any
		if json.Unmarshal([]byte(v), &arr) == nil {
			return arr
		}
	}
	return nil
}

// scanRow converts a sql.Rows row into a map[string]any.
func scanRow(rows *sql.Rows) (map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	values := make([]any, len(cols))
	scanArgs := make([]any, len(cols))
	for i := range values {
		scanArgs[i] = &values[i]
	}
	if err := rows.Scan(scanArgs...); err != nil {
		return nil, err
	}
	rowData := make(map[string]any, len(cols))
	for i, col := range cols {
		val := values[i]
		if b, ok := val.([]byte); ok {
			rowData[col] = string(b)
		} else {
			rowData[col] = val
		}
	}
	return rowData, nil
}

func mysqlQueryMany(ctx context.Context, db *sql.DB, execCtx *schema.ExecContext) (schema.NodeResult, error) {
	query, _ := execCtx.Params["query"].(string)
	if query == "" {
		return schema.NodeResult{}, fmt.Errorf("mysql: query is required")
	}
	params := parseParams(execCtx.RawParam("params"))

	rows, err := db.QueryContext(ctx, query, params...)
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("mysql query failed: %w", err)
	}
	defer rows.Close()

	var out []schema.Item
	for rows.Next() {
		rowData, err := scanRow(rows)
		if err != nil {
			continue
		}
		out = append(out, schema.Item{JSON: rowData})
	}
	if err := rows.Err(); err != nil {
		return schema.NodeResult{}, fmt.Errorf("mysql rows error: %w", err)
	}
	if out == nil {
		out = []schema.Item{}
	}
	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
}

func mysqlQueryOne(ctx context.Context, db *sql.DB, execCtx *schema.ExecContext) (schema.NodeResult, error) {
	query, _ := execCtx.Params["query"].(string)
	if query == "" {
		return schema.NodeResult{}, fmt.Errorf("mysql: query is required")
	}
	params := parseParams(execCtx.RawParam("params"))

	rows, err := db.QueryContext(ctx, query, params...)
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("mysql query failed: %w", err)
	}
	defer rows.Close()

	if rows.Next() {
		rowData, err := scanRow(rows)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("mysql row scan failed: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: rowData}}}}, nil
	}
	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {}}}, nil
}

func mysqlExec(ctx context.Context, db *sql.DB, execCtx *schema.ExecContext) (schema.NodeResult, error) {
	query, _ := execCtx.Params["query"].(string)
	if query == "" {
		return schema.NodeResult{}, fmt.Errorf("mysql: query is required")
	}
	params := parseParams(execCtx.RawParam("params"))

	result, err := db.ExecContext(ctx, query, params...)
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("mysql exec failed: %w", err)
	}
	rowsAffected, _ := result.RowsAffected()
	lastInsertID, _ := result.LastInsertId()
	out := []schema.Item{{JSON: map[string]any{
		"status":        "success",
		"rowsAffected":  rowsAffected,
		"lastInsertId":  lastInsertID,
	}}}
	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
}

// isValidMySQLIdentifier validates a MySQL identifier (table name, column name, procedure name)
// against allowed characters, max 64 chars.
func isValidMySQLIdentifier(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	re := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	return re.MatchString(name)
}

func mysqlStoredProc(ctx context.Context, db *sql.DB, execCtx *schema.ExecContext) (schema.NodeResult, error) {
	procedure, _ := execCtx.Params["procedure"].(string)
	if procedure == "" {
		return schema.NodeResult{}, fmt.Errorf("mysql: procedure name is required")
	}
	params := parseParams(execCtx.RawParam("params"))

	// Validate procedure name to prevent SQL injection
	if !isValidMySQLIdentifier(procedure) {
		return schema.NodeResult{}, fmt.Errorf("mysql: invalid procedure name %q", procedure)
	}

	// Build CALL statement with parameter placeholders
	placeholders := ""
	if len(params) > 0 {
		for i := range params {
			if i > 0 {
				placeholders += ", "
			}
			placeholders += "?"
		}
	}
	callSQL := fmt.Sprintf("CALL `%s`(%s)", procedure, placeholders)

	rows, err := db.QueryContext(ctx, callSQL, params...)
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("mysql stored procedure failed: %w", err)
	}
	defer rows.Close()

	var out []schema.Item
	for rows.Next() {
		rowData, err := scanRow(rows)
		if err != nil {
			continue
		}
		out = append(out, schema.Item{JSON: rowData})
	}
	if out == nil {
		out = []schema.Item{{JSON: map[string]any{"status": "success"}}}
	}
	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
}

func mysqlTriggerNewRow(ctx context.Context, db *sql.DB, execCtx *schema.ExecContext) (schema.NodeResult, error) {
	table, _ := execCtx.Params["table"].(string)
	if table == "" {
		return schema.NodeResult{}, fmt.Errorf("mysql trigger: table name is required")
	}
	idColumn, _ := execCtx.Params["idColumn"].(string)
	if idColumn == "" {
		idColumn = "id"
	}

	// Validate identifiers to prevent SQL injection
	if !isValidMySQLIdentifier(table) {
		return schema.NodeResult{}, fmt.Errorf("mysql trigger: invalid table name %q", table)
	}
	if !isValidMySQLIdentifier(idColumn) {
		return schema.NodeResult{}, fmt.Errorf("mysql trigger: invalid idColumn name %q", idColumn)
	}

	lastID, _ := execCtx.State["lastId"].(string)
	query := fmt.Sprintf("SELECT * FROM `%s`", table)
	if lastID != "" {
		query += fmt.Sprintf(" WHERE `%s` > ?", idColumn)
	}
	query += fmt.Sprintf(" ORDER BY `%s` ASC LIMIT 1", idColumn)

	var args []any
	if lastID != "" {
		args = append(args, lastID)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("mysql trigger poll failed: %w", err)
	}
	defer rows.Close()

	var out []schema.Item
	for rows.Next() {
		rowData, err := scanRow(rows)
		if err != nil {
			continue
		}
		out = append(out, schema.Item{JSON: rowData})
		// Store the last seen ID for next poll
		if v, ok := rowData[idColumn]; ok {
			execCtx.State["lastId"] = fmt.Sprintf("%v", v)
		}
	}

	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
}
