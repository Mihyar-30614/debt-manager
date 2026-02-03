package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)


// --- Budget handlers (full-scope personal budget, explicit debt link) ---

func (a *App) handleBudgetList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	userID := getUserID(r)
	budgets, err := listBudgets(a.db, userID, 24)
	if err != nil {
		log.Printf("Error listing budgets: %v", err)
		http.Error(w, "Internal server error", 500)
		return
	}
	yearStr := r.URL.Query().Get("year")
	monthStr := r.URL.Query().Get("month")
	year := time.Now().Year()
	month := int(time.Now().Month())
	if yearStr != "" {
		if y, err := strconv.Atoi(yearStr); err == nil && y >= 2000 && y <= 2100 {
			year = y
		}
	}
	if monthStr != "" {
		if m, err := strconv.Atoi(monthStr); err == nil && m >= 1 && m <= 12 {
			month = m
		}
	}
	flash, flashType := a.getFlash(r)
	a.render(w, http.StatusOK, "budget_list.html", map[string]any{
		"Budgets":         budgets,
		"Year":            year,
		"Month":           month,
		"Flash":           flash,
		"FlashType":       flashType,
		"CSRFToken":       a.getCSRFToken(r),
		"ContentTemplate": "budget_list_content",
	})
}

func (a *App) handleBudgetView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	userID := getUserID(r)
	yearStr := r.URL.Query().Get("year")
	monthStr := r.URL.Query().Get("month")
	now := time.Now()
	year := now.Year()
	month := int(now.Month())
	if yearStr != "" {
		if y, err := strconv.Atoi(yearStr); err == nil && y >= 2000 && y <= 2100 {
			year = y
		}
	}
	if monthStr != "" {
		if m, err := strconv.Atoi(monthStr); err == nil && m >= 1 && m <= 12 {
			month = m
		}
	}
	// Get or create budget with 0 income so user can edit
	budget, err := getOrCreateBudget(a.db, userID, year, month, 0)
	if err != nil {
		log.Printf("Error getOrCreateBudget: %v", err)
		http.Error(w, "Internal server error", 500)
		return
	}
	categories, err := listCategoriesForBudget(a.db, budget.ID, userID)
	if err != nil {
		log.Printf("Error listCategoriesForBudget: %v", err)
		http.Error(w, "Internal server error", 500)
		return
	}
	// Per-category spent and optional "suggested debt payoff" from plan
	type CatWithSpent struct {
		BudgetCategory
		SpentCents       int64
		SuggestedPayoffCents int64 // only for is_debt_payoff: plan suggestion (extra from plan)
	}
	catWithSpent := make([]CatWithSpent, 0, len(categories))
	minSum, _ := SumOfMinPaymentsForUser(a.db, userID)
	debts, _ := listDebts(a.db, userID)
	var suggestedExtra int64
	for _, c := range categories {
		spent, _ := totalSpentForCategory(a.db, c.ID)
		entry := CatWithSpent{BudgetCategory: c, SpentCents: spent, SuggestedPayoffCents: 0}
		if c.IsDebtPayoff {
			// Suggested extra = (income - sum of other category limits) - min payments, or use plan's "monthly budget" concept
			// We use: total income - sum of all category limits = "leftover"; plan suggests "monthly budget" - minSum = extra.
			// So show: "If you put your full income toward debt after categories, extra = income - sum(limits) - minSum". Simpler: show plan suggestion when we have a "monthly debt budget". Compute monthly debt budget = income - sum(limits of non-debt categories). Then extra = that - minSum.
			var totalAllocated int64
			for _, o := range categories {
				if !o.IsDebtPayoff {
					totalAllocated += o.LimitCents
				}
			}
			availableForDebt := budget.IncomeCents - totalAllocated
			if availableForDebt > minSum {
				suggestedExtra = availableForDebt - minSum
			}
			entry.SuggestedPayoffCents = suggestedExtra
		}
		catWithSpent = append(catWithSpent, entry)
	}
	// If no debt payoff category, compute suggested extra once (income - all limits - minSum)
	if budget.IncomeCents > 0 {
		var totalLimits int64
		for _, c := range categories {
			totalLimits += c.LimitCents
		}
		if totalLimits < budget.IncomeCents && minSum >= 0 {
			suggestedExtra = budget.IncomeCents - totalLimits - minSum
			if suggestedExtra < 0 {
				suggestedExtra = 0
			}
		}
	}
	flash, flashType := a.getFlash(r)
	a.render(w, http.StatusOK, "budget_view.html", map[string]any{
		"Budget":          budget,
		"Categories":      catWithSpent,
		"Debts":           debts,
		"MinPaymentsSum":  minSum,
		"SuggestedExtra":  suggestedExtra,
		"Flash":           flash,
		"FlashType":       flashType,
		"CSRFToken":       a.getCSRFToken(r),
		"ContentTemplate": "budget_view_content",
	})
}

