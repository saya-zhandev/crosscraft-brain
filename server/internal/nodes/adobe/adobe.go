// Package adobe provides Adobe integration nodes. Adobe ships no official Go SDK,
// so these are plain REST on the declarative framework. Acrobat Sign authenticates
// with an integration key (Bearer header); its account shard/base URL is
// overridable per node. PDF Services / Firefly (async jobs + binary) are a
// follow-up that will use the Adobe IMS server-to-server credential (adobeOAuth2Api).
package adobe

import (
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/rest"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

func sp(name, label string, required bool) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "string", Required: required}
}
func jp(name, label string) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "json"}
}

// Nodes returns the Adobe node pack.
func Nodes() []schema.NodeDefinition {
	return []schema.NodeDefinition{
		AcrobatSign("https://api.na1.adobesign.com/api/rest/v6").Build(),
	}
}

// AcrobatSign — Adobe Acrobat Sign v6 (e-signature). Auth: integration key as a
// Bearer header (adobeSignApi). The base URL defaults to the na1 shard and is
// overridable via the per-node "baseUrl" param for other account regions.
func AcrobatSign(base string) rest.Node {
	agID := sp("agreementId", "Agreement ID", true)
	body := jp("body", "Body (JSON)")
	return rest.Node{
		Type: "adobe.acrobatSign", Label: "Adobe Acrobat Sign", Group: "integration", Icon: "FileSignature",
		Description:  "Send and track e-signature agreements.",
		BaseURL:      base,
		BaseURLParam: "baseUrl",
		CredType:     "adobeSignApi",
		Auth:         rest.Auth{Kind: "header", Header: "Authorization", Prefix: "Bearer ", ValueField: "accessToken"},
		Ops: []rest.Op{
			{Resource: "agreement", Name: "list", Label: "List Agreements", Method: "GET", Path: "/agreements", ItemsPath: "userAgreementList"},
			{Resource: "agreement", Name: "get", Label: "Get Agreement", Method: "GET", Path: "/agreements/{agreementId}", Params: []schema.ParamSchema{agID}},
			{Resource: "agreement", Name: "create", Label: "Create Agreement", Method: "POST", Path: "/agreements", BodyParam: "body", Params: []schema.ParamSchema{body}},
			{Resource: "agreement", Name: "signingUrls", Label: "Get Signing URLs", Method: "GET", Path: "/agreements/{agreementId}/signingUrls", Params: []schema.ParamSchema{agID}},
			{Resource: "libraryDocument", Name: "list", Label: "List Library Documents", Method: "GET", Path: "/libraryDocuments", ItemsPath: "libraryDocumentList"},
		},
	}
}
