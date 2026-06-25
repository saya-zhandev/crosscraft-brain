package google

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
	"google.golang.org/api/option"
	sheets "google.golang.org/api/sheets/v4"
)

// ---------------------------------------------------------------------------
// Service cache
// ---------------------------------------------------------------------------

var (
	sheetsSvcMu    sync.Mutex
	sheetsSvcCache = map[string]*sheets.Service{}
)

func getSheetsService(ctx *schema.ExecContext, base string) (*sheets.Service, error) {
	client, err := ctx.AuthorizedClient("credential")
	if err != nil {
		return nil, fmt.Errorf("google sheets: authorized client: %w", err)
	}
	credID, _ := ctx.Params["credential"].(string)
	cacheKey := credID + "|" + base

	// Fast path: read under lock.
	sheetsSvcMu.Lock()
	if svc, ok := sheetsSvcCache[cacheKey]; ok {
		sheetsSvcMu.Unlock()
		return svc, nil
	}
	sheetsSvcMu.Unlock()

	// Slow path: construct and cache.
	// Use WithEndpoint (consistent with Gmail/Calendar) and wrap the
	// client with retry logic for transient failures (429 / 5xx).
	endpoint := base
	if endpoint == "" {
		endpoint = "https://sheets.googleapis.com/"
	}
	retryClient := wrapWithRetry(client)
	svc, err := sheets.NewService(context.Background(),
		option.WithHTTPClient(retryClient),
		option.WithEndpoint(strings.TrimRight(endpoint, "/")+"/"),
	)
	if err != nil {
		return nil, fmt.Errorf("google sheets: new service: %w", err)
	}

	// Double-checked lock to avoid duplicate construction.
	sheetsSvcMu.Lock()
	if existing, ok := sheetsSvcCache[cacheKey]; ok {
		sheetsSvcMu.Unlock()
		return existing, nil
	}
	sheetsSvcCache[cacheKey] = svc
	sheetsSvcMu.Unlock()
	return svc, nil
}

// ---------------------------------------------------------------------------
// Node definition
// ---------------------------------------------------------------------------

func SheetsNode(base string) schema.NodeDefinition {
	return schema.NodeDefinition{
		Type:        "google.sheets",
		Label:       "Google Sheets",
		Group:       "integration",
		Icon:        "Sheet",
		Description: "Read, mutate, map, and watch Google Sheets.",
		Inputs:      []schema.Port{{ID: "main"}},
		Outputs:     []schema.Port{{ID: "main"}},
		Credentials: []string{credType},
		IsTrigger:   true,
		Params: []schema.ParamSchema{
			{Name: "credential", Label: "Credential", Type: "credential", Required: true, CredentialType: credType},
			{Name: "operation", Label: "Operation", Type: "select", Required: true, Options: []schema.ParamOption{
				{Label: "Get Spreadsheet", Value: "spreadsheet:get"},
				{Label: "Create Spreadsheet", Value: "spreadsheet:create"},
				{Label: "Delete Spreadsheet", Value: "spreadsheet:delete"},
				{Label: "Get Values", Value: "values:get"},
				{Label: "Append Values", Value: "values:append"},
				{Label: "Update Values", Value: "values:update"},
				{Label: "Clear Values", Value: "values:clear"},
				{Label: "Map Rows to Objects", Value: "values:toObjects"},
				{Label: "Delete Rows", Value: "values:deleteRows"},
				{Label: "Delete Columns", Value: "values:deleteColumns"},
				{Label: "Trigger: Row Added", Value: "trigger:rowAdded"},
				{Label: "Trigger: Row Updated", Value: "trigger:rowUpdated"},
			}},
			{
				Name: "spreadsheetId", Label: "Spreadsheet ID", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"spreadsheet:get", "spreadsheet:delete",
					"values:get", "values:append", "values:update", "values:clear",
					"values:toObjects", "values:deleteRows", "values:deleteColumns",
					"trigger:rowAdded", "trigger:rowUpdated",
				}},
			},
			{
				Name: "range", Label: "Range (A1 notation, e.g. Sheet1!A1:C10)", Type: "string",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"values:get", "values:append", "values:update", "values:clear",
					"values:toObjects", "values:deleteRows", "values:deleteColumns",
					"trigger:rowAdded", "trigger:rowUpdated",
				}},
			},
			{
				Name: "body", Label: "Body (JSON)", Type: "json",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"spreadsheet:create", "values:append", "values:update",
				}},
			},
			{
				Name: "count", Label: "Count", Type: "number", Default: float64(1),
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"values:deleteRows", "values:deleteColumns",
				}},
			},
			{
				Name: "pollSeconds", Label: "Poll seconds", Type: "number", Default: float64(30),
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"trigger:rowAdded", "trigger:rowUpdated",
				}},
			},
		},
		Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
			return executeSheetsNode(ctx, base)
		},
	}
}

