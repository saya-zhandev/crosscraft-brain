// Package payments provides payment-gateway integration nodes: Stripe.
package payments

import (
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/rest"
	"github.com/CrossCraftAI/crosscraft-brain/server/internal/schema"
)

func sp(name, label string, required bool) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "string", Required: required}
}
func ip(name, label string, def int) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "number", Default: def}
}
func jp(name, label string) schema.ParamSchema {
	return schema.ParamSchema{Name: name, Label: label, Type: "json"}
}

// Nodes returns the full payments node pack.
func Nodes() []schema.NodeDefinition {
	return []schema.NodeDefinition{
		Stripe().Build(),
	}
}

// Stripe exposes the Stripe REST API via a declarative node (Bearer auth with SK key).
func Stripe() rest.Node {
	customerID := sp("customerId", "Customer ID (cus_...)", true)
	chargeID := sp("chargeId", "Charge ID (ch_...)", true)
	paymentIntentID := sp("paymentIntentId", "Payment Intent ID (pi_...)", true)
	subscriptionID := sp("subscriptionId", "Subscription ID (sub_...)", true)
	body := jp("body", "Body (JSON: Stripe API params)")
	limit := ip("limit", "Max results", 10)
	return rest.Node{
		Type: "payments.stripe", Label: "Stripe", Group: "integration", Icon: "CreditCard",
		Description: "Manage Stripe customers, charges, payment intents, and subscriptions.",
		BaseURL:     "https://api.stripe.com/v1",
		CredType:    "stripeApi",
		Auth:        rest.Auth{Kind: "header", Header: "Authorization", Prefix: "Bearer ", ValueField: "accessToken"},
		Ops: []rest.Op{
			// Customers
			{Resource: "customer", Name: "list", Label: "List Customers", Method: "GET",
				Path: "/customers", ItemsPath: "data",
				Query: map[string]string{"limit": "limit"},
				Params: []schema.ParamSchema{limit}},
			{Resource: "customer", Name: "get", Label: "Get Customer", Method: "GET",
				Path: "/customers/{customerId}",
				Params: []schema.ParamSchema{customerID}},
			{Resource: "customer", Name: "create", Label: "Create Customer", Method: "POST",
				Path: "/customers", BodyParam: "body",
				Params: []schema.ParamSchema{body}},
			{Resource: "customer", Name: "update", Label: "Update Customer", Method: "POST",
				Path: "/customers/{customerId}", BodyParam: "body",
				Params: []schema.ParamSchema{customerID, body}},
			{Resource: "customer", Name: "delete", Label: "Delete Customer", Method: "DELETE",
				Path: "/customers/{customerId}",
				Params: []schema.ParamSchema{customerID}},
			// Charges
			{Resource: "charge", Name: "list", Label: "List Charges", Method: "GET",
				Path: "/charges", ItemsPath: "data",
				Query: map[string]string{"limit": "limit"},
				Params: []schema.ParamSchema{limit}},
			{Resource: "charge", Name: "get", Label: "Get Charge", Method: "GET",
				Path: "/charges/{chargeId}",
				Params: []schema.ParamSchema{chargeID}},
			// Payment Intents
			{Resource: "paymentIntent", Name: "list", Label: "List Payment Intents", Method: "GET",
				Path: "/payment_intents", ItemsPath: "data",
				Query: map[string]string{"limit": "limit"},
				Params: []schema.ParamSchema{limit}},
			{Resource: "paymentIntent", Name: "get", Label: "Get Payment Intent", Method: "GET",
				Path: "/payment_intents/{paymentIntentId}",
				Params: []schema.ParamSchema{paymentIntentID}},
			{Resource: "paymentIntent", Name: "create", Label: "Create Payment Intent", Method: "POST",
				Path: "/payment_intents", BodyParam: "body",
				Params: []schema.ParamSchema{body}},
			{Resource: "paymentIntent", Name: "cancel", Label: "Cancel Payment Intent", Method: "POST",
				Path: "/payment_intents/{paymentIntentId}/cancel",
				Params: []schema.ParamSchema{paymentIntentID}},
			// Subscriptions
			{Resource: "subscription", Name: "list", Label: "List Subscriptions", Method: "GET",
				Path: "/subscriptions", ItemsPath: "data",
				Query: map[string]string{"limit": "limit"},
				Params: []schema.ParamSchema{limit}},
			{Resource: "subscription", Name: "get", Label: "Get Subscription", Method: "GET",
				Path: "/subscriptions/{subscriptionId}",
				Params: []schema.ParamSchema{subscriptionID}},
			{Resource: "subscription", Name: "cancel", Label: "Cancel Subscription", Method: "DELETE",
				Path: "/subscriptions/{subscriptionId}",
				Params: []schema.ParamSchema{subscriptionID}},
			// Balance & Payouts
			{Resource: "balance", Name: "get", Label: "Get Balance", Method: "GET",
				Path: "/balance"},
			{Resource: "payout", Name: "list", Label: "List Payouts", Method: "GET",
				Path: "/payouts", ItemsPath: "data",
				Query: map[string]string{"limit": "limit"},
				Params: []schema.ParamSchema{limit}},
		},
	}
}