func (a *App) handleBudgetUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	userID := getUserID(r)
	year, _ := strconv.Atoi(r.FormValue("year"))
	month, _ := strconv.Atoi(r.FormValue("month"))
	incomeStr := r.FormValue("income_cents")
	if year < 2000 || year > 2100 || month < 1 || month > 12 {
		a.setFlash(w, "Invalid year or month.", true)
		http.Redirect(w, r, fmt.Sprintf("/budget/view?year=%d&month=%d", year, month), http.StatusSeeOther)
		return
	}
	var incomeCents int64
	if incomeStr != "" {
		if c, err := strconv.ParseInt(incomeStr, 10, 64); err == nil && c >= 0 {
			incomeCents = c
		}
	}
	// Dollars input is more user-friendly; support both
	incomeDollars := r.FormValue("income_dollars")
	if incomeDollars != "" {
		if d, err := strconv.ParseFloat(incomeDollars, 64); err == nil && d >= 0 {
			incomeCents = int64(d * 100)
		}
	}
	budget, err := getOrCreateBudget(a.db, userID, year, month, incomeCents)
	if err != nil {
		log.Printf("Error getOrCreateBudget: %v", err)
		a.setFlash(w, "Error saving budget.", true)
		http.Redirect(w, r, fmt.Sprintf("/budget/view?year=%d&month=%d", year, month), http.StatusSeeOther)
		return
	}
	if err := updateBudget(a.db, userID, budget.ID, incomeCents); err != nil {
		log.Printf("Error updateBudget: %v", err)
		a.setFlash(w, "Error saving budget.", true)
		http.Redirect(w, r, fmt.Sprintf("/budget/view?year=%d&month=%d", year, month), http.StatusSeeOther)
		return
	}
	a.setFlash(w, "Budget updated.", false)
	http.Redirect(w, r, fmt.Sprintf("/budget/view?year=%d&month=%d", year, month), http.StatusSeeOther)
}

func (a *App) handleBudgetCategoryAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	userID := getUserID(r)
	budgetIDStr := r.URL.Query().Get("budget_id")
	if budgetIDStr == "" {
		http.Error(w, "budget_id required", 400)
		return
	}
	budgetID, _ := strconv.ParseInt(budgetIDStr, 10, 64)
	budget, err := getBudget(a.db, userID, budgetID)
	if err != nil {
		http.Error(w, "Budget not found", 404)
		return
	}
	categories, _ := listCategoriesForBudget(a.db, budget.ID, userID)
	sortOrder := len(categories)
	a.render(w, http.StatusOK, "budget_category_add.html", map[string]any{
		"Budget":          budget,
		"SortOrder":       sortOrder,
		"CSRFToken":       a.getCSRFToken(r),
		"ContentTemplate": "budget_category_add_content",
	})
}

