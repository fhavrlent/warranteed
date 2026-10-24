package api

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fhavrlent/warranteed/internal/store"
)

// CSV columns, in export order. Lists (serial numbers, tags) are separated
// by listSep. Import reads the same format; only product_name and
// purchase_date are required.
var csvHeader = []string{
	"product_name", "brand", "model", "vendor", "order_number", "purchase_date",
	"warranty_months", "warranty_end_date", "lifetime", "price", "currency",
	"serial_numbers", "tags", "product_url", "notes", "archived", "space",
}

const listSep = ";"

// Spreadsheet apps run cells starting with these as formulas. Exported text
// gets a leading apostrophe (shown as text, not printed), which import strips.
const formulaChars = "=+-@\t\r"

func escapeCell(v string) string {
	// Also escape text that already looks escaped, so import restores it as is.
	if v != "" && (strings.ContainsRune(formulaChars, rune(v[0])) || unescapeCell(v) != v) {
		return "'" + v
	}
	return v
}

// unescapeCell reverses escapeCell: one apostrophe is removed from a value
// that, without it, starts with a formula character or is itself escaped.
func unescapeCell(v string) string {
	if len(v) > 1 && v[0] == '\'' {
		rest := v[1:]
		if strings.ContainsRune(formulaChars, rune(rest[0])) || unescapeCell(rest) != rest {
			return rest
		}
	}
	return v
}

