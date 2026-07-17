package google

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
	"google.golang.org/api/option"
	people "google.golang.org/api/people/v1"
)

// ---------------------------------------------------------------------------
// Service cache
// ---------------------------------------------------------------------------

var (
	peopleSvcMu    sync.Mutex
	peopleSvcCache = map[string]*people.Service{}
)

func getPeopleService(ctx *schema.ExecContext, base string) (*people.Service, error) {
	client, err := ctx.AuthorizedClient("credential")
	if err != nil {
		return nil, fmt.Errorf("contacts: authorized client: %w", err)
	}
	credID, _ := ctx.Params["credential"].(string)
	if credID == "" {
		return nil, fmt.Errorf("contacts: credential is required")
	}
	cacheKey := credID + "|" + base

	peopleSvcMu.Lock()
	if svc, ok := peopleSvcCache[cacheKey]; ok {
		peopleSvcMu.Unlock()
		return svc, nil
	}
	peopleSvcMu.Unlock()

	endpoint := base
	if endpoint == "" {
		endpoint = "https://people.googleapis.com/"
	}
	retryClient := wrapWithRetry(client)
	svc, err := people.NewService(context.Background(),
		option.WithHTTPClient(retryClient),
		option.WithEndpoint(strings.TrimRight(endpoint, "/")+"/"),
	)
	if err != nil {
		return nil, fmt.Errorf("contacts: new service: %w", err)
	}

	peopleSvcMu.Lock()
	if existing, ok := peopleSvcCache[cacheKey]; ok {
		peopleSvcMu.Unlock()
		return existing, nil
	}
	peopleSvcCache[cacheKey] = svc
	peopleSvcMu.Unlock()
	return svc, nil
}

// ---------------------------------------------------------------------------
// Node definition
// ---------------------------------------------------------------------------

func ContactsNode(base string) schema.NodeDefinition {
	return schema.NodeDefinition{
		Type:        "google.contacts",
		Label:       "Google Contacts",
		Group:       "integration",
		Icon:        "Users",
		Description: "Manage Google Contacts via the People API: list, get, create, update, and delete contacts.",
		Inputs:      []schema.Port{{ID: "main"}},
		Outputs:     []schema.Port{{ID: "main"}},
		Credentials: []string{credType},
		Params: []schema.ParamSchema{
			{Name: "credential", Label: "Credential", Type: "credential", Required: true, CredentialType: credType},
			{Name: "operation", Label: "Operation", Type: "select", Required: true, Options: []schema.ParamOption{
				{Label: "List Contacts", Value: "contact:list"},
				{Label: "Get Contact", Value: "contact:get"},
				{Label: "Create Contact", Value: "contact:create"},
				{Label: "Update Contact", Value: "contact:update"},
				{Label: "Delete Contact", Value: "contact:delete"},
			}},
			{
				Name: "contactId", Label: "Contact ID (resource name)", Type: "string",
				Description: "The resource name or ID (e.g. 'people/c123' or just 'c123').",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"contact:get", "contact:update", "contact:delete",
				}},
			},
			{
				Name: "body", Label: "Body (JSON)", Type: "json",
				Description: `Contact data, e.g. {"names":[{"givenName":"John","familyName":"Doe"}],"emailAddresses":[{"value":"john@example.com"}]}`,
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"contact:create", "contact:update",
				}},
			},
			{
				Name: "personFields", Label: "Person fields (comma-separated)", Type: "string",
				Default: "names,emailAddresses,phoneNumbers",
				Description: "Fields to return. Common: names,emailAddresses,phoneNumbers,organizations,addresses,photos",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"contact:list", "contact:get", "contact:create", "contact:update",
				}},
			},
			{
				Name: "pageSize", Label: "Page size", Type: "number", Default: float64(50),
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"contact:list",
				}},
			},
			{
				Name: "maxPages", Label: "Max pages", Type: "number", Default: float64(10),
				Description: "Maximum number of pages to fetch. Set to 1 for first page only.",
				ShowWhen: &schema.ShowWhen{Param: "operation", Equals: []any{
					"contact:list",
				}},
			},
		},
		Execute: func(ctx *schema.ExecContext) (schema.NodeResult, error) {
			return executeContactsNode(ctx, base)
		},
	}
}

// ---------------------------------------------------------------------------
// Main execution dispatch
// ---------------------------------------------------------------------------