func (a *App) handleBudgetCategoryCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	userID := getUserID(r)
	budgetID, _ := strconv.ParseInt(r.FormValue("budget_id"), 10, 64)
	name := strings.TrimSpace(r.FormValue("name"))
	limitDollars := r.FormValue("limit_dollars")
	isDebtPayoff := r.FormValue("is_debt_payoff") == "1"
	sortOrder, _ := strconv.Atoi(r.FormValue("sort_order"))
	var limitCents int64
	if d, err := strconv.ParseFloat(limitDollars, 64); err == nil && d >= 0 {
		limitCents = int64(d * 100)
	}
	if name == "" {
		a.setFlash(w, "Category name is required.", true)
		http.Redirect(w, r, fmt.Sprintf("/budget/category/add?budget_id=%d", budgetID), http.StatusSeeOther)
		return
	}
	budget, err := getBudget(a.db, userID, budgetID)
	if err != nil {
		http.Error(w, "Budget not found", 404)
		return
	}
	_, err = createBudgetCategory(a.db, userID, budget.ID, name, limitCents, isDebtPayoff, sortOrder)
	if err != nil {
		log.Printf("Error createBudgetCategory: %v", err)
		a.setFlash(w, "Error creating category.", true)
		http.Redirect(w, r, fmt.Sprintf("/budget/view?year=%d&month=%d", budget.Year, budget.Month), http.StatusSeeOther)
		return
	}
	a.setFlash(w, "Category added.", false)
	http.Redirect(w, r, fmt.Sprintf("/budget/view?year=%d&month=%d", budget.Year, budget.Month), http.StatusSeeOther)
}

func (a *App) handleBudgetCategoryEdit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	userID := getUserID(r)
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "id required", 400)
		return
	}
	id, _ := strconv.ParseInt(idStr, 10, 64)
	cat, err := getBudgetCategory(a.db, userID, id)
	if err != nil {
		http.Error(w, "Category not found", 404)
		return
	}
	budget, _ := getBudget(a.db, userID, cat.BudgetID)
	a.render(w, http.StatusOK, "budget_category_edit.html", map[string]any{
		"Category":        cat,
		"Budget":          budget,
		"CSRFToken":       a.getCSRFToken(r),
		"ContentTemplate": "budget_category_edit_content",
	})
}

func (a *App) handleBudgetCategoryUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	userID := getUserID(r)
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	name := strings.TrimSpace(r.FormValue("name"))
	limitDollars := r.FormValue("limit_dollars")
	isDebtPayoff := r.FormValue("is_debt_payoff") == "1"
	sortOrder, _ := strconv.Atoi(r.FormValue("sort_order"))
	var limitCents int64
	if d, err := strconv.ParseFloat(limitDollars, 64); err == nil && d >= 0 {
		limitCents = int64(d * 100)
	}
	cat, err := getBudgetCategory(a.db, userID, id)
	if err != nil {
		http.Error(w, "Category not found", 404)
		return
	}
	if name == "" {
		a.setFlash(w, "Category name is required.", true)
		http.Redirect(w, r, fmt.Sprintf("/budget/category/edit?id=%d", id), http.StatusSeeOther)
		return
	}
	if err := updateBudgetCategory(a.db, userID, id, name, limitCents, isDebtPayoff, sortOrder); err != nil {
		log.Printf("Error updateBudgetCategory: %v", err)
		a.setFlash(w, "Error updating category.", true)
		http.Redirect(w, r, fmt.Sprintf("/budget/category/edit?id=%d", id), http.StatusSeeOther)
		return
	}
	budget, _ := getBudget(a.db, userID, cat.BudgetID)
	a.setFlash(w, "Category updated.", false)
	http.Redirect(w, r, fmt.Sprintf("/budget/view?year=%d&month=%d", budget.Year, budget.Month), http.StatusSeeOther)
}

