package main

import (
	"fmt"
	"html"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)


func (a *App) handlePaymentAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	debtID, err := parseInt64(r.FormValue("debt_id"))
	if err != nil {
		http.Error(w, "bad debt id", 400)
		return
	}
	paidOn, err := time.Parse("2006-01-02", r.FormValue("paid_on"))
	if err != nil {
		http.Error(w, "bad date", 400)
		return
	}
	amtD, err := strconv.ParseFloat(r.FormValue("amount_dollars"), 64)
	if err != nil || amtD <= 0 {
		http.Error(w, "bad amount", 400)
		return
	}
	note := html.EscapeString(strings.TrimSpace(r.FormValue("note")))

	userID := getUserID(r)
	if err := addPayment(a.db, userID, debtID, paidOn, int64(amtD*100.0), note); err != nil {
		log.Printf("Error adding payment: %v", err)
		a.setFlash(w, "Failed to add payment", true)
		// Redirect back to payment form or debt view depending on referrer
		redirectTo := r.FormValue("redirect_to")
		if redirectTo == "payments" {
			http.Redirect(w, r, "/payments/new", http.StatusSeeOther)
		} else {
			http.Redirect(w, r, fmt.Sprintf("/debts/view?id=%d", debtID), http.StatusSeeOther)
		}
		return
	}
	a.setFlash(w, "Payment recorded. The debt balance has been updated.", false)
	redirectTo := r.FormValue("redirect_to")
	if redirectTo == "payments" {
		http.Redirect(w, r, "/payments", http.StatusSeeOther)
	} else {
		http.Redirect(w, r, fmt.Sprintf("/debts/view?id=%d", debtID), http.StatusSeeOther)
	}
}

func (a *App) handlePaymentEdit(w http.ResponseWriter, r *http.Request) {
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
	payment, err := getPayment(a.db, userID, id)
	if err != nil {
		log.Printf("Error getting payment: %v", err)
		http.Error(w, "Payment not found", 404)
		return
	}
	debt, err := getDebt(a.db, userID, payment.DebtID)
	if err != nil {
		log.Printf("Error getting debt: %v", err)
		http.Error(w, "Debt not found", 404)
		return
	}
	flash, flashType := a.getFlash(r)
	a.render(w, http.StatusOK, "payment_edit.html", map[string]any{
		"Payment":        payment,
		"Debt":          debt,
		"Flash":          flash,
		"FlashType":      flashType,
		"CSRFToken":      a.getCSRFToken(r),
		"ContentTemplate": "payment_edit_content",
	})
}

func (a *App) handlePaymentUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	paymentID, err := parseInt64(r.FormValue("id"))
	if err != nil {
		http.Error(w, "bad payment id", 400)
		return
	}
	paidOn, err := time.Parse("2006-01-02", r.FormValue("paid_on"))
	if err != nil {
		a.setFlash(w, "Invalid date", true)
		http.Redirect(w, r, fmt.Sprintf("/payments/edit?id=%d", paymentID), http.StatusSeeOther)
		return
	}
	amtD, err := strconv.ParseFloat(r.FormValue("amount_dollars"), 64)
	if err != nil || amtD <= 0 {
		a.setFlash(w, "Invalid amount", true)
		http.Redirect(w, r, fmt.Sprintf("/payments/edit?id=%d", paymentID), http.StatusSeeOther)
		return
	}
	note := html.EscapeString(strings.TrimSpace(r.FormValue("note")))

	userID := getUserID(r)
	payment, err := getPayment(a.db, userID, paymentID)
	if err != nil {
		log.Printf("Error getting payment: %v", err)
		http.Error(w, "Payment not found", 404)
		return
	}

	if err := updatePayment(a.db, userID, paymentID, paidOn, int64(amtD*100.0), note); err != nil {
		log.Printf("Error updating payment: %v", err)
		a.setFlash(w, "Failed to update payment", true)
		http.Redirect(w, r, fmt.Sprintf("/payments/edit?id=%d", paymentID), http.StatusSeeOther)
		return
	}
	a.setFlash(w, "Payment updated. Balance has been recalculated.", false)
	http.Redirect(w, r, fmt.Sprintf("/debts/view?id=%d", payment.DebtID), http.StatusSeeOther)
}

func (a *App) handlePaymentDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	paymentID, err := parseInt64(r.FormValue("id"))
	if err != nil {
		http.Error(w, "bad payment id", 400)
		return
	}
	userID := getUserID(r)
	payment, err := getPayment(a.db, userID, paymentID)
	if err != nil {
		log.Printf("Error getting payment: %v", err)
		http.Error(w, "Payment not found", 404)
		return
	}
	debtID := payment.DebtID
	if err := deletePayment(a.db, userID, paymentID); err != nil {
		log.Printf("Error deleting payment: %v", err)
		a.setFlash(w, "Failed to delete payment", true)
		http.Redirect(w, r, fmt.Sprintf("/debts/view?id=%d", debtID), http.StatusSeeOther)
		return
	}
	a.setFlash(w, "Payment removed. The debt balance has been adjusted.", false)
	http.Redirect(w, r, fmt.Sprintf("/debts/view?id=%d", debtID), http.StatusSeeOther)
}

func (a *App) handlePaymentNew(w http.ResponseWriter, r *http.Request) {
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
	// Get only active debts with balance > 0
	activeDebts := make([]Debt, 0)
	for _, d := range debts {
		if d.Active && d.BalanceCents > 0 {
			activeDebts = append(activeDebts, d)
		}
	}
	flash, flashType := a.getFlash(r)
	a.render(w, http.StatusOK, "payment_new.html", map[string]any{
		"ActiveDebts":    activeDebts,
		"Flash":          flash,
		"FlashType":      flashType,
		"CSRFToken":      a.getCSRFToken(r),
		"ContentTemplate": "payment_new_content",
	})
}

func (a *App) handlePayments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	userID := getUserID(r)
	payments, err := listAllPayments(a.db, userID)
	if err != nil {
		log.Printf("Error listing payments: %v", err)
		http.Error(w, "Internal server error", 500)
		return
	}
	paymentsThisMonthCount, paymentsThisMonthTotal, _ := PaymentsThisMonth(a.db, userID)
	flash, flashType := a.getFlash(r)
	a.render(w, http.StatusOK, "payments.html", map[string]any{
		"Payments":                payments,
		"PaymentsThisMonthCount":  paymentsThisMonthCount,
		"PaymentsThisMonthTotal":  paymentsThisMonthTotal,
		"Flash":                   flash,
		"FlashType":               flashType,
		"CSRFToken":               a.getCSRFToken(r),
		"ContentTemplate":         "payments_content",
	})
}
