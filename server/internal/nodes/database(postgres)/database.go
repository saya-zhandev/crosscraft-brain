package database

import (
	"context"
	"fmt"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresNode returns the definition for the PostgreSQL node.
func PostgresNode() schema.NodeDefinition {
	return schema.NodeDefinition{
		Type:        "database.postgres",
		Label:       "PostgreSQL",
		Description: "Query or execute commands on a PostgreSQL database.",
		Group:       "storage",
		Icon:        "Database",
		Inputs:      []schema.Port{{ID: "main"}},
		Outputs:     []schema.Port{{ID: "main", Label: "Results"}, {ID: "error", Label: "Error"}},
		Credentials: []string{"postgres"},
		Params: []schema.ParamSchema{
			{Name: "credential", Label: "Credential", Type: "credential", Required: true, CredentialType: "postgres"},
			{Name: "operation", Label: "Operation", Type: "select", Required: true, Default: "query:many", Options: []schema.ParamOption{
				{Label: "Query (multiple rows)", Value: "query:many"},
				{Label: "Query (single row)", Value: "query:one"},
				{Label: "Execute (Insert, Update, Delete)", Value: "exec"},
			}},
			{Name: "query", Label: "SQL Query", Type: "code:sql", Required: true},
			{Name: "params", Label: "Query Parameters", Type: "json", Description: "An array of values for parameterized queries (e.g., using $1, $2)."},
		},
		Execute: executePostgres,
	}
}

// executePostgres is the execution function for the PostgreSQL node.
func executePostgres(ctx *schema.ExecContext) (schema.NodeResult, error) {
	// 1. Get credentials and construct DSN
	cred, err := ctx.Credential("credential")
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("postgres: failed to get credentials: %w", err)
	}

	// Assumes credential 'data' field is a map with keys: user, password, host, port, dbname
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
		cred["user"],
		cred["password"],
		cred["host"],
		cred["port"],
		cred["dbname"],
	)

	// 2. Connect to the database
	dbpool, err := pgxpool.New(ctx.Context, dsn)
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("postgres: unable to connect to database: %w", err)
	}
	defer dbpool.Close()

	// 3. Get query and parameters
	query, _ := ctx.Params["query"].(string)
	rawParams, _ := ctx.Params["params"]
	queryParams, _ := rawParams.([]any) // Cast to slice of any

	operation, _ := ctx.Params["operation"].(string)
	out := make([]schema.Item, 0)

	// 4. Execute based on operation
	switch operation {
	case "query:many":
		rows, err := dbpool.Query(ctx.Context, query, queryParams...)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("postgres query failed: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				return schema.NodeResult{}, fmt.Errorf("postgres failed to read row values: %w", err)
			}
			fieldDescs := rows.FieldDescriptions()
			rowData := make(map[string]any)
			for i, fd := range fieldDescs {
				rowData[string(fd.Name)] = values[i]
			}
			out = append(out, schema.Item{Data: rowData})
		}
		if err := rows.Err(); err != nil {
			return schema.NodeResult{}, fmt.Errorf("postgres rows error: %w", err)
		}

	case "query:one":
		rows, err := dbpool.Query(ctx.Context, query, queryParams...)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("postgres query failed: %w", err)
		}
		// No defer, we only want one row
		if rows.Next() {
			values, err := rows.Values()
			if err != nil {
				rows.Close()
				return schema.NodeResult{}, fmt.Errorf("postgres failed to read row values: %w", err)
			}
			fieldDescs := rows.FieldDescriptions()
			rowData := make(map[string]any)
			for i, fd := range fieldDescs {
				rowData[string(fd.Name)] = values[i]
			}
			out = append(out, schema.Item{Data: rowData})
		}
		rows.Close() // Close after reading one row

	case "exec":
		cmdTag, err := dbpool.Exec(ctx.Context, query, queryParams...)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("postgres exec failed: %w", err)
		}
		out = append(out, schema.Item{Data: map[string]any{
			"status":        "success",
			"command":       cmdTag.String(),
			"rows_affected": cmdTag.RowsAffected(),
		}})

	default:
		return schema.NodeResult{}, fmt.Errorf("postgres: unknown operation %q", operation)
	}

	// 5. Return results
	return schema.NodeResult{
		Outputs: map[string][]schema.Item{"main": out},
	}, nil
}