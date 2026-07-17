package azure

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/rest"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

// BlobStorage — Azure Blob Storage REST API with Shared Key auth.
func BlobStorage(base string) rest.Node {
	body := jp("body", "Body (JSON)")
	containerName := sp("container", "Container name", true)
	blobName := sp("blob", "Blob name/path", true)
	maxResults := ip("maxResults", "Max results", 100)
	binaryProp := schema.ParamSchema{Name: "binaryProperty", Label: "Binary property", Type: "string", Default: "data"}
	outputProp := schema.ParamSchema{Name: "outputProperty", Label: "Output binary property", Type: "string", Default: "data"}

	return rest.Node{
		Type: "azure.blobStorage", Label: "Azure Blob Storage", Group: "integration", Icon: "HardDrive",
		Description:  "List, upload, download, and manage Azure Blob Storage containers and blobs.",
		BaseURL:      base,
		BaseURLParam: "baseUrl",
		CredType:     "azureStorage",
		Auth: rest.Auth{Kind: "none"}, // Shared Key auth must be wired via NewSignedTransport; see AzureSignRequest
		Ops: []rest.Op{
			{Resource: "container", Name: "list", Label: "List Containers", Method: "GET",
				Path: "/?comp=list", ItemsPath: "Containers.Container",
				Query:  map[string]string{"maxresults": "maxResults"},
				Params: []schema.ParamSchema{maxResults}},
			{Resource: "container", Name: "create", Label: "Create Container", Method: "PUT",
				Path: "/{container}?restype=container",
				Params: []schema.ParamSchema{containerName}},
			{Resource: "container", Name: "delete", Label: "Delete Container", Method: "DELETE",
				Path: "/{container}?restype=container",
				Params: []schema.ParamSchema{containerName}},
			{Resource: "blob", Name: "list", Label: "List Blobs", Method: "GET",
				Path: "/{container}?comp=list&restype=container", ItemsPath: "Blobs.Blob",
				Query:  map[string]string{"maxresults": "maxResults"},
				Params: []schema.ParamSchema{containerName, maxResults}},
			{Resource: "blob", Name: "upload", Label: "Upload Blob", Method: "PUT",
				Path: "/{container}/{blob}", BodyParam: "body",
				Params: []schema.ParamSchema{containerName, blobName, binaryProp, body}},
			{Resource: "blob", Name: "download", Label: "Download Blob", Method: "GET",
				Path: "/{container}/{blob}",
				Params: []schema.ParamSchema{containerName, blobName, outputProp}},
			{Resource: "blob", Name: "delete", Label: "Delete Blob", Method: "DELETE",
				Path: "/{container}/{blob}",
				Params: []schema.ParamSchema{containerName, blobName}},
			{Resource: "blob", Name: "copy", Label: "Copy Blob", Method: "PUT",
				Path: "/{container}/{blob}",
				Params: []schema.ParamSchema{containerName, blobName,
					sp("sourceBlob", "Source blob URL", true)}},
		},
	}
}

// NewSignedTransport wraps an http.RoundTripper with Azure Shared Key signing.
func NewSignedTransport(accountName, accountKey string, base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &azureSigningTransport{
		accountName: accountName,
		accountKey:  accountKey,
		base:        base,
	}
}

type azureSigningTransport struct {
	accountName string
	accountKey  string
	base        http.RoundTripper
}

func (t *azureSigningTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	if err := AzureSignRequest(req2, t.accountName, t.accountKey); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(req2)
}

// AzureSignRequest signs an HTTP request with Azure Storage Shared Key.
func AzureSignRequest(req *http.Request, accountName, accountKey string) error {
	key, err := base64.StdEncoding.DecodeString(accountKey)
	if err != nil {
		return fmt.Errorf("azure storage: decode account key: %w", err)
	}

	// Build string to sign per Azure Storage spec
	stringToSign := req.Method + "\n"
	// Content-Encoding, Content-Language, Content-Length, Content-MD5
	stringToSign += "\n\n" // Content-Encoding, Content-Language
	if req.ContentLength > 0 {
		stringToSign += fmt.Sprintf("%d", req.ContentLength)
	}
	stringToSign += "\n"
	// Content-MD5
	stringToSign += "\n"
	// Content-Type
	if ct := req.Header.Get("Content-Type"); ct != "" {
		stringToSign += ct
	}
	stringToSign += "\n"
	// Date
	stringToSign += "\n"
	// If-Modified-Since, If-Match, If-None-Match, If-Unmodified-Since, Range
	stringToSign += "\n\n\n\n\n"
	// CanonicalizedHeaders
	stringToSign += "x-ms-date:" + req.Header.Get("x-ms-date") + "\n"
	stringToSign += "x-ms-version:2020-04-08\n"
	// CanonicalizedResource
	stringToSign += "/" + accountName + req.URL.Path
	if req.URL.RawQuery != "" {
		stringToSign += "\n" + strings.ReplaceAll(req.URL.RawQuery, "&", "\n")
	}

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(stringToSign))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	req.Header.Set("Authorization", "SharedKey "+accountName+":"+sig)
	return nil
}

// xmlContainerList is the Azure XML response for ListContainers.
type xmlContainerList struct {
	XMLName    xml.Name          `xml:"EnumerationResults"`
	Containers xmlContainerSlice `xml:"Containers>Container"`
}

type xmlContainerSlice struct {
	Name       string `xml:"Name"`
	Properties struct {
		LastModified string `xml:"Last-Modified"`
	} `xml:"Properties"`
}
