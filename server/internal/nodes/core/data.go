package core

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// Data-transform nodes: compression, htmlExtract, jsonNode, sortKeys. Registered via init.
func init() { Nodes = append(Nodes, dataNodes...) }

var dataNodes = []schema.NodeDefinition{compressionNode, htmlExtractNode, jsonTransformNode, sortKeysNode}

// ── Compression ───────────────────────────────────────────────────────────────

var compressionNode = schema.NodeDefinition{
	Type: "core.compression", Label: "Compression", Group: "transform", Icon: "FileArchive",
	Description: "Compress or decompress binary data (gzip or zip).",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "action", Label: "Action", Type: "select", Default: "compress", Options: []schema.ParamOption{
			{Label: "Compress", Value: "compress"}, {Label: "Decompress", Value: "decompress"},
		}},
		{Name: "format", Label: "Format", Type: "select", Default: "gzip", Options: []schema.ParamOption{
			{Label: "Gzip (.gz)", Value: "gzip"}, {Label: "Zip (.zip)", Value: "zip"},
		}},
		{Name: "binaryProperty", Label: "Binary property", Type: "string", Default: "data"},
		{Name: "outputProperty", Label: "Output binary property", Type: "string", Default: "data"},
		{Name: "fileName", Label: "File name inside zip (compress only)", Type: "string", Default: "data"},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		action := asString(ctx.Params["action"], "compress")
		format := asString(ctx.Params["format"], "gzip")
		inProp := asString(ctx.Params["binaryProperty"], "data")
		outProp := asString(ctx.Params["outputProperty"], "data")
		fileName := asString(ctx.Params["fileName"], "data")

		out := make([]schema.Item, 0, len(itemsOrEmpty(ctx.Input)))
		for _, item := range itemsOrEmpty(ctx.Input) {
			ref, ok := item.Binary[inProp]
			if !ok {
				return schema.NodeResult{}, fmt.Errorf("compression: binary property %q not found", inProp)
			}
			raw, err := base64.StdEncoding.DecodeString(ref.Data)
			if err != nil {
				return schema.NodeResult{}, fmt.Errorf("compression: decode binary: %w", err)
			}

			var result []byte
			var mimeType, outName string
			if action == "compress" {
				result, mimeType, outName, err = compressData(format, raw, fileName)
			} else {
				result, mimeType, outName, err = decompressData(format, raw, ref.FileName)
			}
			if err != nil {
				return schema.NodeResult{}, err
			}

			m := map[string]any{}
			for k, v := range item.JSON {
				m[k] = v
			}
			m["fileName"] = outName
			m["size"] = len(result)
			binMap := map[string]schema.BinaryRef{}
			for k, v := range item.Binary {
				binMap[k] = v
			}
			binMap[outProp] = schema.BinaryRef{
				Data:     base64.StdEncoding.EncodeToString(result),
				MimeType: mimeType,
				FileName: outName,
			}
			out = append(out, schema.Item{JSON: m, Binary: binMap})
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}

func compressData(format string, data []byte, name string) ([]byte, string, string, error) {
	var buf bytes.Buffer
	switch format {
	case "zip":
		w := zip.NewWriter(&buf)
		f, err := w.Create(name)
		if err != nil {
			return nil, "", "", err
		}
		if _, err = f.Write(data); err != nil {
			return nil, "", "", err
		}
		if err = w.Close(); err != nil {
			return nil, "", "", err
		}
		return buf.Bytes(), "application/zip", name + ".zip", nil
	default: // gzip
		gz := gzip.NewWriter(&buf)
		gz.Name = name
		if _, err := gz.Write(data); err != nil {
			return nil, "", "", err
		}
		if err := gz.Close(); err != nil {
			return nil, "", "", err
		}
		return buf.Bytes(), "application/gzip", name + ".gz", nil
	}
}

func decompressData(format string, data []byte, originalName string) ([]byte, string, string, error) {
	switch format {
	case "zip":
		r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, "", "", err
		}
		if len(r.File) == 0 {
			return []byte{}, "application/octet-stream", "data", nil
		}
		rc, err := r.File[0].Open()
		if err != nil {
			return nil, "", "", err
		}
		defer rc.Close()
		out, err := io.ReadAll(rc)
		return out, "application/octet-stream", r.File[0].Name, err
	default: // gzip
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, "", "", err
		}
		defer gz.Close()
		out, err := io.ReadAll(gz)
		name := gz.Name
		if name == "" {
			name = strings.TrimSuffix(originalName, ".gz")
		}
		if name == "" {
			name = "data"
		}
		return out, "application/octet-stream", name, err
	}
}