func (s *Server) exportCSV(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListWarranties(r.Context(), userID(r))
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="warranties-%s.csv"`, time.Now().Format("2006-01-02")))
	cw := csv.NewWriter(w)
	cw.Write(csvHeader)
	for _, wr := range list {
		tags := make([]string, len(wr.Tags))
		for i, t := range wr.Tags {
			tags[i] = t.Name
		}
		months, end, price := "", "", ""
		if wr.WarrantyMonths != nil {
			months = strconv.Itoa(*wr.WarrantyMonths)
		}
		if wr.WarrantyEndDate != nil {
			end = *wr.WarrantyEndDate
		}
		if wr.Price != nil {
			price = strconv.FormatFloat(*wr.Price, 'f', -1, 64)
		}
		e := escapeCell
		cw.Write([]string{
			e(wr.ProductName), e(wr.Brand), e(wr.Model), e(wr.Vendor), e(wr.OrderNumber), wr.PurchaseDate,
			months, end, strconv.FormatBool(wr.IsLifetime), price, e(wr.Currency),
			e(strings.Join(wr.SerialNumbers, listSep)), e(strings.Join(tags, listSep)),
			e(wr.ProductURL), e(wr.Notes), strconv.FormatBool(wr.ArchivedAt != nil), wr.SpaceKind,
		})
	}
	cw.Flush()
}

type importResult struct {
	Imported int           `json:"imported"`
	Skipped  int           `json:"skipped"`
	Errors   []importError `json:"errors"`
}

type importError struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}

func (s *Server) importCSV(w http.ResponseWriter, r *http.Request) {
	uid := userID(r)
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Choose a CSV file to import")
		return
	}
	defer file.Close()
	st, err := s.store.Settings(r.Context(), uid)
	if err != nil {
		fail(w, err)
		return
	}

	cr := csv.NewReader(file)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	header, err := cr.Read()
	if err != nil {
		writeError(w, http.StatusBadRequest, "The file is empty or not a CSV")
		return
	}
	col := map[string]int{}
	for i, h := range header {
		// Spreadsheet apps often prepend a byte-order mark.
		h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")))
		col[h] = i
	}
	for _, required := range []string{"product_name", "purchase_date"} {
		if _, ok := col[required]; !ok {
			writeError(w, http.StatusBadRequest, "Missing column "+required)
			return
		}
	}

	// Skip rows that already exist (same name and purchase date) so importing
	// the same file twice is harmless.
	existing, err := s.store.ListWarranties(r.Context(), uid)
	if err != nil {
		fail(w, err)
		return
	}
	// "space" is personal or household; anything else (or no column) means household.
	spaces, err := s.store.ListSpaces(r.Context(), uid)
	if err != nil {
		fail(w, err)
		return
	}
	spaceByKind := map[string]int64{}
	for _, sp := range spaces {
		spaceByKind[sp.Kind] = sp.ID
	}

	seen := map[string]bool{}
	for _, wr := range existing {
		seen[strings.ToLower(wr.ProductName)+"|"+wr.PurchaseDate] = true
	}

	res := importResult{Errors: []importError{}}
	rowNum := 1
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		rowNum++
		if err != nil {
			res.Errors = append(res.Errors, importError{rowNum, "Could not parse row: " + err.Error()})
			continue
		}
		get := func(name string) string {
			if i, ok := col[name]; ok && i < len(rec) {
				return unescapeCell(strings.TrimSpace(rec[i]))
			}
			return ""
		}
		in, archived, msg := rowToInput(get, st)
		if msg != "" {
			res.Errors = append(res.Errors, importError{rowNum, msg})
			continue
		}
		if id, ok := spaceByKind[strings.ToLower(get("space"))]; ok {
			in.SpaceID = &id
		}
		key := strings.ToLower(in.ProductName) + "|" + in.PurchaseDate
		if seen[key] {
			res.Skipped++
			continue
		}
		id, err := s.store.CreateWarranty(r.Context(), uid, in)
		if err != nil {
			if ve, ok := err.(*store.ValidationError); ok {
				res.Errors = append(res.Errors, importError{rowNum, ve.Msg})
				continue
			}
			fail(w, err)
			return
		}
		if archived {
			s.store.SetArchived(r.Context(), uid, id, true)
		}
		seen[key] = true
		res.Imported++
	}
	writeJSON(w, 200, res)
}

func rowToInput(get func(string) string, st *store.Settings) (*store.WarrantyInput, bool, string) {
	in := &store.WarrantyInput{
		ProductName: get("product_name"),
		Brand:       get("brand"),
		Model:       get("model"),
		Vendor:      get("vendor"),
		OrderNumber: get("order_number"),
		Currency:    get("currency"),
		ProductURL:  get("product_url"),
		Notes:       get("notes"),
		IsLifetime:  parseBool(get("lifetime")),
	}
	if in.Currency == "" {
		in.Currency = st.Currency
	}
	purchase, ok := parseDate(get("purchase_date"), st.DateFormat)
	if !ok {
		return nil, false, fmt.Sprintf("Unrecognised purchase_date %q", get("purchase_date"))
	}
	in.PurchaseDate = purchase

	if p := get("price"); p != "" {
		v, err := strconv.ParseFloat(strings.ReplaceAll(p, ",", "."), 64)
		if err != nil {
			return nil, false, fmt.Sprintf("Invalid price %q", p)
		}
		in.Price = &v
	}
	in.SerialNumbers = splitList(get("serial_numbers"))
	in.Tags = splitList(get("tags"))

	if !in.IsLifetime {
		if m := get("warranty_months"); m != "" {
			n, err := strconv.Atoi(m)
			if err != nil {
				return nil, false, fmt.Sprintf("Invalid warranty_months %q", m)
			}
			in.WarrantyMonths = &n
		} else if e := get("warranty_end_date"); e != "" {
			end, ok := parseDate(e, st.DateFormat)
			if !ok {
				return nil, false, fmt.Sprintf("Unrecognised warranty_end_date %q", e)
			}
			in.WarrantyEndDate = &end
		} else {
			return nil, false, "Needs warranty_months, warranty_end_date, or lifetime=true"
		}
	}
	return in, parseBool(get("archived")), ""
}

func parseBool(s string) bool {
	switch strings.ToLower(s) {
	case "true", "yes", "1", "y":
		return true
	}
	return false
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, listSep)
}

// parseDate accepts ISO dates (what export writes) plus the user's own date
// format and its common variants, since spreadsheet apps rewrite dates.
func parseDate(s, userFormat string) (string, bool) {
	if s == "" {
		return "", false
	}
	if len(s) > 10 && (s[10] == 'T' || s[10] == ' ') {
		s = s[:10]
	}
	layouts := []string{"2006-01-02", "2006/01/02"}
	if strings.HasPrefix(userFormat, "MM/") {
		layouts = append(layouts, "01/02/2006", "1/2/2006")
	} else {
		layouts = append(layouts, "02.01.2006", "2.1.2006", "2. 1. 2006", "02/01/2006", "2/1/2006")
	}
	layouts = append(layouts, "2 Jan 2006", "Jan 2, 2006", "2 January 2006", "January 2, 2006")
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.Format(store.DateLayout), true
		}
	}
	return "", false
}
