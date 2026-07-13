// Package azure provides Azure integration nodes: Blob Storage, Cosmos DB, MSSQL,
// Power BI, DevOps, and OpenAI. Blob Storage uses Azure Shared Key auth.
// Cosmos DB uses master key HMAC auth. MSSQL uses database/sql. Power BI,
// DevOps, and OpenAI use Bearer/API-key auth via the declarative REST framework.
package azure

import (
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

func sp(name, label string, required bool) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "string", Required: required}
}
func jp(name, label string) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "json"}
}
func ip(name, label string, def int) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "number", Default: float64(def)}
}

// Nodes returns the full Azure node pack.
func Nodes() []schema.NodeDefinition {
	return []schema.NodeDefinition{
		BlobStorage("https://{accountName}.blob.core.windows.net").Build(),
		CosmosDB("https://{accountName}.documents.azure.com").Build(),
		PowerBI("https://api.powerbi.com/v1.0/myorg").Build(),
		DevOps("https://dev.azure.com").Build(),
		OpenAI("https://{resourceName}.openai.azure.com").Build(),
	}
}
