package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type App struct {
	db            *sql.DB
	tpl           *template.Template
	sessionKey    string
	csrfKey       string
	rateLimiter   map[string][]time.Time
	rateLimiterMu sync.RWMutex
}

func generateSessionKey() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func loadEnvFile() map[string]string {
	env := make(map[string]string)
	data, err := os.ReadFile(".env")
	if err != nil {
		return env // Return empty map if .env doesn't exist
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Parse KEY=VALUE
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			// Remove quotes if present
			if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
				value = value[1 : len(value)-1]
			}
			env[key] = value
		}
	}
	return env
}

func getEnv(key string, env map[string]string) string {
	// Check environment variables first (override .env)
	if val := os.Getenv(key); val != "" {
		return val
	}
	// Then check .env file
	return env[key]
}

func loadOrCreateKey(keyName string, env map[string]string) string {
	// First, try to get from .env or environment variable
	if key := getEnv(keyName, env); key != "" {
		if len(key) >= 32 { // Basic validation
			return key
		}
		log.Printf("Warning: %s from .env is too short, generating new one", keyName)
	}

	// Generate new key
	key := generateSessionKey()

	// Try to save to .env file
	envFile := ".env"
	envContent, err := os.ReadFile(envFile)
	envLines := []string{}
	if err == nil {
		envLines = strings.Split(string(envContent), "\n")
	} else {
		// .env doesn't exist, start with empty file
		envLines = []string{}
	}

	// Check if key already exists in .env
	keyFound := false
	for i, line := range envLines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") && strings.HasPrefix(trimmed, keyName+"=") {
			envLines[i] = fmt.Sprintf("%s=%s", keyName, key)
			keyFound = true
			break
		}
	}

	if !keyFound {
		// Add new key at the end
		if len(envLines) > 0 && envLines[len(envLines)-1] != "" {
			envLines = append(envLines, "")
		}
		envLines = append(envLines, fmt.Sprintf("%s=%s", keyName, key))
	}

	// Write back to .env
	content := strings.Join(envLines, "\n")
	if !strings.HasSuffix(content, "\n") && len(envLines) > 0 {
		content += "\n"
	}
	if err := os.WriteFile(envFile, []byte(content), 0600); err != nil {
		log.Printf("Warning: failed to save %s to .env: %v", keyName, err)
	} else {
		log.Printf("Generated and saved %s to .env", keyName)
	}

	return key
}