func executeContactsNode(ctx *schema.ExecContext, base string) (schema.NodeResult, error) {
	op, _ := ctx.Params["operation"].(string)
	if op == "" {
		return schema.NodeResult{}, fmt.Errorf("contacts: operation is required")
	}

	svc, err := getPeopleService(ctx, base)
	if err != nil {
		return schema.NodeResult{}, err
	}

	contactID, _ := ctx.Params["contactId"].(string)
	personFields, _ := ctx.Params["personFields"].(string)
	if personFields == "" {
		personFields = "names,emailAddresses,phoneNumbers"
	}

	// Normalise resource name: always prefix with "people/" if just an ID.
	resourceName := contactID
	if resourceName != "" && !strings.HasPrefix(resourceName, "people/") {
		resourceName = "people/" + resourceName
	}

	switch op {

	case "contact:list":
		pageSize := parseIntParam(ctx.Params["pageSize"], 50)
		maxPages := parseIntParam(ctx.Params["maxPages"], 10)

		var allItems []schema.Item
		pageToken := ""
		for page := 0; page < maxPages; page++ {
			call := svc.People.Connections.List("people/me").
				PersonFields(personFields).
				PageSize(int64(pageSize))
			if pageToken != "" {
				call = call.PageToken(pageToken)
			}
			resp, err := call.Do()
			if err != nil {
				return schema.NodeResult{}, fmt.Errorf("contacts contact:list: %w", err)
			}
			for _, p := range resp.Connections {
				allItems = append(allItems, personToItem(p))
			}
			if resp.NextPageToken == "" {
				break
			}
			pageToken = resp.NextPageToken
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": allItems}}, nil

	case "contact:get":
		if resourceName == "" {
			return schema.NodeResult{}, fmt.Errorf("contacts contact:get: contactId is required")
		}
		p, err := svc.People.Get(resourceName).PersonFields(personFields).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("contacts contact:get: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {personToItem(p)}}}, nil

	case "contact:create":
		bodyValue := asObject(ctx.RawParam("body"))
		bodyBytes, err := json.Marshal(bodyValue)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("contacts contact:create: marshal body: %w", err)
		}
		var p people.Person
		if err := json.Unmarshal(bodyBytes, &p); err != nil {
			return schema.NodeResult{}, fmt.Errorf("contacts contact:create: unmarshal person: %w", err)
		}
		created, err := svc.People.CreateContact(&p).PersonFields(personFields).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("contacts contact:create: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {personToItem(created)}}}, nil

	case "contact:update":
		if resourceName == "" {
			return schema.NodeResult{}, fmt.Errorf("contacts contact:update: contactId is required")
		}
		bodyValue := asObject(ctx.RawParam("body"))
		bodyBytes, err := json.Marshal(bodyValue)
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("contacts contact:update: marshal body: %w", err)
		}
		var p people.Person
		if err := json.Unmarshal(bodyBytes, &p); err != nil {
			return schema.NodeResult{}, fmt.Errorf("contacts contact:update: unmarshal person: %w", err)
		}
		// The resource name in the etag must match; set it from the path.
		updated, err := svc.People.UpdateContact(resourceName, &p).
			UpdatePersonFields(personFields).Do()
		if err != nil {
			return schema.NodeResult{}, fmt.Errorf("contacts contact:update: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {personToItem(updated)}}}, nil

	case "contact:delete":
		if resourceName == "" {
			return schema.NodeResult{}, fmt.Errorf("contacts contact:delete: contactId is required")
		}
		if _, err := svc.People.DeleteContact(resourceName).Do(); err != nil {
			return schema.NodeResult{}, fmt.Errorf("contacts contact:delete: %w", err)
		}
		return schema.NodeResult{Outputs: map[string][]schema.Item{"main": {{JSON: map[string]any{"deleted": true, "contactId": resourceName}}}}}, nil

	default:
		return schema.NodeResult{}, fmt.Errorf("contacts: unknown operation %q", op)
	}
}

// ---------------------------------------------------------------------------
// SDK struct → schema.Item helper
// ---------------------------------------------------------------------------

func personToItem(p *people.Person) schema.Item {
	if p == nil {
		return schema.Item{JSON: map[string]any{}}
	}
	item := map[string]any{
		"resourceName": p.ResourceName,
		"etag":         p.Etag,
	}
	if len(p.Names) > 0 {
		names := make([]map[string]any, 0, len(p.Names))
		for _, n := range p.Names {
			names = append(names, map[string]any{
				"givenName":      n.GivenName,
				"familyName":     n.FamilyName,
				"displayName":    n.DisplayName,
				"displayNameLastFirst": n.DisplayNameLastFirst,
			})
		}
		item["names"] = names
	}
	if len(p.EmailAddresses) > 0 {
		emails := make([]map[string]any, 0, len(p.EmailAddresses))
		for _, e := range p.EmailAddresses {
			emails = append(emails, map[string]any{
				"value": e.Value,
				"type":  e.Type,
			})
		}
		item["emailAddresses"] = emails
	}
	if len(p.PhoneNumbers) > 0 {
		phones := make([]map[string]any, 0, len(p.PhoneNumbers))
		for _, ph := range p.PhoneNumbers {
			phones = append(phones, map[string]any{
				"value": ph.Value,
				"type":  ph.Type,
			})
		}
		item["phoneNumbers"] = phones
	}
	if len(p.Organizations) > 0 {
		orgs := make([]map[string]any, 0, len(p.Organizations))
		for _, o := range p.Organizations {
			orgs = append(orgs, map[string]any{
				"name":  o.Name,
				"title": o.Title,
				"department": o.Department,
			})
		}
		item["organizations"] = orgs
	}
	if len(p.Addresses) > 0 {
		addrs := make([]map[string]any, 0, len(p.Addresses))
		for _, a := range p.Addresses {
			addrs = append(addrs, map[string]any{
				"streetAddress": a.StreetAddress,
				"city":          a.City,
				"region":        a.Region,
				"postalCode":    a.PostalCode,
				"country":       a.Country,
				"type":          a.Type,
			})
		}
		item["addresses"] = addrs
	}
	if len(p.Photos) > 0 {
		photos := make([]map[string]any, 0, len(p.Photos))
		for _, ph := range p.Photos {
			photos = append(photos, map[string]any{
				"url": ph.Url,
			})
		}
		item["photos"] = photos
	}
	return schema.Item{JSON: item}
}