// ---------------------------------------------------------------------------
// Main execution dispatch
// ---------------------------------------------------------------------------

func executeSheetsNode(ctx *schema.ExecContext, base string) (schema.NodeResult, error) {
	op, _ := ctx.Params["operation"].(string)
	if op == "" {
		return schema.NodeResult{}, fmt.Errorf("google sheets: operation is required")
	}

	svc, err := getSheetsService(ctx, base)
	if err != nil {
		return schema.NodeResult{}, err
	}

	spreadsheetID, _ := ctx.Params["spreadsheetId"].(string)
	rangeValue, _ := ctx.Params["range"].(string)

	switch op {

	case "spreadsheet:get":
		if spreadsheetID == "" {
			return schema.NodeResult{}, fmt.Errorf("google sheets spreadsheet:get: spreadsheetId is required")
		}
		resp, err := svc.Spreadsheets.Get(spreadsheetID).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("google sheets spreadsheet:get: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{"spreadsheet": resp}}}}}, nil

	case "spreadsheet:create":
		bodyValue := asObject(ctx.RawParam("body"))
		title, _ := bodyValue["title"].(string)
		if title == "" {
			return schema.NodeResult{}, fmt.Errorf("google sheets spreadsheet:create: body.title is required")
		}
		created, err := svc.Spreadsheets.Create(&sheets.Spreadsheet{
			Properties: &sheets.SpreadsheetProperties{Title: title},
		}).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("google sheets spreadsheet:create: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{"spreadsheet": created}}}}}, nil

	case "spreadsheet:delete":
		if spreadsheetID == "" {
			return schema.NodeResult{}, fmt.Errorf("google sheets spreadsheet:delete: spreadsheetId is required")
		}
		// The Sheets API does not expose a DeleteSpreadsheet method.
		// Deletion goes through Drive: DELETE /drive/v3/files/{id}.
		// Re-fetch the client (wrapped with retry) since the Sheets
		// service doesn't expose its underlying HTTP client.
		client, err := ctx.AuthorizedClient("credential")
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("google sheets spreadsheet:delete: %w", err)
		}
		client = wrapWithRetry(client)
		req, err := http.NewRequestWithContext(
			context.Background(), "DELETE",
			"https://www.googleapis.com/drive/v3/files/"+spreadsheetID,
			nil,
		)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("google sheets spreadsheet:delete: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("google sheets spreadsheet:delete: %w", err)
		}
		resp.Body.Close()
		if resp.StatusCode >= 400 {
			return schema.NodeResult{}, fmt.Errorf("google sheets spreadsheet:delete: Drive API returned %d", resp.StatusCode)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{"deleted": true, "spreadsheetId": spreadsheetID}}}}}, nil

	case "values:get":
		if spreadsheetID == "" {
			return schema.NodeResult{}, fmt.Errorf("google sheets values:get: spreadsheetId is required")
		}
		if rangeValue == "" {
			return schema.NodeResult{}, fmt.Errorf("google sheets values:get: range is required")
		}
		resp, err := svc.Spreadsheets.Values.Get(spreadsheetID, rangeValue).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("google sheets values:get: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": rowsToItems(resp.Values)}}, nil

	case "values:append":
		if spreadsheetID == "" {
			return schema.NodeResult{}, fmt.Errorf("google sheets values:append: spreadsheetId is required")
		}
		if rangeValue == "" {
			return schema.NodeResult{}, fmt.Errorf("google sheets values:append: range is required")
		}
		bodyValue := asObject(ctx.RawParam("body"))
		values, _ := bodyValue["values"].([]any)
		resp, err := svc.Spreadsheets.Values.Append(spreadsheetID, rangeValue, &sheets.ValueRange{
			Values: toSheetValues(values),
		}).ValueInputOption("USER_ENTERED").Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("google sheets values:append: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{"updated": true, "response": resp}}}}}, nil

	case "values:update":
		if spreadsheetID == "" {
			return schema.NodeResult{}, fmt.Errorf("google sheets values:update: spreadsheetId is required")
		}
		if rangeValue == "" {
			return schema.NodeResult{}, fmt.Errorf("google sheets values:update: range is required")
		}
		bodyValue := asObject(ctx.RawParam("body"))
		values, _ := bodyValue["values"].([]any)
		if _, err := svc.Spreadsheets.Values.Update(spreadsheetID, rangeValue, &sheets.ValueRange{
			Values: toSheetValues(values),
		}).ValueInputOption("USER_ENTERED").Do(); err != nil {
			return schema.NodeResult{}, fmt.Errorf("google sheets values:update: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{"updated": true}}}}}, nil

	case "values:clear":
		if spreadsheetID == "" {
			return schema.NodeResult{}, fmt.Errorf("google sheets values:clear: spreadsheetId is required")
		}
		if rangeValue == "" {
			return schema.NodeResult{}, fmt.Errorf("google sheets values:clear: range is required")
		}
		if _, err := svc.Spreadsheets.Values.Clear(spreadsheetID, rangeValue, &sheets.ClearValuesRequest{}).Do(); err != nil {
			return schema.NodeResult{}, fmt.Errorf("google sheets values:clear: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{"cleared": true}}}}}, nil

	case "values:toObjects":
		if spreadsheetID == "" {
			return schema.NodeResult{}, fmt.Errorf("google sheets values:toObjects: spreadsheetId is required")
		}
		if rangeValue == "" {
			return schema.NodeResult{}, fmt.Errorf("google sheets values:toObjects: range is required")
		}
		resp, err := svc.Spreadsheets.Values.Get(spreadsheetID, rangeValue).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("google sheets values:toObjects: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": rowsToObjects(resp.Values)}}, nil

	case "values:deleteRows":
		if spreadsheetID == "" {
			return schema.NodeResult{}, fmt.Errorf("google sheets values:deleteRows: spreadsheetId is required")
		}
		if rangeValue == "" {
			return schema.NodeResult{}, fmt.Errorf("google sheets values:deleteRows: range is required")
		}
		return deleteDimension(ctx, svc, spreadsheetID, rangeValue, true)

	case "values:deleteColumns":
		if spreadsheetID == "" {
			return schema.NodeResult{}, fmt.Errorf("google sheets values:deleteColumns: spreadsheetId is required")
		}
		if rangeValue == "" {
			return schema.NodeResult{}, fmt.Errorf("google sheets values:deleteColumns: range is required")
		}
		return deleteDimension(ctx, svc, spreadsheetID, rangeValue, false)

	case "trigger:rowAdded", "trigger:rowUpdated":
		if spreadsheetID == "" {
			return schema.NodeResult{}, fmt.Errorf("google sheets %s: spreadsheetId is required", op)
		}
		if rangeValue == "" {
			return schema.NodeResult{}, fmt.Errorf("google sheets %s: range is required", op)
		}
		return executeSheetsTrigger(ctx, svc, spreadsheetID, rangeValue, op, base)

	default:
		return schema.NodeResult{}, fmt.Errorf("google sheets: unknown operation %q", op)
	}
}

// ---------------------------------------------------------------------------
// Trigger (polling)
// ---------------------------------------------------------------------------

func executeSheetsTrigger(ctx *schema.ExecContext, svc *sheets.Service, spreadsheetID, rangeValue, op, base string) (schema.NodeResult, error) {
	if ctx.State == nil {
		ctx.State = map[string]any{}
	}

	pollSeconds := parseIntParam(ctx.Params["pollSeconds"], 30)
	if pollSeconds < 1 {
		pollSeconds = 1
	}

	lastPollKey := fmt.Sprintf("sheets:lastPoll:%s:%s:%s", op, spreadsheetID, rangeValue)
	cursorKey := fmt.Sprintf("sheets:cursor:%s:%s", spreadsheetID, rangeValue)
	hashKey := fmt.Sprintf("sheets:hashes:%s:%s", spreadsheetID, rangeValue)

	if lastPollAny, ok := ctx.State[lastPollKey]; ok {
		if ts, valid := toInt64(lastPollAny); valid && time.Since(time.Unix(ts, 0)) < time.Duration(pollSeconds)*time.Second {
			return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {}}}, nil
		}
	}

	resp, err := svc.Spreadsheets.Values.Get(spreadsheetID, rangeValue).Do()
	if err != nil {
		return schema.NodeResult{}, fmt.Errorf("google sheets %s: %w", op, err)
	}
	rows := rowsToItems(resp.Values)

	var cursor int64
	if v, ok := ctx.State[cursorKey]; ok {
		cursor, _ = toInt64(v)
	}

	var out []schema.Item

	switch op {

	case "trigger:rowAdded":
		if int(cursor) < len(rows) {
			for i, row := range rows[cursor:] {
				out = append(out, flattenRow(row, int(cursor)+i))
			}
		}

	case "trigger:rowUpdated":
		currentHashes := hashRows(rows)

		var storedHashes []string
		if raw, ok := ctx.State[hashKey].([]any); ok {
			for _, v := range raw {
				if s, ok := v.(string); ok {
					storedHashes = append(storedHashes, s)
				}
			}
		}

		// Compare overlapping rows by hash (content change detection).
		for i := 0; i < len(currentHashes) && i < len(storedHashes); i++ {
			if storedHashes[i] != currentHashes[i] {
				out = append(out, flattenRow(rows[i], i))
			}
		}

		// Emit rows beyond the stored hash count (new rows).
		for i := len(storedHashes); i < len(currentHashes); i++ {
			out = append(out, flattenRow(rows[i], i))
		}

		hashAny := make([]any, len(currentHashes))
		for i, h := range currentHashes {
			hashAny[i] = h
		}
		ctx.State[hashKey] = hashAny
	}

	ctx.State[cursorKey] = int64(len(rows))
	ctx.State[lastPollKey] = time.Now().Unix()
	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
}

