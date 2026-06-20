package core

import (
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// File/format nodes: move between an item's binary (a file) and items. CSV/JSON/text
// via stdlib. Registered onto core.Nodes via init.
func init() { Nodes = append(Nodes, fileNodes...) }

var fileNodes = []schema.NodeDefinition{extractFromFileNode, convertToFileNode}

var extractFromFileNode = schema.NodeDefinition{
	Type: "core.extractFromFile", Label: "Extract From File", Group: "transform", Icon: "FileInput",
	Description: "Parse an item's binary file (CSV / JSON / text) into items.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "format", Label: "Format", Type: "select", Default: "csv", Options: []schema.ParamOption{
			{Label: "CSV", Value: "csv"}, {Label: "JSON", Value: "json"}, {Label: "Text", Value: "text"},
		}},
		{Name: "binaryProperty", Label: "Binary property", Type: "string", Default: "data"},
		{Name: "hasHeader", Label: "CSV has a header row", Type: "boolean", Default: true,
			ShowWhen: &schema.ShowWhen{Param: "format", Equals: []any{"csv"}}},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		format := asString(ctx.Params["format"], "csv")
		prop := asString(ctx.Params["binaryProperty"], "data")
		hasHeader := true
		if v, ok := ctx.Params["hasHeader"]; ok {
			hasHeader = isTruthy(v)
		}
		out := []schema.Item{}
		for _, item := range itemsOrEmpty(ctx.Input) {
			if item.Binary == nil {
				continue
			}
			ref, ok := item.Binary[prop]
			if !ok {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(ref.Data)
			if err != nil {
				return schema.NodeResult{}, fmt.Errorf("decode binary %q: %w", prop, err)
			}
			parsed, err := parseFile(format, data, hasHeader)
			if err != nil {
				return schema.NodeResult{}, err
			}
			out = append(out, parsed...)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}

func parseFile(format string, data []byte, hasHeader bool) ([]schema.Item, error) {
	switch format {
	case "json":
		var root any
		if err := json.Unmarshal(data, &root); err != nil {
			return nil, err
		}
		if arr, ok := root.([]any); ok {
			out := make([]schema.Item, 0, len(arr))
			for _, e := range arr {
				out = append(out, toItem(e))
			}
			return out, nil
		}
		return []schema.Item{toItem(root)}, nil
	case "text":
		return []schema.Item{{JSON: map[string]any{"data": string(data)}}}, nil
	default: // csv
		r := csv.NewReader(bytes.NewReader(data))
		r.FieldsPerRecord = -1
		rows, err := r.ReadAll()
		if err != nil {
			return nil, err
		}
		out := []schema.Item{}
		if hasHeader {
			if len(rows) == 0 {
				return out, nil
			}
			headers := rows[0]
			for _, row := range rows[1:] {
				m := map[string]any{}
				for i, h := range headers {
					if i < len(row) {
						m[h] = row[i]
					}
				}
				out = append(out, schema.Item{JSON: m})
			}
		} else {
			for _, row := range rows {
				m := map[string]any{}
				for i, v := range row {
					m[fmt.Sprintf("col%d", i)] = v
				}
				out = append(out, schema.Item{JSON: m})
			}
		}
		return out, nil
	}
}

var convertToFileNode = schema.NodeDefinition{
	Type: "core.convertToFile", Label: "Convert to File", Group: "transform", Icon: "FileOutput",
	Description: "Combine items into a single file (JSON / CSV) in an item's binary.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "format", Label: "Format", Type: "select", Default: "json", Options: []schema.ParamOption{
			{Label: "JSON", Value: "json"}, {Label: "CSV", Value: "csv"},
		}},
		{Name: "binaryProperty", Label: "Output binary property", Type: "string", Default: "data"},
		{Name: "fileName", Label: "File name", Type: "string"},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		format := asString(ctx.Params["format"], "json")
		prop := asString(ctx.Params["binaryProperty"], "data")
		in := ctx.Input
		var data []byte
		var mimeType, defName string
		switch format {
		case "csv":
			var buf bytes.Buffer
			w := csv.NewWriter(&buf)
			headers := unionKeys(in)
			_ = w.Write(headers)
			for _, item := range in {
				row := make([]string, len(headers))
				for i, h := range headers {
					if v, ok := item.JSON[h]; ok {
						row[i] = fmt.Sprintf("%v", v)
					}
				}
				_ = w.Write(row)
			}
			w.Flush()
			if err := w.Error(); err != nil {
				return schema.NodeResult{}, err
			}
			data, mimeType, defName = buf.Bytes(), "text/csv", "data.csv"
		default: // json
			arr := make([]any, 0, len(in))
			for _, item := range in {
				arr = append(arr, item.JSON)
			}
			b, err := json.MarshalIndent(arr, "", "  ")
			if err != nil {
				return schema.NodeResult{}, err
			}
			data, mimeType, defName = b, "application/json", "data.json"
		}
		name := asString(ctx.Params["fileName"], defName)
		item := schema.Item{
			JSON: map[string]any{"fileName": name, "mimeType": mimeType, "size": len(data)},
			Binary: map[string]schema.BinaryRef{
				prop: {Data: base64.StdEncoding.EncodeToString(data), MimeType: mimeType, FileName: name},
			},
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {item}}}, nil
	},
}

func toItem(v any) schema.Item {
	if m, ok := v.(map[string]any); ok {
		return schema.Item{JSON: m}
	}
	return schema.Item{JSON: map[string]any{"value": v}}
}

func unionKeys(items []schema.Item) []string {
	seen := map[string]bool{}
	keys := []string{}
	for _, item := range items {
		for k := range item.JSON {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	sort.Strings(keys)
	return keys
}