// ── HTML Extract ──────────────────────────────────────────────────────────────

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)
var htmlSpaceRe = regexp.MustCompile(`\s{2,}`)

var htmlExtractNode = schema.NodeDefinition{
	Type: "core.htmlExtract", Label: "HTML Extract", Group: "transform", Icon: "FileCode2",
	Description: "Strip HTML tags from a field and optionally extract inner text.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "field", Label: "Source field", Type: "string", Required: true, Default: "html"},
		{Name: "destination", Label: "Output field", Type: "string", Default: "text"},
		{Name: "trim", Label: "Trim whitespace", Type: "boolean", Default: true},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		field := asString(ctx.Params["field"], "html")
		dest := asString(ctx.Params["destination"], "text")
		trim := true
		if v, ok := ctx.Params["trim"]; ok {
			trim = isTruthy(v)
		}
		out := make([]schema.Item, 0, len(itemsOrEmpty(ctx.Input)))
		for _, item := range itemsOrEmpty(ctx.Input) {
			html := fmt.Sprintf("%v", item.JSON[field])
			text := htmlTagRe.ReplaceAllString(html, " ")
			if trim {
				text = strings.TrimSpace(htmlSpaceRe.ReplaceAllString(text, " "))
			}
			m := map[string]any{}
			for k, v := range item.JSON {
				m[k] = v
			}
			m[dest] = text
			out = append(out, schema.Item{JSON: m})
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}

// ── JSON Transform ────────────────────────────────────────────────────────────

var jsonTransformNode = schema.NodeDefinition{
	Type: "core.json", Label: "JSON", Group: "transform", Icon: "Braces",
	Description: "Parse a JSON string into structured data or stringify a field to JSON.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "action", Label: "Action", Type: "select", Default: "parse", Options: []schema.ParamOption{
			{Label: "Parse (string → object)", Value: "parse"},
			{Label: "Stringify (object → string)", Value: "stringify"},
		}},
		{Name: "field", Label: "Field", Type: "string", Required: true, Default: "data"},
		{Name: "destination", Label: "Output field (blank = overwrite)", Type: "string"},
		{Name: "indent", Label: "Pretty-print", Type: "boolean", Default: false,
			ShowWhen: &schema.ShowWhen{Param: "action", Equals: []any{"stringify"}}},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		action := asString(ctx.Params["action"], "parse")
		field := asString(ctx.Params["field"], "data")
		dest := asString(ctx.Params["destination"], "")
		if dest == "" {
			dest = field
		}
		indent := isTruthy(ctx.Params["indent"])

		out := make([]schema.Item, 0, len(itemsOrEmpty(ctx.Input)))
		for _, item := range itemsOrEmpty(ctx.Input) {
			m := map[string]any{}
			for k, v := range item.JSON {
				m[k] = v
			}
			val := item.JSON[field]
			switch action {
			case "stringify":
				var b []byte
				var err error
				if indent {
					b, err = json.MarshalIndent(val, "", "  ")
				} else {
					b, err = json.Marshal(val)
				}
				if err != nil {
					return schema.NodeResult{}, fmt.Errorf("json stringify: %w", err)
				}
				m[dest] = string(b)
			default: // parse
				s := fmt.Sprintf("%v", val)
				var parsed any
				if err := json.Unmarshal([]byte(s), &parsed); err != nil {
					return schema.NodeResult{}, fmt.Errorf("json parse field %q: %w", field, err)
				}
				m[dest] = parsed
			}
			out = append(out, schema.Item{JSON: m})
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}

// ── Sort Keys ─────────────────────────────────────────────────────────────────

var sortKeysNode = schema.NodeDefinition{
	Type: "core.sortKeys", Label: "Sort Keys", Group: "transform", Icon: "CaseSensitive",
	Description: "Return each item with its JSON keys sorted alphabetically.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "order", Label: "Order", Type: "select", Default: "asc", Options: []schema.ParamOption{
			{Label: "Ascending", Value: "asc"}, {Label: "Descending", Value: "desc"},
		}},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		desc := asString(ctx.Params["order"], "asc") == "desc"
		out := make([]schema.Item, 0, len(itemsOrEmpty(ctx.Input)))
		for _, item := range itemsOrEmpty(ctx.Input) {
			keys := make([]string, 0, len(item.JSON))
			for k := range item.JSON {
				keys = append(keys, k)
			}
			if desc {
				sort.Sort(sort.Reverse(sort.StringSlice(keys)))
			} else {
				sort.Strings(keys)
			}
			m := make(map[string]any, len(keys))
			for _, k := range keys {
				m[k] = item.JSON[k]
			}
			out = append(out, schema.Item{JSON: m})
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}
