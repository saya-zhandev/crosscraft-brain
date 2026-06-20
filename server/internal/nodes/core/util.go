package core

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"strconv"
	"time"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/expr"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// Utility function nodes (crypto, date/time). Registered onto core.Nodes via init.
func init() { Nodes = append(Nodes, utilNodes...) }

var utilNodes = []schema.NodeDefinition{cryptoNode, dateTimeNode}

var cryptoNode = schema.NodeDefinition{
	Type: "core.crypto", Label: "Crypto", Group: "transform", Icon: "KeyRound",
	Description: "Hash, HMAC, or Base64 a value; writes the result to an output field.",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "action", Label: "Action", Type: "select", Default: "hash", Options: []schema.ParamOption{
			{Label: "Hash", Value: "hash"}, {Label: "HMAC", Value: "hmac"},
			{Label: "Base64 Encode", Value: "base64Encode"}, {Label: "Base64 Decode", Value: "base64Decode"},
		}},
		{Name: "algorithm", Label: "Algorithm", Type: "select", Default: "sha256", Options: []schema.ParamOption{
			{Label: "MD5", Value: "md5"}, {Label: "SHA1", Value: "sha1"}, {Label: "SHA256", Value: "sha256"}, {Label: "SHA512", Value: "sha512"},
		}, ShowWhen: &schema.ShowWhen{Param: "action", Equals: []any{"hash", "hmac"}}},
		{Name: "value", Label: "Value", Type: "expression", Required: true},
		{Name: "secret", Label: "Secret (HMAC)", Type: "string", ShowWhen: &schema.ShowWhen{Param: "action", Equals: []any{"hmac"}}},
		{Name: "destination", Label: "Output field", Type: "string", Default: "data"},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		action := asString(ctx.Params["action"], "hash")
		algo := asString(ctx.Params["algorithm"], "sha256")
		dest := asString(ctx.Params["destination"], "data")
		rawVal := ctx.RawParam("value")
		in := itemsOrEmpty(ctx.Input)
		out := make([]schema.Item, 0, len(in))
		for _, item := range in {
			v, err := expr.ResolveValue(rawVal, scopeFor(item, ctx))
			if err != nil {
				return schema.NodeResult{}, err
			}
			s := fmt.Sprintf("%v", v)
			var result string
			switch action {
			case "base64Encode":
				result = base64.StdEncoding.EncodeToString([]byte(s))
			case "base64Decode":
				b, err := base64.StdEncoding.DecodeString(s)
				if err != nil {
					return schema.NodeResult{}, err
				}
				result = string(b)
			case "hmac":
				h := hmac.New(hashFor(algo), []byte(asString(ctx.Params["secret"], "")))
				h.Write([]byte(s))
				result = hex.EncodeToString(h.Sum(nil))
			default: // hash
				h := hashFor(algo)()
				h.Write([]byte(s))
				result = hex.EncodeToString(h.Sum(nil))
			}
			m := map[string]any{}
			for k, val := range item.JSON {
				m[k] = val
			}
			m[dest] = result
			out = append(out, schema.Item{JSON: m})
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}

func hashFor(algo string) func() hash.Hash {
	switch algo {
	case "md5":
		return md5.New
	case "sha1":
		return sha1.New
	case "sha512":
		return sha512.New
	default:
		return sha256.New
	}
}

var dateTimeNode = schema.NodeDefinition{
	Type: "core.dateTime", Label: "Date & Time", Group: "transform", Icon: "Clock",
	Description: "Get the current time, or parse / shift a timestamp (RFC3339 or Unix).",
	Inputs:      []schema.Port{{ID: "main"}},
	Outputs:     []schema.Port{{ID: "main"}},
	Params: []schema.ParamSchema{
		{Name: "action", Label: "Action", Type: "select", Default: "now", Options: []schema.ParamOption{
			{Label: "Now", Value: "now"}, {Label: "Parse/Format", Value: "format"},
			{Label: "Add", Value: "add"}, {Label: "Subtract", Value: "subtract"},
		}},
		{Name: "value", Label: "Value (timestamp)", Type: "expression",
			ShowWhen: &schema.ShowWhen{Param: "action", Equals: []any{"format", "add", "subtract"}}},
		{Name: "amount", Label: "Amount", Type: "number",
			ShowWhen: &schema.ShowWhen{Param: "action", Equals: []any{"add", "subtract"}}},
		{Name: "unit", Label: "Unit", Type: "select", Default: "hours", Options: []schema.ParamOption{
			{Label: "Seconds", Value: "seconds"}, {Label: "Minutes", Value: "minutes"},
			{Label: "Hours", Value: "hours"}, {Label: "Days", Value: "days"},
		}, ShowWhen: &schema.ShowWhen{Param: "action", Equals: []any{"add", "subtract"}}},
		{Name: "destination", Label: "Output field", Type: "string", Default: "datetime"},
	},
	Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
		action := asString(ctx.Params["action"], "now")
		dest := asString(ctx.Params["destination"], "datetime")
		rawVal := ctx.RawParam("value")
		emit := func(item schema.Item, t time.Time) schema.Item {
			m := map[string]any{}
			for k, v := range item.JSON {
				m[k] = v
			}
			m[dest] = map[string]any{"iso": t.UTC().Format(time.RFC3339), "unix": t.Unix()}
			return schema.Item{JSON: m}
		}
		in := itemsOrEmpty(ctx.Input)
		out := make([]schema.Item, 0, len(in))
		for _, item := range in {
			var t time.Time
			if action == "now" {
				t = time.Now()
			} else {
				v, err := expr.ResolveValue(rawVal, scopeFor(item, ctx))
				if err != nil {
					return schema.NodeResult{}, err
				}
				t, err = parseTime(v)
				if err != nil {
					return schema.NodeResult{}, err
				}
				if action == "add" || action == "subtract" {
					d := durationOf(asInt(ctx.Params["amount"], 0), asString(ctx.Params["unit"], "hours"))
					if action == "subtract" {
						d = -d
					}
					t = t.Add(d)
				}
			}
			out = append(out, emit(item, t))
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": out}}, nil
	},
}

func parseTime(v any) (time.Time, error) {
	switch t := v.(type) {
	case float64:
		return time.Unix(int64(t), 0).UTC(), nil
	case int64:
		return time.Unix(t, 0).UTC(), nil
	case int:
		return time.Unix(int64(t), 0).UTC(), nil
	case string:
		if t == "" {
			return time.Now(), nil
		}
		if ts, err := time.Parse(time.RFC3339, t); err == nil {
			return ts, nil
		}
		if n, err := strconv.ParseInt(t, 10, 64); err == nil {
			return time.Unix(n, 0).UTC(), nil
		}
		return time.Time{}, fmt.Errorf("cannot parse time %q", t)
	}
	return time.Time{}, fmt.Errorf("cannot parse time value of type %T", v)
}

func durationOf(amount int, unit string) time.Duration {
	switch unit {
	case "seconds":
		return time.Duration(amount) * time.Second
	case "minutes":
		return time.Duration(amount) * time.Minute
	case "days":
		return time.Duration(amount) * 24 * time.Hour
	default:
		return time.Duration(amount) * time.Hour
	}
}
