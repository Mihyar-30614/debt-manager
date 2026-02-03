package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

func (a *App) handleSignup(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		flash, flashType := a.getFlash(r)
		a.render(w, http.StatusOK, "signup.html", map[string]any{
			"Flash":          flash,
			"FlashType":      flashType,
			"ContentTemplate": "signup_content",
		})
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirm_password")

	if email == "" {
		a.setFlash(w, "Email is required", true)
		http.Redirect(w, r, "/signup", http.StatusSeeOther)
		return
	}

	if len(password) < 8 {
		a.setFlash(w, "Password must be at least 8 characters", true)
		http.Redirect(w, r, "/signup", http.StatusSeeOther)
		return
	}

	if password != confirmPassword {
		a.setFlash(w, "Passwords do not match", true)
		http.Redirect(w, r, "/signup", http.StatusSeeOther)
		return
	}

	// Check if user already exists
	_, err := getUserByEmail(a.db, email)
	if err == nil {
		a.setFlash(w, "Email already registered", true)
		http.Redirect(w, r, "/signup", http.StatusSeeOther)
		return
	}

	passwordHash, err := hashPassword(password)
	if err != nil {
		a.setFlash(w, "Error creating account", true)
		http.Redirect(w, r, "/signup", http.StatusSeeOther)
		return
	}

	userID, err := createUser(a.db, email, passwordHash)
	if err != nil {
		log.Printf("Error creating user: %v", err)
		a.setFlash(w, "Error creating account. Please try again.", true)
		http.Redirect(w, r, "/signup", http.StatusSeeOther)
		return
	}

	// Auto-login after signup
	sessionValue := fmt.Sprintf("%d:%s", userID, a.sessionKey)
	cookie := http.Cookie{
		Name:     "session",
		Value:    sessionValue,
		Path:     "/",
		MaxAge:   86400 * 7, // 7 days
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, &cookie)

	a.setFlash(w, "Account created successfully!", false)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		redirect := r.URL.Query().Get("redirect")
		if redirect == "" {
			redirect = "/"
		}
		flash, flashType := a.getFlash(r)
		a.render(w, http.StatusOK, "login.html", map[string]any{
			"Redirect":       redirect,
			"Flash":          flash,
			"FlashType":      flashType,
			"ContentTemplate": "login_content",
		})
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	redirect := r.FormValue("redirect")
	if redirect == "" {
		redirect = "/"
	}

	if email == "" || password == "" {
		a.setFlash(w, "Email and password are required", true)
		http.Redirect(w, r, "/login?redirect="+redirect, http.StatusSeeOther)
		return
	}

	user, err := getUserByEmail(a.db, email)
	if err != nil {
		a.setFlash(w, "Invalid email or password", true)
		http.Redirect(w, r, "/login?redirect="+redirect, http.StatusSeeOther)
		return
	}

	if !checkPasswordHash(password, user.PasswordHash) {
		a.setFlash(w, "Invalid email or password", true)
		http.Redirect(w, r, "/login?redirect="+redirect, http.StatusSeeOther)
		return
	}

	// Set session cookie
	sessionValue := fmt.Sprintf("%d:%s", user.ID, a.sessionKey)
	cookie := http.Cookie{
		Name:     "session",
		Value:    sessionValue,
		Path:     "/",
		MaxAge:   86400 * 7, // 7 days
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, &cookie)

	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func (a *App) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		flash, flashType := a.getFlash(r)
		a.render(w, http.StatusOK, "forgot_password.html", map[string]any{
			"Flash":          flash,
			"FlashType":      flashType,
			"ContentTemplate": "forgot_password_content",
		})
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		a.setFlash(w, "Email is required", true)
		http.Redirect(w, r, "/forgot-password", http.StatusSeeOther)
		return
	}

	user, err := getUserByEmail(a.db, email)
	if err != nil {
		// Don't reveal if email exists or not
		a.setFlash(w, "If that email exists, a password reset link has been sent", false)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Generate reset token
	token := generateResetToken()
	expiresAt := time.Now().Add(1 * time.Hour) // Token valid for 1 hour

	err = createPasswordReset(a.db, user.ID, token, expiresAt)
	if err != nil {
		log.Printf("Error creating password reset: %v", err)
		a.setFlash(w, "Error processing request", true)
		http.Redirect(w, r, "/forgot-password", http.StatusSeeOther)
		return
	}

	// Send password reset email
	resetURL := fmt.Sprintf("%s/reset-password?token=%s", getBaseURL(r), token)
	if err := a.sendPasswordResetEmail(email, resetURL); err != nil {
		log.Printf("Error sending password reset email: %v", err)
		// Still show success message to user (security: don't reveal if email exists)
	}

	a.setFlash(w, "If that email exists, a password reset link has been sent", false)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *App) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		a.setFlash(w, "Invalid reset token", true)
		http.Redirect(w, r, "/forgot-password", http.StatusSeeOther)
		return
	}

	if r.Method == http.MethodGet {
		// Verify token is valid
		pr, err := getPasswordResetByToken(a.db, token)
		if err != nil {
			a.setFlash(w, "Invalid or expired reset token", true)
			http.Redirect(w, r, "/forgot-password", http.StatusSeeOther)
			return
		}

		if pr.Used || time.Now().After(pr.ExpiresAt) {
			a.setFlash(w, "Reset token has expired or already been used", true)
			http.Redirect(w, r, "/forgot-password", http.StatusSeeOther)
			return
		}

		flash, flashType := a.getFlash(r)
		a.render(w, http.StatusOK, "reset_password.html", map[string]any{
			"Token":          token,
			"Flash":          flash,
			"FlashType":      flashType,
			"ContentTemplate": "reset_password_content",
		})
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	token = r.FormValue("token")
	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirm_password")

	if len(password) < 8 {
		a.setFlash(w, "Password must be at least 8 characters", true)
		http.Redirect(w, r, "/reset-password?token="+token, http.StatusSeeOther)
		return
	}

	if password != confirmPassword {
		a.setFlash(w, "Passwords do not match", true)
		http.Redirect(w, r, "/reset-password?token="+token, http.StatusSeeOther)
		return
	}

	// Verify token
	pr, err := getPasswordResetByToken(a.db, token)
	if err != nil {
		a.setFlash(w, "Invalid reset token", true)
		http.Redirect(w, r, "/forgot-password", http.StatusSeeOther)
		return
	}

	if pr.Used || time.Now().After(pr.ExpiresAt) {
		a.setFlash(w, "Reset token has expired or already been used", true)
		http.Redirect(w, r, "/forgot-password", http.StatusSeeOther)
		return
	}

	// Update password
	passwordHash, err := hashPassword(password)
	if err != nil {
		a.setFlash(w, "Error resetting password", true)
		http.Redirect(w, r, "/reset-password?token="+token, http.StatusSeeOther)
		return
	}

	err = updateUserPassword(a.db, pr.UserID, passwordHash)
	if err != nil {
		a.setFlash(w, "Error resetting password", true)
		http.Redirect(w, r, "/reset-password?token="+token, http.StatusSeeOther)
		return
	}

	// Mark token as used
	err = markPasswordResetUsed(a.db, token)
	if err != nil {
		log.Printf("Error marking reset token as used: %v", err)
	}

	a.setFlash(w, "Password reset successfully! You can now log in.", false)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie := http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	}
	http.SetCookie(w, &cookie)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
