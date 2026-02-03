package main

import (
	"math"
	"sort"
)

type Strategy string

const (
	Snowball  Strategy = "snowball"  // smallest balance first
	Avalanche Strategy = "avalanche" // highest APR first
)

type PlanMonth struct {
	MonthIndex     int
	InterestCents  int64
	Payments       map[int64]int64 // debtID -> paid cents this month
	Balances       map[int64]int64 // end-of-month balances
	TotalPaidCents int64
}

type PlanResult struct {
	Months             []PlanMonth
	TotalInterestCents int64
	PayoffMonths       int
}

func monthlyRate(aprBps int64) float64 {
	apr := float64(aprBps) / 10000.0
	return apr / 12.0
}

func roundToCents(x float64) int64 {
	return int64(math.Round(x))
}

func GeneratePlan(debts []Debt, monthlyBudgetCents int64, strategy Strategy, maxMonths int) PlanResult {
	// Filter active with positive balance
	active := make([]Debt, 0, len(debts))
	for _, d := range debts {
		if d.Active && d.BalanceCents > 0 {
			active = append(active, d)
		}
	}
	// Working balances
	bal := map[int64]int64{}
	for _, d := range active {
		bal[d.ID] = d.BalanceCents
	}

	pickOrder := func() []Debt {
		cp := make([]Debt, 0, len(active))
		for _, d := range active {
			if bal[d.ID] > 0 {
				cp = append(cp, d)
			}
		}
		switch strategy {
		case Snowball:
			sort.Slice(cp, func(i, j int) bool {
				if bal[cp[i].ID] == bal[cp[j].ID] {
					return cp[i].APRBps > cp[j].APRBps
				}
				return bal[cp[i].ID] < bal[cp[j].ID]
			})
		default: // Avalanche
			sort.Slice(cp, func(i, j int) bool {
				if cp[i].APRBps == cp[j].APRBps {
					return bal[cp[i].ID] < bal[cp[j].ID]
				}
				return cp[i].APRBps > cp[j].APRBps
			})
		}
		return cp
	}

	var res PlanResult
	for m := 1; m <= maxMonths; m++ {
		// Check done
		done := true
		for _, d := range active {
			if bal[d.ID] > 0 {
				done = false
				break
			}
		}
		if done {
			res.PayoffMonths = m - 1
			return res
		}

		month := PlanMonth{
			MonthIndex: m,
			Payments:   map[int64]int64{},
			Balances:   map[int64]int64{},
		}

		// 1) Accrue monthly interest on remaining balances
		var monthInterest int64
		for _, d := range active {
			b := bal[d.ID]
			if b <= 0 {
				continue
			}
			r := monthlyRate(d.APRBps)
			interest := roundToCents(float64(b) * r)
			if interest < 0 {
				interest = 0
			}
			bal[d.ID] += interest
			monthInterest += interest
		}
		month.InterestCents = monthInterest
		res.TotalInterestCents += monthInterest

		// 2) Pay minimums
		remaining := monthlyBudgetCents
		for _, d := range active {
			if bal[d.ID] <= 0 {
				continue
			}
			minPay := d.MinPaymentCents
			if minPay > remaining {
				minPay = remaining
			}
			if minPay > bal[d.ID] {
				minPay = bal[d.ID]
			}
			if minPay > 0 {
				bal[d.ID] -= minPay
				month.Payments[d.ID] += minPay
				month.TotalPaidCents += minPay
				remaining -= minPay
			}
		}

		// 3) Apply remaining to target debt by strategy, looping as debts are paid off
		for remaining > 0 {
			order := pickOrder()
			if len(order) == 0 {
				break
			}
			t := order[0]
			if bal[t.ID] <= 0 {
				continue
			}
			pay := remaining
			if pay > bal[t.ID] {
				pay = bal[t.ID]
			}
			bal[t.ID] -= pay
			month.Payments[t.ID] += pay
			month.TotalPaidCents += pay
			remaining -= pay
		}

		for _, d := range active {
			month.Balances[d.ID] = bal[d.ID]
		}
		res.Months = append(res.Months, month)
	}

	// If we hit maxMonths
	res.PayoffMonths = maxMonths
	return res
}
