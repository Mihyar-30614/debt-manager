package main

import "fmt"

// TaxBracket represents one combined (federal + provincial) tax bracket.
// MaxCents is the top of the bracket in cents; Rate is e.g. 19.55 for 19.55%.
type TaxBracket struct {
	MaxCents int64   // upper bound of this bracket (cumulative)
	RatePct  float64 // marginal rate as percentage, e.g. 19.55
}

// BracketFill is one bucket in the visualization: income in this bracket and tax on it.
type BracketFill struct {
	Label                string  // e.g. "$0 – $52,886"
	RatePct              float64
	IncomeInBracketCents int64
	TaxCents             int64
	FullBracketCents     int64
	FillPct              float64 // 0–100 for CSS width
}

// Province brackets: combined federal + provincial marginal rates (other income), 2025.
// Source: TaxTips.ca and CRA. Amounts in cents.
var provinceBrackets = map[string][]TaxBracket{
	"ON": {
		{5288600, 19.55},
		{5737500, 23.65},
		{9313200, 29.65},
		{10577500, 31.48},
		{10972700, 33.89},
		{11475000, 37.91},
		{15000000, 43.41},
		{17788200, 44.97},
		{22000000, 48.28},
		{25341400, 49.84},
		{9999999900, 53.53}, // top bracket
	},
	"BC": {
		{4927900, 19.56},   // $0–$49,279
		{9856000, 28.20},   // up to $98,560
		{11315800, 31.00},
		{13740700, 32.79},
		{18630600, 38.29},
		{25982900, 49.80},
		{9999999900, 53.50},
	},
	"AB": {
		{142292_00, 25.00},
		{170751_00, 30.50},
		{227668_00, 36.00},
		{341502_00, 38.00},
		{9999999900, 48.00},
	},
	"QC": {
		{51425_00, 27.53},
		{102865_00, 32.53},
		{119545_00, 37.12},
		{9999999900, 45.71},
	},
	"SK": {
		{52057_00, 25.50},
		{148734_00, 32.50},
		{9999999900, 35.50},
	},
	"MB": {
		{47000_00, 25.80},
		{100000_00, 27.75},
		{9999999900, 33.25},
	},
	"NS": {
		{29590_00, 23.79},
		{59180_00, 30.00},
		{93000_00, 31.00},
		{150000_00, 34.67},
		{9999999900, 39.00},
	},
	"NB": {
		{47715_00, 24.20},
		{95431_00, 31.32},
		{176756_00, 34.32},
		{9999999900, 36.84},
	},
	"NL": {
		{43198_00, 23.70},
		{86395_00, 30.50},
		{154244_00, 33.80},
		{215000_00, 36.50},
		{9999999900, 39.50},
	},
	"PE": {
		{31984_00, 23.75},
		{63969_00, 30.25},
		{9999999900, 33.25},
	},
	"NT": {
		{50897_00, 19.90},
		{101792_00, 26.40},
		{165429_00, 29.90},
		{235675_00, 33.40},
		{9999999900, 36.90},
	},
	"NU": {
		{50897_00, 19.90},
		{101792_00, 26.40},
		{165429_00, 29.90},
		{235675_00, 33.40},
		{9999999900, 36.90},
	},
	"YT": {
		{55867_00, 19.05},
		{111733_00, 25.55},
		{173205_00, 31.05},
		{246752_00, 34.37},
		{9999999900, 37.70},
	},
}

var provinceNames = map[string]string{
	"ON": "Ontario",
	"BC": "British Columbia",
	"AB": "Alberta",
	"QC": "Quebec",
	"SK": "Saskatchewan",
	"MB": "Manitoba",
	"NS": "Nova Scotia",
	"NB": "New Brunswick",
	"NL": "Newfoundland and Labrador",
	"PE": "Prince Edward Island",
	"NT": "Northwest Territories",
	"NU": "Nunavut",
	"YT": "Yukon",
}

// ComputeBracketFills returns a slice of BracketFill for the given province and income (cents).
// Total income and total tax are also computed.
func ComputeBracketFills(province string, incomeCents int64) (fills []BracketFill, totalTaxCents int64) {
	brackets, ok := provinceBrackets[province]
	if !ok {
		return nil, 0
	}
	var prev int64
	for _, b := range brackets {
		bandTop := b.MaxCents
		isTopBracket := bandTop > 9999990000
		if isTopBracket {
			bandTop = incomeCents + 1
		}
		fullBracketSize := bandTop - prev
		incomeInBracket := fullBracketSize
		if incomeCents < bandTop {
			incomeInBracket = incomeCents - prev
			if incomeInBracket < 0 {
				incomeInBracket = 0
			}
			// For top bracket (no real cap), show bar 100% full for the income in it
			if isTopBracket && incomeInBracket > 0 {
				fullBracketSize = incomeInBracket
			}
		}
		taxInBracket := int64(float64(incomeInBracket) * (b.RatePct / 100.0))
		totalTaxCents += taxInBracket

		fillPct := 100.0
		if fullBracketSize > 0 {
			fillPct = float64(incomeInBracket) / float64(fullBracketSize) * 100
		}
		label := formatBracketLabel(prev, b.MaxCents)
		fills = append(fills, BracketFill{
			Label:                label,
			RatePct:              b.RatePct,
			IncomeInBracketCents: incomeInBracket,
			TaxCents:             taxInBracket,
			FullBracketCents:     fullBracketSize,
			FillPct:              fillPct,
		})
		prev = bandTop
		if incomeCents < bandTop {
			break
		}
	}
	return fills, totalTaxCents
}

func formatBracketLabel(low, high int64) string {
	if high > 9999990000 {
		return fmt.Sprintf("Over $%s", formatDollars(low))
	}
	return fmt.Sprintf("$%s – $%s", formatDollars(low), formatDollars(high))
}

func formatDollars(cents int64) string {
	d := cents / 100
	if d >= 1000 {
		return fmt.Sprintf("%d,%03d", d/1000, d%1000)
	}
	return fmt.Sprintf("%d", d)
}
