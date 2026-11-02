// Package receipts reads purchase and warranty details from a receipt with
// Claude, for warranties drafted from documents in Paperless.
package receipts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Details are what a receipt says about the purchase. Empty strings and nil
// mean the receipt doesn't say.
type Details struct {
	ProductName    string   `json:"productName"`
	Brand          string   `json:"brand"`
	Model          string   `json:"model"`
	Vendor         string   `json:"vendor"`
	PurchaseDate   string   `json:"purchaseDate"`
	Price          *float64 `json:"price"`
	Currency       string   `json:"currency"`
	WarrantyMonths *int     `json:"warrantyMonths"`
	SerialNumbers  []string `json:"serialNumbers"`
	OrderNumber    string   `json:"orderNumber"`
}

// schema is the JSON shape Claude answers in.
var schema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required": []string{"productName", "brand", "model", "vendor", "purchaseDate", "price", "currency",
		"warrantyMonths", "serialNumbers", "orderNumber"},
	"properties": map[string]any{
		"productName":    map[string]any{"type": "string", "description": "The product, without brand or model, e.g. \"Dishwasher\""},
		"brand":          map[string]any{"type": "string"},
		"model":          map[string]any{"type": "string", "description": "Model name or number"},
		"vendor":         map[string]any{"type": "string", "description": "The shop that sold it"},
		"purchaseDate":   map[string]any{"type": "string", "description": "YYYY-MM-DD, or empty"},
		"price":          map[string]any{"anyOf": []any{map[string]any{"type": "number"}, map[string]any{"type": "null"}}, "description": "Price paid for the product including tax"},
		"currency":       map[string]any{"type": "string", "description": "ISO 4217 code such as CZK or EUR, or empty"},
		"warrantyMonths": map[string]any{"anyOf": []any{map[string]any{"type": "integer"}, map[string]any{"type": "null"}}, "description": "Only if the document states the warranty length"},
		"serialNumbers":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"orderNumber":    map[string]any{"type": "string", "description": "Order or invoice number"},
	},
}

const instructions = `You read receipts, invoices and warranty cards to fill in a warranty tracker.

Find the product the document is about. When it lists several items, pick the one that would most likely carry a warranty, usually the most expensive device, and ignore delivery, services and accessories. Copy names, model numbers and serial numbers exactly as printed. Give the price paid for that product including tax. Give the warranty length only when the document states it; don't assume a legal default. Leave anything the document doesn't show empty, or null for numbers.`

// Reader reads receipts with Claude.
type Reader struct {
	client anthropic.Client
	model  string
}

func New(apiKey, model string) *Reader {
	return &Reader{client: anthropic.NewClient(option.WithAPIKey(apiKey)), model: model}
}

// Read extracts the details from a document: a PDF or an image.
func (r *Reader) Read(ctx context.Context, data []byte, mimeType string) (*Details, error) {
	b64 := base64.StdEncoding.EncodeToString(data)
	var doc anthropic.BetaContentBlockParamUnion
	switch mimeType {
	case "application/pdf":
		doc = anthropic.NewBetaDocumentBlock(anthropic.BetaBase64PDFSourceParam{Data: b64})
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		doc = anthropic.NewBetaImageBlock(anthropic.BetaBase64ImageSourceParam{
			Data: b64, MediaType: anthropic.BetaBase64ImageSourceMediaType(mimeType),
		})
	default:
		return nil, fmt.Errorf("can't read %s documents", mimeType)
	}

	msg, err := r.client.Beta.Messages.New(ctx, anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(r.model),
		MaxTokens: 16000,
		System:    []anthropic.BetaTextBlockParam{{Text: instructions}},
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(doc, anthropic.NewBetaTextBlock("Read this document.")),
		},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffortLow,
			Format: anthropic.BetaJSONOutputFormatParam{Schema: schema},
		},
		// A declined request is retried on the model's default fallback.
		Fallbacks: anthropic.BetaFallbacksParamOfDefault(),
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
	})
	if err != nil {
		return nil, err
	}
	switch msg.StopReason {
	case anthropic.BetaStopReasonRefusal:
		return nil, errors.New("Claude declined to read the document")
	case anthropic.BetaStopReasonMaxTokens:
		return nil, errors.New("Claude's answer was cut off")
	}
	var text strings.Builder
	for _, block := range msg.Content {
		if t, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			text.WriteString(t.Text)
		}
	}
	var d Details
	if err := json.Unmarshal([]byte(text.String()), &d); err != nil {
		return nil, fmt.Errorf("couldn't read Claude's answer: %w", err)
	}
	return &d, nil
}
