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

// Details are what a receipt says about one product bought. Empty strings
// and nil mean the receipt doesn't say.
type Details struct {
	ProductName  string `json:"productName"`
	Brand        string `json:"brand"`
	Model        string `json:"model"`
	Vendor       string `json:"vendor"`
	PurchaseDate string `json:"purchaseDate"`
	// ReceivedDate is when the buyer received the goods, if the document
	// says and it isn't the purchase date.
	ReceivedDate   string   `json:"receivedDate"`
	Price          *float64 `json:"price"`
	Currency       string   `json:"currency"`
	WarrantyMonths *int     `json:"warrantyMonths"`
	SerialNumbers  []string `json:"serialNumbers"`
	OrderNumber    string   `json:"orderNumber"`
	// Extension is an extended warranty or warranty insurance bought with
	// the product, as the document names it, adding ExtendedMonths.
	Extension      string `json:"extension"`
	ExtendedMonths *int   `json:"extendedMonths"`
}

// MaxItems is how many products Read returns at most.
const MaxItems = 30

// answer is what Claude returns: the purchase, and the products in it.
type answer struct {
	Vendor       string `json:"vendor"`
	PurchaseDate string `json:"purchaseDate"`
	ReceivedDate string `json:"receivedDate"`
	Currency     string `json:"currency"`
	OrderNumber  string `json:"orderNumber"`
	Items        []struct {
		ProductName    string   `json:"productName"`
		Brand          string   `json:"brand"`
		Model          string   `json:"model"`
		Price          *float64 `json:"price"`
		WarrantyMonths *int     `json:"warrantyMonths"`
		SerialNumbers  []string `json:"serialNumbers"`
		Extension      string   `json:"extension"`
		ExtendedMonths *int     `json:"extendedMonths"`
	} `json:"items"`
}

// schema is the JSON shape Claude answers in.
var schema = map[string]any{
	"type":                 "object",
	"additionalProperties": false,
	"required":             []string{"vendor", "purchaseDate", "receivedDate", "currency", "orderNumber", "items"},
	"properties": map[string]any{
		"vendor":       map[string]any{"type": "string", "description": "The shop that sold it"},
		"purchaseDate": map[string]any{"type": "string", "description": "When it was sold or the document issued; YYYY-MM-DD, or empty"},
		"receivedDate": map[string]any{"type": "string", "description": "When the buyer received the goods, if the document gives that date; YYYY-MM-DD, or empty"},
		"currency":     map[string]any{"type": "string", "description": "ISO 4217 code such as CZK or EUR, or empty"},
		"orderNumber":  map[string]any{"type": "string", "description": "Order or invoice number"},
		"items": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"productName", "brand", "model", "price", "warrantyMonths", "serialNumbers", "extension", "extendedMonths"},
				"properties": map[string]any{
					"productName":    map[string]any{"type": "string", "description": "What the product is called, without the brand, in the document's language, e.g. \"Myčka nádobí\" or \"Galaxy S24\""},
					"brand":          map[string]any{"type": "string"},
					"model":          map[string]any{"type": "string", "description": "Model name or number"},
					"price":          map[string]any{"anyOf": []any{map[string]any{"type": "number"}, map[string]any{"type": "null"}}, "description": "Price of one piece including tax"},
					"warrantyMonths": map[string]any{"anyOf": []any{map[string]any{"type": "integer"}, map[string]any{"type": "null"}}, "description": "Only if the document states the warranty length"},
					"serialNumbers":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"extension":      map[string]any{"type": "string", "description": "An extended warranty or warranty insurance bought for this product, as the document names it, or empty"},
					"extendedMonths": map[string]any{"anyOf": []any{map[string]any{"type": "integer"}, map[string]any{"type": "null"}}, "description": "Months that extension adds on top of the product's own warranty"},
				},
			},
		},
	},
}