func (a *App) handleBudgetCategoryDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	userID := getUserID(r)
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	cat, err := getBudgetCategory(a.db, userID, id)
	if err != nil {
		http.Error(w, "Category not found", 404)
		return
	}
	if err := deleteBudgetCategory(a.db, userID, id); err != nil {
		log.Printf("Error deleteBudgetCategory: %v", err)
		a.setFlash(w, "Error deleting category.", true)
	} else {
		a.setFlash(w, "Category deleted.", false)
	}
	budget, _ := getBudget(a.db, userID, cat.BudgetID)
	http.Redirect(w, r, fmt.Sprintf("/budget/view?year=%d&month=%d", budget.Year, budget.Month), http.StatusSeeOther)
}

func (a *App) handleBudgetCategoryExpenses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	userID := getUserID(r)
	catIDStr := r.URL.Query().Get("category_id")
	if catIDStr == "" {
		http.Error(w, "category_id required", 400)
		return
	}
	catID, _ := strconv.ParseInt(catIDStr, 10, 64)
	cat, err := getBudgetCategory(a.db, userID, catID)
	if err != nil {
		http.Error(w, "Category not found", 404)
		return
	}
	budget, _ := getBudget(a.db, userID, cat.BudgetID)
	expenses, err := listExpensesForCategory(a.db, userID, catID)
	if err != nil {
		log.Printf("Error listExpensesForCategory: %v", err)
		http.Error(w, "Internal server error", 500)
		return
	}
	spent, _ := totalSpentForCategory(a.db, catID)
	flash, flashType := a.getFlash(r)
	a.render(w, http.StatusOK, "budget_category_expenses.html", map[string]any{
		"Category":        cat,
		"Budget":          budget,
		"Expenses":        expenses,
		"TotalSpentCents": spent,
		"Flash":           flash,
		"FlashType":       flashType,
		"CSRFToken":       a.getCSRFToken(r),
		"ContentTemplate": "budget_category_expenses_content",
	})
}

func (a *App) handleBudgetExpenseAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	userID := getUserID(r)
	catIDStr := r.URL.Query().Get("category_id")
	if catIDStr == "" {
		http.Error(w, "category_id required", 400)
		return
	}
	catID, _ := strconv.ParseInt(catIDStr, 10, 64)
	cat, err := getBudgetCategory(a.db, userID, catID)
	if err != nil {
		http.Error(w, "Category not found", 404)
		return
	}
	budget, _ := getBudget(a.db, userID, cat.BudgetID)
	a.render(w, http.StatusOK, "budget_expense_add.html", map[string]any{
		"Category":        cat,
		"Budget":          budget,
		"CSRFToken":       a.getCSRFToken(r),
		"ContentTemplate": "budget_expense_add_content",
	})
}

func (a *App) handleBudgetExpenseCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	userID := getUserID(r)
	catID, _ := strconv.ParseInt(r.FormValue("category_id"), 10, 64)
	spentOnStr := r.FormValue("spent_on")
	amountDollars := r.FormValue("amount_dollars")
	note := strings.TrimSpace(r.FormValue("note"))
	var spentOn time.Time
	if spentOnStr != "" {
		spentOn, _ = time.Parse("2006-01-02", spentOnStr)
	}
	if spentOn.IsZero() {
		spentOn = time.Now()
	}
	var amountCents int64
	if d, err := strconv.ParseFloat(amountDollars, 64); err == nil && d > 0 {
		amountCents = int64(d * 100)
	}
	if amountCents <= 0 {
		a.setFlash(w, "Amount must be greater than zero.", true)
		http.Redirect(w, r, fmt.Sprintf("/budget/expense/add?category_id=%d", catID), http.StatusSeeOther)
		return
	}
	cat, err := getBudgetCategory(a.db, userID, catID)
	if err != nil {
		http.Error(w, "Category not found", 404)
		return
	}
	if err := addBudgetExpense(a.db, userID, catID, spentOn, amountCents, note); err != nil {
		log.Printf("Error addBudgetExpense: %v", err)
		a.setFlash(w, "Error adding expense.", true)
		http.Redirect(w, r, fmt.Sprintf("/budget/expense/add?category_id=%d", catID), http.StatusSeeOther)
		return
	}
	budget, _ := getBudget(a.db, userID, cat.BudgetID)
	a.setFlash(w, "Expense recorded.", false)
	http.Redirect(w, r, fmt.Sprintf("/budget/view?year=%d&month=%d", budget.Year, budget.Month), http.StatusSeeOther)
}

