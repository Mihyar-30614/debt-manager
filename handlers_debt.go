package main

import (
	"fmt"
	"html"
	"log"
	"net/http"
	"strconv"
	"strings"
)


func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	
	searchQuery := r.URL.Query().Get("search")
	kindFilter := r.URL.Query().Get("kind")
	statusFilter := r.URL.Query().Get("status")
	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = "default"
	}
	
	userID := getUserID(r)
	debts, err := listDebtsFiltered(a.db, userID, searchQuery, kindFilter, statusFilter, sortBy)
	if err != nil {
		log.Printf("Error listing debts: %v", err)
		http.Error(w, "Internal server error", 500)
		return
	}

	var total int64
	var activeTotal int64
	activeDebts := make([]Debt, 0)
	for _, d := range debts {
		total += d.BalanceCents
		if d.Active {
			activeTotal += d.BalanceCents
			if d.BalanceCents > 0 {
				activeDebts = append(activeDebts, d)
			}
		}
	}
	// Only pass activeDebts if there are any (for showing the shortcut button)

	flash, flashType := a.getFlash(r)
	a.render(w, http.StatusOK, "index.html", map[string]any{
		"Debts":          debts,
		"ActiveDebts":    activeDebts,
		"Total":          total,
		"ActiveTotal":    activeTotal,
		"SearchQuery":    searchQuery,
		"KindFilter":     kindFilter,
		"StatusFilter":   statusFilter,
		"SortBy":         sortBy,
		"Flash":          flash,
		"FlashType":      flashType,
		"CSRFToken":      a.getCSRFToken(r),
		"ContentTemplate": "index_content",
	})
}

func (a *App) handleDebtNew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	a.render(w, http.StatusOK, "debt_new.html", map[string]any{
		"CSRFToken":      a.getCSRFToken(r),
		"ContentTemplate": "debt_new_content",
	})
}

func (a *App) handleDebtCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	name := r.FormValue("name")
	kind := r.FormValue("kind")
	balanceDollars := r.FormValue("balance_dollars")
	aprPercent := r.FormValue("apr_percent")
	minPayDollars := r.FormValue("min_payment_dollars")
	paymentDollars := r.FormValue("payment_dollars")
	dueDayStr := r.FormValue("due_day")

	// Very basic validation
	validKinds := map[string]bool{
		"card":          true,
		"line_of_credit": true,
		"personal_loan": true,
		"auto_loan":     true,
		"student_loan":  true,
		"mortgage":      true,
		"other_loan":    true,
	}
	if name == "" {
		a.setFlash(w, "Debt name is required.", true)
		http.Redirect(w, r, "/debts/new", http.StatusSeeOther)
		return
	}
	if !validKinds[kind] {
		a.setFlash(w, "Please select a valid debt type.", true)
		http.Redirect(w, r, "/debts/new", http.StatusSeeOther)
		return
	}

	balD, err := strconv.ParseFloat(balanceDollars, 64)
	if err != nil {
		a.setFlash(w, "Invalid balance amount. Please enter a valid number.", true)
		http.Redirect(w, r, "/debts/new", http.StatusSeeOther)
		return
	}
	if balD < 0 {
		a.setFlash(w, "Balance cannot be negative.", true)
		http.Redirect(w, r, "/debts/new", http.StatusSeeOther)
		return
	}
	aprP, err := strconv.ParseFloat(aprPercent, 64)
	if err != nil {
		a.setFlash(w, "Invalid APR. Please enter a valid number.", true)
		http.Redirect(w, r, "/debts/new", http.StatusSeeOther)
		return
	}
	if aprP < 0 {
		a.setFlash(w, "APR cannot be negative.", true)
		http.Redirect(w, r, "/debts/new", http.StatusSeeOther)
		return
	}
	minD, err := strconv.ParseFloat(minPayDollars, 64)
	if err != nil {
		a.setFlash(w, "Invalid minimum payment amount. Please enter a valid number.", true)
		http.Redirect(w, r, "/debts/new", http.StatusSeeOther)
		return
	}
	if minD < 0 {
		a.setFlash(w, "Minimum payment cannot be negative.", true)
		http.Redirect(w, r, "/debts/new", http.StatusSeeOther)
		return
	}
	payD := 0.0
	if paymentDollars != "" {
		payD, err = strconv.ParseFloat(paymentDollars, 64)
		if err != nil {
			a.setFlash(w, "Invalid payment amount. Please enter a valid number.", true)
			http.Redirect(w, r, "/debts/new", http.StatusSeeOther)
			return
		}
		if payD < 0 {
			a.setFlash(w, "Payment amount cannot be negative.", true)
			http.Redirect(w, r, "/debts/new", http.StatusSeeOther)
			return
		}
	}
	dueDay, err := parseInt(dueDayStr)
	if err != nil {
		a.setFlash(w, "Invalid due day. Please enter a number between 1 and 28.", true)
		http.Redirect(w, r, "/debts/new", http.StatusSeeOther)
		return
	}
	if dueDay < 1 || dueDay > 28 {
		a.setFlash(w, "Due day must be between 1 and 28.", true)
		http.Redirect(w, r, "/debts/new", http.StatusSeeOther)
		return
	}

	notes := html.EscapeString(strings.TrimSpace(r.FormValue("notes")))
	name = html.EscapeString(strings.TrimSpace(name))
	d := Debt{
		Name:            name,
		Kind:            kind,
		BalanceCents:    int64(balD * 100.0),
		APRBps:          int64(aprP * 100.0), // percent -> bps
		MinPaymentCents: int64(minD * 100.0),
		PaymentCents:    int64(payD * 100.0),
		DueDay:          dueDay,
		Notes:           notes,
	}
	userID := getUserID(r)
	_, err = createDebt(a.db, userID, d)
	if err != nil {
		log.Printf("Error creating debt: %v", err)
		a.setFlash(w, "Failed to create debt", true)
		http.Redirect(w, r, "/debts/new", http.StatusSeeOther)
		return
	}
	a.setFlash(w, "Debt created successfully", false)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) handleDebtView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	id, err := parseInt64(r.URL.Query().Get("id"))
	if err != nil {
		http.Error(w, "Invalid debt ID", 400)
		return
	}
	userID := getUserID(r)
	debt, err := getDebt(a.db, userID, id)
	if err != nil {
		log.Printf("Error getting debt: %v", err)
		http.Error(w, "Debt not found", 404)
		return
	}
	payments, err := listPaymentsForDebt(a.db, userID, id)
	if err != nil {
		log.Printf("Error listing payments: %v", err)
		http.Error(w, "Internal server error", 500)
		return
	}
	flash, flashType := a.getFlash(r)
	a.render(w, http.StatusOK, "debt_view.html", map[string]any{
		"Debt":           debt,
		"Payments":       payments,
		"Flash":          flash,
		"FlashType":      flashType,
		"CSRFToken":      a.getCSRFToken(r),
		"ContentTemplate": "debt_view_content",
	})
}

