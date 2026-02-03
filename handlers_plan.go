package main

import (
	"log"
	"net/http"
	"strconv"
	"time"
)


func (a *App) handlePlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	userID := getUserID(r)
	debts, err := listDebts(a.db, userID)
	if err != nil {
		log.Printf("Error listing debts: %v", err)
		http.Error(w, "Internal server error", 500)
		return
	}

	budgetDollarsStr := r.URL.Query().Get("budget_dollars")
	strategyStr := r.URL.Query().Get("strategy")
	if strategyStr == "" {
		strategyStr = string(Avalanche)
	}
	if budgetDollarsStr == "" {
		budgetDollarsStr = "500"
	}
	budgetD, _ := strconv.ParseFloat(budgetDollarsStr, 64)
	monthlyBudgetCents := int64(budgetD * 100.0)

	strategy := Strategy(strategyStr)
	if strategy != Snowball && strategy != Avalanche {
		strategy = Avalanche
	}

	plan := GeneratePlan(debts, monthlyBudgetCents, strategy, 240) // up to 20 years

	// Create a map of debt ID to debt for easy lookup in template
	debtMap := make(map[int64]Debt)
	// Track which debts are in the plan (active with initial balance > 0)
	debtsInPlan := make(map[int64]bool)
	for _, d := range debts {
		debtMap[d.ID] = d
		if d.Active && d.BalanceCents > 0 {
			debtsInPlan[d.ID] = true
		}
	}

	// Explicit budget link: suggested monthly budget from current month's budget (income − non-debt category limits)
	now := time.Now()
	budgetSuggestedCents := int64(0)
	if b, err := getBudgetByYearMonth(a.db, userID, now.Year(), int(now.Month())); err == nil && b.IncomeCents > 0 {
		cats, _ := listCategoriesForBudget(a.db, b.ID, userID)
		var nonDebtTotal int64
		for _, c := range cats {
			if !c.IsDebtPayoff {
				nonDebtTotal += c.LimitCents
			}
		}
		if b.IncomeCents > nonDebtTotal {
			budgetSuggestedCents = b.IncomeCents - nonDebtTotal
		}
	}

	a.render(w, http.StatusOK, "plan.html", map[string]any{
		"Debts":                debts,
		"DebtMap":              debtMap,
		"DebtsInPlan":          debtsInPlan,
		"MonthlyBudgetCents":    monthlyBudgetCents,
		"Strategy":             strategy,
		"Plan":                 plan,
		"BudgetSuggestedCents": budgetSuggestedCents,
		"CSRFToken":            a.getCSRFToken(r),
		"ContentTemplate":      "plan_content",
	})
}