// ---------------------------------------------------------------------------
// Row hash / flatten helpers
// ---------------------------------------------------------------------------

func hashRows(rows []schema.Item) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		b, _ := json.Marshal(r.JSON)
		h := sha256.Sum256(b)
		out[i] = fmt.Sprintf("%x", h[:])
	}
	return out
}

func flattenRow(row schema.Item, index int) schema.Item {
	rowMap := row.JSON
	if values, ok := rowMap["values"].([]any); ok {
		item := map[string]any{}
		item["_row"] = index
		for i, v := range values {
			item[fmt.Sprintf("col_%d", i+1)] = v
		}
		return schema.Item{JSON: item}
	}
	return row
}

// ---------------------------------------------------------------------------
// Delete rows / columns (batch update)
// ---------------------------------------------------------------------------

var a1CellRe = regexp.MustCompile(`^([A-Z]+)(\d+)$`)

func parseCellRef(cell string) (colIdx int, rowNum int) {
	cell = strings.ToUpper(strings.TrimSpace(cell))
	m := a1CellRe.FindStringSubmatch(cell)
	if m == nil {
		return 0, 1
	}
	colStr := m[1]
	colIdx = 0
	for _, ch := range colStr {
		colIdx = colIdx*26 + int(ch-'A'+1)
	}
	colIdx--
	rowNum, _ = strconv.Atoi(m[2])
	if rowNum < 1 {
		rowNum = 1
	}
	return
}