func (a *App) handleDebtEdit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	id, err := parseInt64(r.URL.Query().Get("id"))
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	userID := getUserID(r)
	debt, err := getDebt(a.db, userID, id)
	if err != nil {
		log.Printf("Error getting debt: %v", err)
		http.Error(w, "Debt not found", 404)
		return
	}
	flash, flashType := a.getFlash(r)
	a.render(w, http.StatusOK, "debt_edit.html", map[string]any{
		"Debt":           debt,
		"Flash":          flash,
		"FlashType":      flashType,
		"CSRFToken":      a.getCSRFToken(r),
		"ContentTemplate": "debt_edit_content",
	})
}

func (a *App) handleDebtUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	id, err := parseInt64(r.FormValue("id"))
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}

	name := html.EscapeString(strings.TrimSpace(r.FormValue("name")))
	kind := r.FormValue("kind")
	balanceDollars := r.FormValue("balance_dollars")
	aprPercent := r.FormValue("apr_percent")
	minPayDollars := r.FormValue("min_payment_dollars")
	paymentDollars := r.FormValue("payment_dollars")
	dueDayStr := r.FormValue("due_day")

	validKinds := map[string]bool{
		"card":           true,
		"line_of_credit": true,
		"personal_loan":  true,
		"auto_loan":      true,
		"student_loan":   true,
		"mortgage":       true,
		"other_loan":     true,
	}
	if name == "" {
		a.setFlash(w, "Debt name is required.", true)
		http.Redirect(w, r, fmt.Sprintf("/debts/edit?id=%d", id), http.StatusSeeOther)
		return
	}
	if !validKinds[kind] {
		a.setFlash(w, "Please select a valid debt type.", true)
		http.Redirect(w, r, fmt.Sprintf("/debts/edit?id=%d", id), http.StatusSeeOther)
		return
	}

	balD, err := strconv.ParseFloat(balanceDollars, 64)
	if err != nil {
		a.setFlash(w, "Invalid balance amount. Please enter a valid number.", true)
		http.Redirect(w, r, fmt.Sprintf("/debts/edit?id=%d", id), http.StatusSeeOther)
		return
	}
	if balD < 0 {
		a.setFlash(w, "Balance cannot be negative.", true)
		http.Redirect(w, r, fmt.Sprintf("/debts/edit?id=%d", id), http.StatusSeeOther)
		return
	}
	aprP, err := strconv.ParseFloat(aprPercent, 64)
	if err != nil {
		a.setFlash(w, "Invalid APR. Please enter a valid number.", true)
		http.Redirect(w, r, fmt.Sprintf("/debts/edit?id=%d", id), http.StatusSeeOther)
		return
	}
	if aprP < 0 {
		a.setFlash(w, "APR cannot be negative.", true)
		http.Redirect(w, r, fmt.Sprintf("/debts/edit?id=%d", id), http.StatusSeeOther)
		return
	}
	minD, err := strconv.ParseFloat(minPayDollars, 64)
	if err != nil {
		a.setFlash(w, "Invalid minimum payment amount. Please enter a valid number.", true)
		http.Redirect(w, r, fmt.Sprintf("/debts/edit?id=%d", id), http.StatusSeeOther)
		return
	}
	if minD < 0 {
		a.setFlash(w, "Minimum payment cannot be negative.", true)
		http.Redirect(w, r, fmt.Sprintf("/debts/edit?id=%d", id), http.StatusSeeOther)
		return
	}
	payD := 0.0
	if paymentDollars != "" {
		payD, err = strconv.ParseFloat(paymentDollars, 64)
		if err != nil {
			a.setFlash(w, "Invalid payment amount. Please enter a valid number.", true)
			http.Redirect(w, r, fmt.Sprintf("/debts/edit?id=%d", id), http.StatusSeeOther)
			return
		}
		if payD < 0 {
			a.setFlash(w, "Payment amount cannot be negative.", true)
			http.Redirect(w, r, fmt.Sprintf("/debts/edit?id=%d", id), http.StatusSeeOther)
			return
		}
	}
	dueDay, err := parseInt(dueDayStr)
	if err != nil {
		a.setFlash(w, "Invalid due day. Please enter a number between 1 and 28.", true)
		http.Redirect(w, r, fmt.Sprintf("/debts/edit?id=%d", id), http.StatusSeeOther)
		return
	}
	if dueDay < 1 || dueDay > 28 {
		a.setFlash(w, "Due day must be between 1 and 28.", true)
		http.Redirect(w, r, fmt.Sprintf("/debts/edit?id=%d", id), http.StatusSeeOther)
		return
	}

	notes := html.EscapeString(strings.TrimSpace(r.FormValue("notes")))
	name = html.EscapeString(strings.TrimSpace(name))
	d := Debt{
		ID:              id,
		Name:            name,
		Kind:            kind,
		BalanceCents:    int64(balD * 100.0),
		APRBps:          int64(aprP * 100.0),
		MinPaymentCents: int64(minD * 100.0),
		PaymentCents:    int64(payD * 100.0),
		DueDay:          dueDay,
		Notes:           notes,
	}
	userID := getUserID(r)
	if err := updateDebt(a.db, userID, d); err != nil {
		log.Printf("Error updating debt: %v", err)
		a.setFlash(w, "Failed to update debt", true)
		http.Redirect(w, r, fmt.Sprintf("/debts/edit?id=%d", id), http.StatusSeeOther)
		return
	}
	a.setFlash(w, "Debt updated successfully", false)
	http.Redirect(w, r, fmt.Sprintf("/debts/view?id=%d", id), http.StatusSeeOther)
}

func (a *App) handleDebtDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	id, err := parseInt64(r.FormValue("id"))
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	userID := getUserID(r)
	if err := deleteDebt(a.db, userID, id); err != nil {
		log.Printf("Error deleting debt: %v", err)
		a.setFlash(w, "Failed to delete debt", true)
		http.Redirect(w, r, fmt.Sprintf("/debts/view?id=%d", id), http.StatusSeeOther)
		return
	}
	a.setFlash(w, "Debt deleted successfully", false)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) handleDebtToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	id, err := parseInt64(r.FormValue("id"))
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	active := r.FormValue("active") == "1"
	userID := getUserID(r)
	if err := setDebtActive(a.db, userID, id, active); err != nil {
		log.Printf("Error toggling debt: %v", err)
		a.setFlash(w, "Failed to update debt status", true)
		http.Redirect(w, r, fmt.Sprintf("/debts/view?id=%d", id), http.StatusSeeOther)
		return
	}
	status := "closed"
	if active {
		status = "reopened"
	}
	a.setFlash(w, fmt.Sprintf("Debt %s successfully", status), false)
	http.Redirect(w, r, fmt.Sprintf("/debts/view?id=%d", id), http.StatusSeeOther)
}