// Which products to list: the ones worth a warranty, or every one.
const (
	worthyItems = `List each product on the document that's worth tracking a warranty for: devices, electronics, appliances, light bulbs, tools and the like. Leave out delivery, discounts, services, and things nobody claims a warranty on, such as food, books, music and films, decorations, textiles and household consumables. Go through every line: till slips shorten names, so watch for hints such as Matter, LED, USB or Wi-Fi. A product bought several times is one entry. When nothing qualifies, list just the main product.`
	everyItem   = `List every product bought on the document, whatever it is. Leave out only lines that aren't goods, such as delivery, discounts, fees and deposits. Go through every line: till slips shorten names. A product bought several times is one entry.`
)

const instructions = `You read receipts, invoices and warranty cards to fill in a warranty tracker.

%s

Keep the document's language; never translate. For the product name, use what people call the product: its model name when it's known by one (a phone, a laptop), otherwise a short description from the document such as "Rychlovarná konvice", without the brand and without long lists of features. Copy model numbers and serial numbers exactly as printed.

Give what one piece cost including tax; per-piece columns often leave the tax out, so add it or divide the line total by the quantity. Give the warranty length only when the document states it; don't assume a legal default. A warranty column on a line that isn't a product, such as a service or insurance, says nothing about the products. Leave anything the document doesn't show empty, or null for numbers.

An extended warranty or warranty insurance bought for a product (often a service line that names the product or its code) isn't a product of its own: put it on that product's entry, with the months it adds on top of the product's warranty. Its price isn't part of the product's price.

The purchase date is the date of sale, or the issue date if that's the only one. Give the date the buyer received the goods separately when the document states it, e.g. "Datum převzetí" or a delivery date; due dates and the like are neither.`

// prompt is the instructions, asking for every product or only the ones
// worth a warranty.
func prompt(every bool) string {
	if every {
		return fmt.Sprintf(instructions, everyItem)
	}
	return fmt.Sprintf(instructions, worthyItems)
}

// Reader reads receipts with Claude.
type Reader struct {
	client anthropic.Client
	model  string
}

func New(apiKey, model string) *Reader {
	return &Reader{client: anthropic.NewClient(option.WithAPIKey(apiKey)), model: model}
}

// Read extracts the products bought from a document, a PDF or an image:
// at least one, at most MaxItems. With every, it lists all products, not
// only the ones worth a warranty.
func (r *Reader) Read(ctx context.Context, data []byte, mimeType string, every bool) ([]Details, error) {
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
		System:    []anthropic.BetaTextBlockParam{{Text: prompt(every)}},
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(doc, anthropic.NewBetaTextBlock("Read this document.")),
		},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffortLow,
			Format: anthropic.BetaJSONOutputFormatParam{Schema: schema},
		},
		// A declined request is retried on the model's default fallback, where
		// it has one; otherwise this changes nothing.
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
	var a answer
	if err := json.Unmarshal([]byte(text.String()), &a); err != nil {
		return nil, fmt.Errorf("couldn't read Claude's answer: %w", err)
	}
	return a.details(), nil
}

// details gives each product the purchase's details: at least one, at most
// MaxItems.
func (a *answer) details() []Details {
	purchase := Details{Vendor: a.Vendor, PurchaseDate: a.PurchaseDate, Currency: a.Currency, OrderNumber: a.OrderNumber}
	if a.ReceivedDate != a.PurchaseDate {
		purchase.ReceivedDate = a.ReceivedDate
	}
	if len(a.Items) == 0 {
		return []Details{purchase}
	}
	var out []Details
	for _, it := range a.Items[:min(len(a.Items), MaxItems)] {
		d := purchase
		d.ProductName, d.Brand, d.Model, d.Price = it.ProductName, it.Brand, it.Model, it.Price
		d.WarrantyMonths, d.SerialNumbers = it.WarrantyMonths, it.SerialNumbers
		d.Extension, d.ExtendedMonths = it.Extension, it.ExtendedMonths
		out = append(out, d)
	}
	return out
}