func money(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	d := cents / 100
	r := cents % 100
	// Add commas for thousands separator
	dStr := fmt.Sprintf("%d", d)
	var result []byte
	for i, c := range dStr {
		if i > 0 && (len(dStr)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return fmt.Sprintf("%s$%s.%02d", sign, string(result), r)
}

func bpsToAPR(bps int64) string {
	return fmt.Sprintf("%.2f%%", float64(bps)/100.0)
}

func formatDebtKind(kind string) string {
	kindMap := map[string]string{
		"card":           "Credit Card",
		"line_of_credit": "Line of Credit",
		"personal_loan":  "Personal Loan",
		"auto_loan":      "Auto Loan",
		"student_loan":   "Student Loan",
		"mortgage":       "Mortgage",
		"other_loan":     "Other Loan",
	}
	if formatted, ok := kindMap[kind]; ok {
		return formatted
	}
	return kind
}

func parseInt64(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }
func parseInt(s string) (int, error)     { return strconv.Atoi(s) }

// PWA: serve web app manifest
func serveManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	data, err := os.ReadFile(filepath.Join("static", "manifest.webmanifest"))
	if err != nil {
		http.Error(w, "manifest not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

// PWA: serve service worker
func serveServiceWorker(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	data, err := os.ReadFile(filepath.Join("static", "sw.js"))
	if err != nil {
		http.Error(w, "service worker not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/javascript")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}

// PWA & favicon: serve static icon from static/icon-192.png or static/icon-512.png
func serveIcon(size int) http.HandlerFunc {
	filename := filepath.Join("static", fmt.Sprintf("icon-%d.png", size))
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", 405)
			return
		}
		data, err := os.ReadFile(filename)
		if err != nil {
			http.Error(w, "icon not found", 404)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(data)
	}
}

func main() {
	db, err := openDB()
	if err != nil {
		log.Fatal(err)
	}
	if err := migrate(db); err != nil {
		log.Fatal(err)
	}

	var tpl *template.Template
	tpl = template.New("")
	funcs := template.FuncMap{
		"money":    money,
		"apr":      bpsToAPR,
		"mul":      func(a, b int64) int64 { return a * b },
		"sub":      func(a, b int64) int64 { return a - b },
		"div":      func(a, b float64) float64 { return a / b },
		"float":    func(i int64) float64 { return float64(i) },
		"debtKind": formatDebtKind,
		"now":      func() time.Time { return time.Now() },
		"date":     func(format string, t time.Time) string { return t.Format(format) },
		"getDebt": func(debtMap map[int64]Debt, id int64) Debt {
			return debtMap[id]
		},
		"dollars": func(v any) string {
			var cents int64
			switch x := v.(type) {
			case int64:
				cents = x
			case int:
				cents = int64(x)
			case float64:
				cents = int64(x)
			default:
				return "0.00"
			}
			return fmt.Sprintf("%.2f", float64(cents)/100.0)
		},
		"renderContent": func(name string, data any) (template.HTML, error) {
			var buf bytes.Buffer
			err := tpl.ExecuteTemplate(&buf, name, data)
			return template.HTML(buf.String()), err
		},
		"buildSortURL": func(search, kind, status, sort string) string {
			params := url.Values{}
			if search != "" {
				params.Set("search", search)
			}
			if kind != "" {
				params.Set("kind", kind)
			}
			if status != "" {
				params.Set("status", status)
			}
			if sort != "" && sort != "default" {
				params.Set("sort", sort)
			}
			query := params.Encode()
			if query != "" {
				return "/?" + query
			}
			return "/"
		},
		"cond": func(condition bool, trueVal, falseVal string) string {
			if condition {
				return trueVal
			}
			return falseVal
		},
		"monthName": func(month int) string {
			names := []string{"", "January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
			if month >= 1 && month <= 12 {
				return names[month]
			}
			return ""
		},
		"csrfToken": func(userID int64) string {
			// This will be set per-request in render
			return ""
		},
	}
	tpl = tpl.Funcs(funcs)
	tpl = template.Must(tpl.ParseGlob(filepath.Join("templates", "*.html")))

	// Load .env file
	env := loadEnvFile()

	app := &App{
		db:          db,
		tpl:         tpl,
		sessionKey:  loadOrCreateKey("SESSION_KEY", env),
		csrfKey:     loadOrCreateKey("CSRF_KEY", env),
		rateLimiter: make(map[string][]time.Time),
	}

	mux := http.NewServeMux()
	// Static files (logo, etc.)
	mux.Handle("/static/", http.StripPrefix("/static", http.FileServer(http.Dir("static"))))
	// PWA: manifest, service worker, and icons (no auth required)
	mux.HandleFunc("/manifest.webmanifest", serveManifest)
	mux.HandleFunc("/sw.js", serveServiceWorker)
	mux.HandleFunc("/icon-192.png", serveIcon(192))
	mux.HandleFunc("/icon-512.png", serveIcon(512))
	mux.HandleFunc("/signup", app.rateLimit(5, 15*time.Minute)(app.handleSignup))
	mux.HandleFunc("/login", app.rateLimit(5, 15*time.Minute)(app.handleLogin))
	mux.HandleFunc("/forgot-password", app.rateLimit(3, 1*time.Hour)(app.handleForgotPassword))
	mux.HandleFunc("/reset-password", app.rateLimit(5, 15*time.Minute)(app.handleResetPassword))
	mux.HandleFunc("/logout", app.handleLogout)
	mux.HandleFunc("/", app.requireAuth(app.handleIndex))
	mux.HandleFunc("/debts/new", app.requireAuth(app.handleDebtNew))
	mux.HandleFunc("/debts/create", app.requireAuth(app.requireCSRF(app.handleDebtCreate)))
	mux.HandleFunc("/debts/view", app.requireAuth(app.handleDebtView))
	mux.HandleFunc("/debts/edit", app.requireAuth(app.handleDebtEdit))
	mux.HandleFunc("/debts/update", app.requireAuth(app.requireCSRF(app.handleDebtUpdate)))
	mux.HandleFunc("/debts/delete", app.requireAuth(app.requireCSRF(app.handleDebtDelete)))
	mux.HandleFunc("/debts/toggle", app.requireAuth(app.requireCSRF(app.handleDebtToggle)))
	mux.HandleFunc("/payments/new", app.requireAuth(app.handlePaymentNew))
	mux.HandleFunc("/payments/add", app.requireAuth(app.requireCSRF(app.handlePaymentAdd)))
	mux.HandleFunc("/payments/edit", app.requireAuth(app.handlePaymentEdit))
	mux.HandleFunc("/payments/update", app.requireAuth(app.requireCSRF(app.handlePaymentUpdate)))
	mux.HandleFunc("/payments/delete", app.requireAuth(app.requireCSRF(app.handlePaymentDelete)))
	mux.HandleFunc("/payments", app.requireAuth(app.handlePayments))
	mux.HandleFunc("/plan", app.requireAuth(app.handlePlan))
	mux.HandleFunc("/budget", app.requireAuth(app.handleBudgetList))
	mux.HandleFunc("/budget/view", app.requireAuth(app.handleBudgetView))
	mux.HandleFunc("/budget/update", app.requireAuth(app.requireCSRF(app.handleBudgetUpdate)))
	mux.HandleFunc("/budget/category/add", app.requireAuth(app.handleBudgetCategoryAdd))
	mux.HandleFunc("/budget/category/create", app.requireAuth(app.requireCSRF(app.handleBudgetCategoryCreate)))
	mux.HandleFunc("/budget/category/edit", app.requireAuth(app.handleBudgetCategoryEdit))
	mux.HandleFunc("/budget/category/update", app.requireAuth(app.requireCSRF(app.handleBudgetCategoryUpdate)))
	mux.HandleFunc("/budget/category/delete", app.requireAuth(app.requireCSRF(app.handleBudgetCategoryDelete)))
	mux.HandleFunc("/budget/category/expenses", app.requireAuth(app.handleBudgetCategoryExpenses))
	mux.HandleFunc("/budget/expense/add", app.requireAuth(app.handleBudgetExpenseAdd))
	mux.HandleFunc("/budget/expense/create", app.requireAuth(app.requireCSRF(app.handleBudgetExpenseCreate)))
	mux.HandleFunc("/budget/expense/edit", app.requireAuth(app.handleBudgetExpenseEdit))
	mux.HandleFunc("/budget/expense/update", app.requireAuth(app.requireCSRF(app.handleBudgetExpenseUpdate)))
	mux.HandleFunc("/budget/expense/delete", app.requireAuth(app.requireCSRF(app.handleBudgetExpenseDelete)))

	// HTTPS support - check for TLS cert files
	certFile := getEnv("TLS_CERT_FILE", env)
	keyFile := getEnv("TLS_KEY_FILE", env)
	port := getEnv("PORT", env)
	if port == "" {
		port = "8100"
	}

	bind := getEnv("BIND", env)
	if bind == "" {
		bind = "127.0.0.1"
	}

	addr := bind + ":" + port

	if certFile != "" && keyFile != "" {
		log.Printf("Starting HTTPS server on :%s", addr)
		log.Fatal(http.ListenAndServeTLS(addr, certFile, keyFile, mux))
	} else {
		log.Printf("Starting HTTP server on :%s (set TLS_CERT_FILE and TLS_KEY_FILE for HTTPS)", port)
		log.Fatal(http.ListenAndServe(addr, mux))
	}
}

func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func checkPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func generateResetToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func generateCSRFToken(userID int64, csrfKey string) string {
	currentHour := time.Now().Unix() / 3600
	data := fmt.Sprintf("%d:%d", userID, currentHour)
	mac := hmac.New(sha256.New, []byte(csrfKey))
	mac.Write([]byte(data))
	return base64.URLEncoding.EncodeToString(mac.Sum(nil))
}

func validateCSRFToken(token string, userID int64, csrfKey string) bool {
	// Generate expected token for current hour and previous hour (to handle clock skew)
	currentHour := time.Now().Unix() / 3600
	for i := int64(0); i <= 1; i++ {
		data := fmt.Sprintf("%d:%d", userID, currentHour-i)
		mac := hmac.New(sha256.New, []byte(csrfKey))
		mac.Write([]byte(data))
		expected := base64.URLEncoding.EncodeToString(mac.Sum(nil))
		if hmac.Equal([]byte(token), []byte(expected)) {
			return true
		}
	}
	return false
}

type contextKey string

const userIDKey contextKey = "userID"

func (a *App) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionCookie, err := r.Cookie("session")
		if err != nil {
			http.Redirect(w, r, "/login?redirect="+r.URL.Path, http.StatusSeeOther)
			return
		}

		// Decode session: format is "userID:sessionKey"
		parts := strings.Split(sessionCookie.Value, ":")
		if len(parts) != 2 || parts[1] != a.sessionKey {
			http.Redirect(w, r, "/login?redirect="+r.URL.Path, http.StatusSeeOther)
			return
		}

		userID, err := parseInt64(parts[0])
		if err != nil {
			http.Redirect(w, r, "/login?redirect="+r.URL.Path, http.StatusSeeOther)
			return
		}

		// Verify user still exists
		_, err = getUserByID(a.db, userID)
		if err != nil {
			http.Redirect(w, r, "/login?redirect="+r.URL.Path, http.StatusSeeOther)
			return
		}

		// Add userID to request context
		ctx := context.WithValue(r.Context(), userIDKey, userID)
		next(w, r.WithContext(ctx))
	}
}

func (a *App) requireCSRF(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" || r.Method == "HEAD" {
			next(w, r)
			return
		}

		userID := getUserID(r)
		if userID == 0 {
			http.Error(w, "Unauthorized", 401)
			return
		}

		token := r.FormValue("csrf_token")
		if token == "" {
			token = r.Header.Get("X-CSRF-Token")
		}

		if !validateCSRFToken(token, userID, a.csrfKey) {
			log.Printf("CSRF validation failed for user %d", userID)
			http.Error(w, "Invalid security token", 403)
			return
		}

		next(w, r)
	}
}

func (a *App) rateLimit(maxAttempts int, window time.Duration) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// Skip rate limiting for localhost (development)
			host := r.RemoteAddr
			if strings.HasPrefix(host, "127.0.0.1:") || strings.HasPrefix(host, "[::1]:") || strings.HasPrefix(host, "localhost:") {
				next(w, r)
				return
			}

			key := host
			now := time.Now()

			a.rateLimiterMu.Lock()
			// Clean old entries
			if attempts, exists := a.rateLimiter[key]; exists {
				valid := make([]time.Time, 0)
				for _, t := range attempts {
					if now.Sub(t) < window {
						valid = append(valid, t)
					}
				}
				a.rateLimiter[key] = valid

				if len(valid) >= maxAttempts {
					a.rateLimiterMu.Unlock()
					log.Printf("Rate limit exceeded for %s", key)
					http.Error(w, "Too many requests. Please try again later.", 429)
					return
				}
			}

			// Add current attempt
			if a.rateLimiter[key] == nil {
				a.rateLimiter[key] = make([]time.Time, 0)
			}
			a.rateLimiter[key] = append(a.rateLimiter[key], now)
			a.rateLimiterMu.Unlock()

			next(w, r)
		}
	}
}