func parseRangeStart(rangeValue string) (colIdx int, rowIdx int) {
	if idx := strings.Index(rangeValue, "!"); idx >= 0 {
		rangeValue = rangeValue[idx+1:]
	}
	startCell := rangeValue
	if idx := strings.Index(rangeValue, ":"); idx >= 0 {
		startCell = rangeValue[:idx]
	}
	c, r := parseCellRef(startCell)
	return c, r - 1
}

func resolveSheetID(spreadsheet *sheets.Spreadsheet, rangeValue string) int64 {
	sheetName := ""
	if idx := strings.Index(rangeValue, "!"); idx >= 0 {
		sheetName = rangeValue[:idx]
	}
	if spreadsheet.Sheets != nil {
		for _, s := range spreadsheet.Sheets {
			if s.Properties != nil && s.Properties.Title != "" {
				if sheetName == s.Properties.Title || (sheetName == "" && strings.HasPrefix(s.Properties.Title, "Sheet")) {
					return s.Properties.SheetId
				}
			}
		}
	}
	return 0
}

func deleteDimension(ctx *schema.ExecContext, svc *sheets.Service, spreadsheetID, rangeValue string, isRows bool) (schema.NodeResult, error) {
	count := parseIntParam(ctx.Params["count"], 1)
	if count < 1 {
		count = 1
	}

	colIdx, rowIdx := parseRangeStart(rangeValue)

	spreadsheet, err := svc.Spreadsheets.Get(spreadsheetID).Do()
	if err != nil {
		label := "values:deleteRows"
		if !isRows {
			label = "values:deleteColumns"
		}
		return schema.NodeResult{}, fmt.Errorf("google sheets %s: %w", label, err)
	}
	sheetID := resolveSheetID(spreadsheet, rangeValue)

	var startIdx int64
	dimension := "ROWS"
	if isRows {
		startIdx = int64(rowIdx)
	} else {
		startIdx = int64(colIdx)
		dimension = "COLUMNS"
	}

	label := "values:deleteRows"
	if !isRows {
		label = "values:deleteColumns"
	}

	requests := []*sheets.Request{{
		DeleteDimension: &sheets.DeleteDimensionRequest{
			Range: &sheets.DimensionRange{
				SheetId:    sheetID,
				Dimension:  dimension,
				StartIndex: startIdx,
				EndIndex:   startIdx + int64(count),
			},
		},
	}}
	if _, err := svc.Spreadsheets.BatchUpdate(spreadsheetID, &sheets.BatchUpdateSpreadsheetRequest{Requests: requests}).Do(); err != nil {
		return schema.NodeResult{}, fmt.Errorf("google sheets %s: %w", label, err)
	}
	return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{"deleted": true}}}}}, nil
}

// ---------------------------------------------------------------------------
// Row helpers
// ---------------------------------------------------------------------------

func rowsToItems(values [][]any) []schema.Item {
	out := make([]schema.Item, 0, len(values))
	for _, row := range values {
		out = append(out, schema.Item{JSON: map[string]any{"values": row}})
	}
	return out
}

func rowsToObjects(values [][]any) []schema.Item {
	if len(values) == 0 {
		return []schema.Item{}
	}
	headers := make([]string, len(values[0]))
	for i, v := range values[0] {
		headers[i] = fmt.Sprint(v)
	}
	out := make([]schema.Item, 0, len(values)-1)
	for _, row := range values[1:] {
		obj := map[string]any{}
		for i, header := range headers {
			if i < len(row) {
				obj[header] = row[i]
			}
		}
		out = append(out, schema.Item{JSON: obj})
	}
	return out
}

func toSheetValues(values []any) [][]any {
	out := make([][]any, 0, len(values))
	for _, v := range values {
		if row, ok := v.([]any); ok {
			out = append(out, row)
			continue
		}
		out = append(out, []any{v})
	}
	return out
}
