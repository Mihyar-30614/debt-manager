package main

import (
	"net/http"
	"strconv"
)

func (a *App) handleTaxBrackets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}

	province := r.URL.Query().Get("province")
	if province == "" {
		province = "ON"
	}
	if _, ok := provinceBrackets[province]; !ok {
		province = "ON"
	}

	incomeStr := r.URL.Query().Get("income")
	incomeFilled := r.URL.Query().Has("income")
	incomeCents := int64(0)
	if incomeStr != "" {
		if f, err := strconv.ParseFloat(incomeStr, 64); err == nil && f >= 0 {
			incomeCents = int64(f * 100)
		}
	}

	fills, totalTaxCents := ComputeBracketFills(province, incomeCents)
	if fills == nil {
		fills = []BracketFill{}
	}

	provincesList := make([]struct{ Code, Name string }, 0, len(provinceNames))
	for _, code := range []string{"ON", "BC", "AB", "QC", "SK", "MB", "NS", "NB", "NL", "PE", "NT", "NU", "YT"} {
		if name, ok := provinceNames[code]; ok {
			provincesList = append(provincesList, struct{ Code, Name string }{code, name})
		}
	}

	a.render(w, http.StatusOK, "tax_brackets.html", map[string]any{
		"Provinces":      provincesList,
		"Province":       province,
		"ProvinceName":   provinceNames[province],
		"IncomeCents":    incomeCents,
		"IncomeDollars":  incomeCents / 100,
		"IncomeFilled":   incomeFilled,
		"Fills":          fills,
		"TotalTaxCents":  totalTaxCents,
		"TaxYear":        "2025",
		"CSRFToken":      a.getCSRFToken(r),
		"ContentTemplate": "tax_brackets_content",
	})
}