func (a *App) handleBudgetExpenseEdit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	userID := getUserID(r)
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "id required", 400)
		return
	}
	id, _ := strconv.ParseInt(idStr, 10, 64)
	exp, err := getBudgetExpense(a.db, userID, id)
	if err != nil {
		http.Error(w, "Expense not found", 404)
		return
	}
	cat, _ := getBudgetCategory(a.db, userID, exp.BudgetCategoryID)
	budget, _ := getBudget(a.db, userID, cat.BudgetID)
	a.render(w, http.StatusOK, "budget_expense_edit.html", map[string]any{
		"Expense":         exp,
		"Category":        cat,
		"Budget":          budget,
		"CSRFToken":       a.getCSRFToken(r),
		"ContentTemplate": "budget_expense_edit_content",
	})
}

func (a *App) handleBudgetExpenseUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	userID := getUserID(r)
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	spentOnStr := r.FormValue("spent_on")
	amountDollars := r.FormValue("amount_dollars")
	note := strings.TrimSpace(r.FormValue("note"))
	var spentOn time.Time
	if spentOnStr != "" {
		spentOn, _ = time.Parse("2006-01-02", spentOnStr)
	}
	var amountCents int64
	if d, err := strconv.ParseFloat(amountDollars, 64); err == nil && d > 0 {
		amountCents = int64(d * 100)
	}
	exp, err := getBudgetExpense(a.db, userID, id)
	if err != nil {
		http.Error(w, "Expense not found", 404)
		return
	}
	if spentOn.IsZero() {
		spentOn = exp.SpentOn
	}
	if amountCents <= 0 {
		a.setFlash(w, "Amount must be greater than zero.", true)
		http.Redirect(w, r, fmt.Sprintf("/budget/expense/edit?id=%d", id), http.StatusSeeOther)
		return
	}
	if err := updateBudgetExpense(a.db, userID, id, spentOn, amountCents, note); err != nil {
		log.Printf("Error updateBudgetExpense: %v", err)
		a.setFlash(w, "Error updating expense.", true)
		http.Redirect(w, r, fmt.Sprintf("/budget/expense/edit?id=%d", id), http.StatusSeeOther)
		return
	}
	cat, _ := getBudgetCategory(a.db, userID, exp.BudgetCategoryID)
	budget, _ := getBudget(a.db, userID, cat.BudgetID)
	a.setFlash(w, "Expense updated.", false)
	http.Redirect(w, r, fmt.Sprintf("/budget/view?year=%d&month=%d", budget.Year, budget.Month), http.StatusSeeOther)
}

func (a *App) handleBudgetExpenseDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	userID := getUserID(r)
	id, _ := strconv.ParseInt(r.FormValue("id"), 10, 64)
	exp, err := getBudgetExpense(a.db, userID, id)
	if err != nil {
		http.Error(w, "Expense not found", 404)
		return
	}
	if err := deleteBudgetExpense(a.db, userID, id); err != nil {
		log.Printf("Error deleteBudgetExpense: %v", err)
		a.setFlash(w, "Error deleting expense.", true)
	} else {
		a.setFlash(w, "Expense deleted.", false)
	}
	cat, _ := getBudgetCategory(a.db, userID, exp.BudgetCategoryID)
	budget, _ := getBudget(a.db, userID, cat.BudgetID)
	http.Redirect(w, r, fmt.Sprintf("/budget/view?year=%d&month=%d", budget.Year, budget.Month), http.StatusSeeOther)
}