func getUserID(r *http.Request) int64 {
	userID, ok := r.Context().Value(userIDKey).(int64)
	if !ok {
		return 0
	}
	return userID
}

func (a *App) render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	if err := a.tpl.ExecuteTemplate(w, name, data); err != nil {
		// At this point headers are already written, so just log
		log.Printf("template error (%s): %v", name, err)
	}
}

func (a *App) getCSRFToken(r *http.Request) string {
	userID := getUserID(r)
	if userID == 0 {
		return ""
	}
	return generateCSRFToken(userID, a.csrfKey)
}

func getBaseURL(r *http.Request) string {
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		scheme = "http"
		if r.TLS != nil {
			scheme = "https"
		}
	}
	host := r.Host
	if host == "" {
		host = "localhost:8100"
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}

func (a *App) sendPasswordResetEmail(to, resetURL string) error {
	env := loadEnvFile()
	smtpHost := getEnv("SMTP_HOST", env)
	smtpPort := getEnv("SMTP_PORT", env)
	smtpUser := getEnv("SMTP_USER", env)
	smtpPass := getEnv("SMTP_PASSWORD", env)
	smtpFrom := getEnv("SMTP_FROM", env)

	// If SMTP not configured, log the link instead
	if smtpHost == "" || smtpUser == "" || smtpPass == "" {
		log.Printf("SMTP not configured. Password reset link for %s: %s", to, resetURL)
		return nil
	}

	if smtpFrom == "" {
		smtpFrom = smtpUser
	}
	if smtpPort == "" {
		smtpPort = "587"
	}

	subject := "Password Reset - Debt Manager"
	body := fmt.Sprintf(`Hello,

You requested a password reset for your Debt Manager account.

Click the link below to reset your password:
%s

This link will expire in 1 hour.

If you didn't request this, please ignore this email.

--
Debt Manager`, resetURL)

	msg := []byte(fmt.Sprintf("To: %s\r\n", to) +
		fmt.Sprintf("From: %s\r\n", smtpFrom) +
		fmt.Sprintf("Subject: %s\r\n", subject) +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" +
		body + "\r\n")

	addr := fmt.Sprintf("%s:%s", smtpHost, smtpPort)
	auth := smtp.PlainAuth("", smtpUser, smtpPass, smtpHost)

	return smtp.SendMail(addr, auth, smtpFrom, []string{to}, msg)
}

func (a *App) setFlash(w http.ResponseWriter, message string, isError bool) {
	flashType := "success"
	if isError {
		flashType = "error"
	}
	cookie := http.Cookie{
		Name:     "flash",
		Value:    message,
		Path:     "/",
		MaxAge:   1,
		HttpOnly: true,
	}
	http.SetCookie(w, &cookie)
	cookie = http.Cookie{
		Name:     "flash_type",
		Value:    flashType,
		Path:     "/",
		MaxAge:   1,
		HttpOnly: true,
	}
	http.SetCookie(w, &cookie)
}

func (a *App) getFlash(r *http.Request) (string, string) {
	flashCookie, _ := r.Cookie("flash")
	typeCookie, _ := r.Cookie("flash_type")
	if flashCookie == nil {
		return "", ""
	}
	flashType := "success"
	if typeCookie != nil {
		flashType = typeCookie.Value
	}
	return flashCookie.Value, flashType
}
