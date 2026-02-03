// Package main: data layer — types, migration, and CRUD for users, debts, payments, and budgets.
package main

import (
	"database/sql"
	"fmt"
	"strconv"
	"time"

	_ "github.com/lib/pq"
)

type User struct {
	ID           int64
	Email        string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type PasswordReset struct {
	ID        int64
	UserID    int64
	Token     string
	ExpiresAt time.Time
	Used      bool
	CreatedAt time.Time
}

func openDB() (*sql.DB, error) {
	env := loadEnvFile()
	host := getEnv("DB_HOST", env)
	if host == "" {
		host = "localhost"
	}
	port := getEnv("DB_PORT", env)
	if port == "" {
		port = "5432"
	}
	user := getEnv("DB_USER", env)
	if user == "" {
		user = "postgres"
	}
	password := getEnv("DB_PASSWORD", env)
	dbname := getEnv("DB_NAME", env)
	if dbname == "" {
		dbname = "debtapp"
	}
	sslmode := getEnv("DB_SSLMODE", env)
	if sslmode == "" {
		sslmode = "disable"
	}

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, dbname, sslmode)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	if p := getEnv("DB_MAX_OPEN", env); p != "" {
		if n, e := strconv.Atoi(p); e == nil && n > 0 {
			db.SetMaxOpenConns(n)
		}
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS users (
  id BIGSERIAL PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS password_resets (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL,
  token TEXT NOT NULL UNIQUE,
  expires_at TIMESTAMPTZ NOT NULL,
  used BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL,
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS debts (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL,
  name TEXT NOT NULL,
  kind TEXT NOT NULL,
  balance_cents BIGINT NOT NULL CHECK (balance_cents >= 0),
  apr_bps BIGINT NOT NULL CHECK (apr_bps >= 0),
  min_payment_cents BIGINT NOT NULL CHECK (min_payment_cents >= 0),
  payment_cents BIGINT NOT NULL DEFAULT 0 CHECK (payment_cents >= 0),
  due_day INTEGER NOT NULL CHECK (due_day >= 1 AND due_day <= 28),
  notes TEXT NOT NULL DEFAULT '',
  active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS payments (
  id BIGSERIAL PRIMARY KEY,
  debt_id BIGINT NOT NULL,
  paid_on DATE NOT NULL,
  amount_cents BIGINT NOT NULL CHECK (amount_cents > 0),
  note TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  FOREIGN KEY (debt_id) REFERENCES debts(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_payments_debt ON payments(debt_id);
CREATE INDEX IF NOT EXISTS idx_password_resets_token ON password_resets(token);
CREATE INDEX IF NOT EXISTS idx_password_resets_user ON password_resets(user_id);
CREATE INDEX IF NOT EXISTS idx_debts_user ON debts(user_id);

-- Personal budget: one row per user per (year, month)
CREATE TABLE IF NOT EXISTS budgets (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NOT NULL,
  year INTEGER NOT NULL CHECK (year >= 2000 AND year <= 2100),
  month INTEGER NOT NULL CHECK (month >= 1 AND month <= 12),
  income_cents BIGINT NOT NULL CHECK (income_cents >= 0),
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  UNIQUE(user_id, year, month),
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Budget categories: spending limits per category. is_debt_payoff = true means "Extra for debt" (explicit link to payoff plan).
CREATE TABLE IF NOT EXISTS budget_categories (
  id BIGSERIAL PRIMARY KEY,
  budget_id BIGINT NOT NULL,
  name TEXT NOT NULL,
  limit_cents BIGINT NOT NULL CHECK (limit_cents >= 0),
  is_debt_payoff BOOLEAN NOT NULL DEFAULT FALSE,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  FOREIGN KEY (budget_id) REFERENCES budgets(id) ON DELETE CASCADE
);

-- Budget expenses: actual spending per category (manual entries).
CREATE TABLE IF NOT EXISTS budget_expenses (
  id BIGSERIAL PRIMARY KEY,
  budget_category_id BIGINT NOT NULL,
  spent_on DATE NOT NULL,
  amount_cents BIGINT NOT NULL CHECK (amount_cents > 0),
  note TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  FOREIGN KEY (budget_category_id) REFERENCES budget_categories(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_budgets_user ON budgets(user_id);
CREATE INDEX IF NOT EXISTS idx_budget_categories_budget ON budget_categories(budget_id);
CREATE INDEX IF NOT EXISTS idx_budget_expenses_category ON budget_expenses(budget_category_id);
`
	_, err := db.Exec(schema)
	return err
}

type Debt struct {
	ID              int64
	Name            string
	Kind            string
	BalanceCents    int64
	APRBps          int64
	MinPaymentCents int64
	PaymentCents    int64
	DueDay          int
	Notes           string
	Active          bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Payment struct {
	ID          int64
	DebtID      int64
	PaidOn      time.Time
	AmountCents int64
	Note        string
	CreatedAt   time.Time
}

// Budget: one per user per (year, month). Full-scope personal budget.
type Budget struct {
	ID          int64
	UserID      int64
	Year        int
	Month       int
	IncomeCents int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// BudgetCategory: spending category with a limit. is_debt_payoff = true means "Extra for debt" (explicit link to payoff plan).
type BudgetCategory struct {
	ID           int64
	BudgetID     int64
	Name         string
	LimitCents   int64
	IsDebtPayoff bool
	SortOrder    int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// BudgetExpense: one manual spending entry for a category.
type BudgetExpense struct {
	ID               int64
	BudgetCategoryID int64
	SpentOn          time.Time
	AmountCents      int64
	Note             string
	CreatedAt        time.Time
}

func listDebts(db *sql.DB, userID int64) ([]Debt, error) {
	return listDebtsFiltered(db, userID, "", "", "", "default")
}

func listDebtsFiltered(db *sql.DB, userID int64, searchQuery, kindFilter, statusFilter, sortBy string) ([]Debt, error) {
	query := `
SELECT id, name, kind, balance_cents, apr_bps, min_payment_cents, payment_cents, due_day, notes, active, created_at, updated_at
FROM debts
WHERE user_id = $1`
	args := []any{userID}
	n := 2

	if searchQuery != "" {
		query += fmt.Sprintf(" AND name LIKE $%d", n)
		args = append(args, "%"+searchQuery+"%")
		n++
	}

	if kindFilter != "" {
		query += fmt.Sprintf(" AND kind = $%d", n)
		args = append(args, kindFilter)
		n++
	}

	if statusFilter == "active" {
		query += " AND active = TRUE"
	} else if statusFilter == "closed" {
		query += " AND active = FALSE"
	}

	switch sortBy {
	case "name_asc":
		query += " ORDER BY name ASC"
	case "name_desc":
		query += " ORDER BY name DESC"
	case "balance_asc":
		query += " ORDER BY balance_cents ASC"
	case "balance_desc":
		query += " ORDER BY balance_cents DESC"
	case "apr_asc":
		query += " ORDER BY apr_bps ASC"
	case "apr_desc":
		query += " ORDER BY apr_bps DESC"
	case "min_asc":
		query += " ORDER BY min_payment_cents ASC"
	case "min_desc":
		query += " ORDER BY min_payment_cents DESC"
	case "due_asc":
		query += " ORDER BY due_day ASC"
	case "due_desc":
		query += " ORDER BY due_day DESC"
	case "type_asc":
		query += " ORDER BY kind ASC"
	case "type_desc":
		query += " ORDER BY kind DESC"
	default:
		query += " ORDER BY active DESC, name ASC"
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Debt
	for rows.Next() {
		var d Debt
		if err := rows.Scan(&d.ID, &d.Name, &d.Kind, &d.BalanceCents, &d.APRBps, &d.MinPaymentCents, &d.PaymentCents, &d.DueDay, &d.Notes, &d.Active, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func getDebt(db *sql.DB, userID, id int64) (Debt, error) {
	var d Debt
	err := db.QueryRow(`
SELECT id, name, kind, balance_cents, apr_bps, min_payment_cents, payment_cents, due_day, notes, active, created_at, updated_at
FROM debts WHERE id = $1 AND user_id = $2`, id, userID).
		Scan(&d.ID, &d.Name, &d.Kind, &d.BalanceCents, &d.APRBps, &d.MinPaymentCents, &d.PaymentCents, &d.DueDay, &d.Notes, &d.Active, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return Debt{}, err
	}
	return d, nil
}

func listPaymentsForDebt(db *sql.DB, userID, debtID int64) ([]Payment, error) {
	rows, err := db.Query(`
SELECT p.id, p.debt_id, p.paid_on, p.amount_cents, p.note, p.created_at
FROM payments p
JOIN debts d ON p.debt_id = d.id
WHERE p.debt_id = $1 AND d.user_id = $2
ORDER BY p.paid_on DESC, p.id DESC`, debtID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Payment
	for rows.Next() {
		var p Payment
		if err := rows.Scan(&p.ID, &p.DebtID, &p.PaidOn, &p.AmountCents, &p.Note, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type PaymentWithDebt struct {
	Payment
	DebtName string
}

func listAllPayments(db *sql.DB, userID int64) ([]PaymentWithDebt, error) {
	rows, err := db.Query(`
SELECT p.id, p.debt_id, p.paid_on, p.amount_cents, p.note, p.created_at, d.name
FROM payments p
JOIN debts d ON p.debt_id = d.id
WHERE d.user_id = $1
ORDER BY p.paid_on DESC, p.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PaymentWithDebt
	for rows.Next() {
		var pwd PaymentWithDebt
		if err := rows.Scan(&pwd.ID, &pwd.DebtID, &pwd.PaidOn, &pwd.AmountCents, &pwd.Note, &pwd.CreatedAt, &pwd.DebtName); err != nil {
			return nil, err
		}
		out = append(out, pwd)
	}
	return out, rows.Err()
}

func createDebt(db *sql.DB, userID int64, d Debt) (int64, error) {
	now := time.Now().UTC()
	err := db.QueryRow(`
INSERT INTO debts(user_id, name, kind, balance_cents, apr_bps, min_payment_cents, payment_cents, due_day, notes, active, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,TRUE,$10,$10)
RETURNING id`,
		userID, d.Name, d.Kind, d.BalanceCents, d.APRBps, d.MinPaymentCents, d.PaymentCents, d.DueDay, d.Notes, now).
		Scan(&d.ID)
	if err != nil {
		return 0, err
	}
	return d.ID, nil
}

func setDebtActive(db *sql.DB, userID, id int64, active bool) error {
	now := time.Now().UTC()
	_, err := db.Exec(`UPDATE debts SET active = $1, updated_at = $2 WHERE id = $3 AND user_id = $4`, active, now, id, userID)
	return err
}

func updateDebtBalance(db *sql.DB, userID, id int64, newBalanceCents int64) error {
	now := time.Now().UTC()
	_, err := db.Exec(`UPDATE debts SET balance_cents = $1, updated_at = $2 WHERE id = $3 AND user_id = $4`, newBalanceCents, now, id, userID)
	return err
}

func updateDebt(db *sql.DB, userID int64, d Debt) error {
	now := time.Now().UTC()
	_, err := db.Exec(`
UPDATE debts 
SET name = $1, kind = $2, balance_cents = $3, apr_bps = $4, min_payment_cents = $5, payment_cents = $6, due_day = $7, notes = $8, updated_at = $9
WHERE id = $10 AND user_id = $11`,
		d.Name, d.Kind, d.BalanceCents, d.APRBps, d.MinPaymentCents, d.PaymentCents, d.DueDay, d.Notes, now, d.ID, userID)
	return err
}

func deleteDebt(db *sql.DB, userID, id int64) error {
	_, err := db.Exec(`DELETE FROM debts WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

func deletePayment(db *sql.DB, userID, paymentID int64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var debtID, amountCents int64
	err = tx.QueryRow(`
		SELECT p.debt_id, p.amount_cents 
		FROM payments p
		JOIN debts d ON p.debt_id = d.id
		WHERE p.id = $1 AND d.user_id = $2`, paymentID, userID).Scan(&debtID, &amountCents)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`DELETE FROM payments WHERE id = $1`, paymentID)
	if err != nil {
		return err
	}

	var bal int64
	if err := tx.QueryRow(`SELECT balance_cents FROM debts WHERE id = $1 AND user_id = $2`, debtID, userID).Scan(&bal); err != nil {
		return err
	}
	newBal := bal + amountCents
	now := time.Now().UTC()
	if _, err := tx.Exec(`UPDATE debts SET balance_cents = $1, updated_at = $2 WHERE id = $3 AND user_id = $4`, newBal, now, debtID, userID); err != nil {
		return err
	}

	return tx.Commit()
}

func getPayment(db *sql.DB, userID, id int64) (Payment, error) {
	var p Payment
	err := db.QueryRow(`
SELECT p.id, p.debt_id, p.paid_on, p.amount_cents, p.note, p.created_at
FROM payments p
JOIN debts d ON p.debt_id = d.id
WHERE p.id = $1 AND d.user_id = $2`, id, userID).
		Scan(&p.ID, &p.DebtID, &p.PaidOn, &p.AmountCents, &p.Note, &p.CreatedAt)
	if err != nil {
		return Payment{}, err
	}
	return p, nil
}

func updatePayment(db *sql.DB, userID, paymentID int64, paidOn time.Time, amountCents int64, note string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var oldAmountCents, debtID int64
	err = tx.QueryRow(`
		SELECT p.debt_id, p.amount_cents 
		FROM payments p
		JOIN debts d ON p.debt_id = d.id
		WHERE p.id = $1 AND d.user_id = $2`, paymentID, userID).Scan(&debtID, &oldAmountCents)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`UPDATE payments SET paid_on = $1, amount_cents = $2, note = $3 WHERE id = $4`,
		paidOn, amountCents, note, paymentID)
	if err != nil {
		return err
	}

	var bal int64
	if err := tx.QueryRow(`SELECT balance_cents FROM debts WHERE id = $1 AND user_id = $2`, debtID, userID).Scan(&bal); err != nil {
		return err
	}
	newBal := bal + oldAmountCents - amountCents
	if newBal < 0 {
		newBal = 0
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(`UPDATE debts SET balance_cents = $1, updated_at = $2 WHERE id = $3 AND user_id = $4`, newBal, now, debtID, userID); err != nil {
		return err
	}

	return tx.Commit()
}

func addPayment(db *sql.DB, userID, debtID int64, paidOn time.Time, amountCents int64, note string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	created := time.Now().UTC()
	_, err = tx.Exec(`
INSERT INTO payments(debt_id, paid_on, amount_cents, note, created_at)
VALUES($1,$2,$3,$4,$5)`, debtID, paidOn, amountCents, note, created)
	if err != nil {
		return err
	}

	var bal int64
	var exists int
	err = tx.QueryRow(`SELECT 1 FROM debts WHERE id = $1 AND user_id = $2`, debtID, userID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("debt not found or access denied")
	}

	if err := tx.QueryRow(`SELECT balance_cents FROM debts WHERE id = $1 AND user_id = $2`, debtID, userID).Scan(&bal); err != nil {
		return err
	}
	newBal := bal - amountCents
	if newBal < 0 {
		newBal = 0
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(`UPDATE debts SET balance_cents = $1, updated_at = $2 WHERE id = $3 AND user_id = $4`, newBal, now, debtID, userID); err != nil {
		return err
	}

	return tx.Commit()
}

func createUser(db *sql.DB, email, passwordHash string) (int64, error) {
	now := time.Now().UTC()
	var id int64
	err := db.QueryRow(`
INSERT INTO users(email, password_hash, created_at, updated_at)
VALUES($1,$2,$3,$3)
RETURNING id`, email, passwordHash, now).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func getUserByEmail(db *sql.DB, email string) (User, error) {
	var u User
	err := db.QueryRow(`
SELECT id, email, password_hash, created_at, updated_at
FROM users WHERE email = $1`, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return User{}, err
	}
	return u, nil
}

func getUserByID(db *sql.DB, id int64) (User, error) {
	var u User
	err := db.QueryRow(`
SELECT id, email, password_hash, created_at, updated_at
FROM users WHERE id = $1`, id).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return User{}, err
	}
	return u, nil
}

func createPasswordReset(db *sql.DB, userID int64, token string, expiresAt time.Time) error {
	now := time.Now().UTC()
	_, err := db.Exec(`
INSERT INTO password_resets(user_id, token, expires_at, created_at)
VALUES($1,$2,$3,$4)`, userID, token, expiresAt.UTC(), now)
	return err
}

func getPasswordResetByToken(db *sql.DB, token string) (PasswordReset, error) {
	var pr PasswordReset
	err := db.QueryRow(`
SELECT id, user_id, token, expires_at, used, created_at
FROM password_resets WHERE token = $1`, token).
		Scan(&pr.ID, &pr.UserID, &pr.Token, &pr.ExpiresAt, &pr.Used, &pr.CreatedAt)
	if err != nil {
		return PasswordReset{}, err
	}
	return pr, nil
}

func markPasswordResetUsed(db *sql.DB, token string) error {
	_, err := db.Exec(`UPDATE password_resets SET used = TRUE WHERE token = $1`, token)
	return err
}

func updateUserPassword(db *sql.DB, userID int64, passwordHash string) error {
	now := time.Now().UTC()
	_, err := db.Exec(`UPDATE users SET password_hash = $1, updated_at = $2 WHERE id = $3`, passwordHash, now, userID)
	return err
}

// --- Budget CRUD ---

func getBudgetByYearMonth(db *sql.DB, userID int64, year, month int) (Budget, error) {
	var b Budget
	err := db.QueryRow(`
SELECT id, user_id, year, month, income_cents, created_at, updated_at
FROM budgets WHERE user_id = $1 AND year = $2 AND month = $3`, userID, year, month).
		Scan(&b.ID, &b.UserID, &b.Year, &b.Month, &b.IncomeCents, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return Budget{}, err
	}
	return b, nil
}

func getOrCreateBudget(db *sql.DB, userID int64, year, month int, incomeCents int64) (Budget, error) {
	b, err := getBudgetByYearMonth(db, userID, year, month)
	if err == nil {
		return b, nil
	}
	now := time.Now().UTC()
	err = db.QueryRow(`
INSERT INTO budgets(user_id, year, month, income_cents, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$5)
RETURNING id, user_id, year, month, income_cents, created_at, updated_at`,
		userID, year, month, incomeCents, now).
		Scan(&b.ID, &b.UserID, &b.Year, &b.Month, &b.IncomeCents, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return Budget{}, err
	}
	return b, nil
}

func listBudgets(db *sql.DB, userID int64, limit int) ([]Budget, error) {
	if limit <= 0 {
		limit = 24
	}
	rows, err := db.Query(`
SELECT id, user_id, year, month, income_cents, created_at, updated_at
FROM budgets WHERE user_id = $1 ORDER BY year DESC, month DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Budget
	for rows.Next() {
		var b Budget
		if err := rows.Scan(&b.ID, &b.UserID, &b.Year, &b.Month, &b.IncomeCents, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func getBudget(db *sql.DB, userID, budgetID int64) (Budget, error) {
	var b Budget
	err := db.QueryRow(`
SELECT id, user_id, year, month, income_cents, created_at, updated_at
FROM budgets WHERE id = $1 AND user_id = $2`, budgetID, userID).
		Scan(&b.ID, &b.UserID, &b.Year, &b.Month, &b.IncomeCents, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return Budget{}, err
	}
	return b, nil
}

func createBudget(db *sql.DB, userID int64, year, month int, incomeCents int64) (int64, error) {
	now := time.Now().UTC()
	var id int64
	err := db.QueryRow(`
INSERT INTO budgets(user_id, year, month, income_cents, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$5)
RETURNING id`, userID, year, month, incomeCents, now).Scan(&id)
	return id, err
}

func updateBudget(db *sql.DB, userID, budgetID int64, incomeCents int64) error {
	now := time.Now().UTC()
	res, err := db.Exec(`UPDATE budgets SET income_cents = $1, updated_at = $2 WHERE id = $3 AND user_id = $4`,
		incomeCents, now, budgetID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func listCategoriesForBudget(db *sql.DB, budgetID, userID int64) ([]BudgetCategory, error) {
	rows, err := db.Query(`
SELECT c.id, c.budget_id, c.name, c.limit_cents, c.is_debt_payoff, c.sort_order, c.created_at, c.updated_at
FROM budget_categories c
JOIN budgets b ON c.budget_id = b.id
WHERE c.budget_id = $1 AND b.user_id = $2 ORDER BY c.sort_order ASC, c.id ASC`, budgetID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BudgetCategory
	for rows.Next() {
		var c BudgetCategory
		if err := rows.Scan(&c.ID, &c.BudgetID, &c.Name, &c.LimitCents, &c.IsDebtPayoff, &c.SortOrder, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func getBudgetCategory(db *sql.DB, userID, categoryID int64) (BudgetCategory, error) {
	var c BudgetCategory
	err := db.QueryRow(`
SELECT c.id, c.budget_id, c.name, c.limit_cents, c.is_debt_payoff, c.sort_order, c.created_at, c.updated_at
FROM budget_categories c
JOIN budgets b ON c.budget_id = b.id
WHERE c.id = $1 AND b.user_id = $2`, categoryID, userID).
		Scan(&c.ID, &c.BudgetID, &c.Name, &c.LimitCents, &c.IsDebtPayoff, &c.SortOrder, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return BudgetCategory{}, err
	}
	return c, nil
}

func createBudgetCategory(db *sql.DB, userID, budgetID int64, name string, limitCents int64, isDebtPayoff bool, sortOrder int) (int64, error) {
	// Verify budget belongs to user
	if _, err := getBudget(db, userID, budgetID); err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	var id int64
	err := db.QueryRow(`
INSERT INTO budget_categories(budget_id, name, limit_cents, is_debt_payoff, sort_order, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$6)
RETURNING id`, budgetID, name, limitCents, isDebtPayoff, sortOrder, now).Scan(&id)
	return id, err
}

func updateBudgetCategory(db *sql.DB, userID, categoryID int64, name string, limitCents int64, isDebtPayoff bool, sortOrder int) error {
	// Verify category belongs to user via budget
	if _, err := getBudgetCategory(db, userID, categoryID); err != nil {
		return err
	}
	now := time.Now().UTC()
	res, err := db.Exec(`
UPDATE budget_categories SET name = $1, limit_cents = $2, is_debt_payoff = $3, sort_order = $4, updated_at = $5
WHERE id = $6`, name, limitCents, isDebtPayoff, sortOrder, now, categoryID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func deleteBudgetCategory(db *sql.DB, userID, categoryID int64) error {
	if _, err := getBudgetCategory(db, userID, categoryID); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM budget_categories WHERE id = $1`, categoryID)
	return err
}

func totalSpentForCategory(db *sql.DB, categoryID int64) (int64, error) {
	var total sql.NullInt64
	err := db.QueryRow(`SELECT COALESCE(SUM(amount_cents), 0) FROM budget_expenses WHERE budget_category_id = $1`, categoryID).Scan(&total)
	if err != nil {
		return 0, err
	}
	if total.Valid {
		return total.Int64, nil
	}
	return 0, nil
}

func listExpensesForCategory(db *sql.DB, userID, categoryID int64) ([]BudgetExpense, error) {
	rows, err := db.Query(`
SELECT e.id, e.budget_category_id, e.spent_on, e.amount_cents, e.note, e.created_at
FROM budget_expenses e
JOIN budget_categories c ON e.budget_category_id = c.id
JOIN budgets b ON c.budget_id = b.id
WHERE e.budget_category_id = $1 AND b.user_id = $2 ORDER BY e.spent_on DESC, e.id DESC`, categoryID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BudgetExpense
	for rows.Next() {
		var e BudgetExpense
		if err := rows.Scan(&e.ID, &e.BudgetCategoryID, &e.SpentOn, &e.AmountCents, &e.Note, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func getBudgetExpense(db *sql.DB, userID, expenseID int64) (BudgetExpense, error) {
	var e BudgetExpense
	err := db.QueryRow(`
SELECT e.id, e.budget_category_id, e.spent_on, e.amount_cents, e.note, e.created_at
FROM budget_expenses e
JOIN budget_categories c ON e.budget_category_id = c.id
JOIN budgets b ON c.budget_id = b.id
WHERE e.id = $1 AND b.user_id = $2`, expenseID, userID).
		Scan(&e.ID, &e.BudgetCategoryID, &e.SpentOn, &e.AmountCents, &e.Note, &e.CreatedAt)
	if err != nil {
		return BudgetExpense{}, err
	}
	return e, nil
}

func addBudgetExpense(db *sql.DB, userID, categoryID int64, spentOn time.Time, amountCents int64, note string) error {
	if _, err := getBudgetCategory(db, userID, categoryID); err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err := db.Exec(`
INSERT INTO budget_expenses(budget_category_id, spent_on, amount_cents, note, created_at)
VALUES($1,$2,$3,$4,$5)`, categoryID, spentOn, amountCents, note, now)
	return err
}

func updateBudgetExpense(db *sql.DB, userID, expenseID int64, spentOn time.Time, amountCents int64, note string) error {
	if _, err := getBudgetExpense(db, userID, expenseID); err != nil {
		return err
	}
	_, err := db.Exec(`UPDATE budget_expenses SET spent_on = $1, amount_cents = $2, note = $3 WHERE id = $4`,
		spentOn, amountCents, note, expenseID)
	return err
}

func deleteBudgetExpense(db *sql.DB, userID, expenseID int64) error {
	if _, err := getBudgetExpense(db, userID, expenseID); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM budget_expenses WHERE id = $1`, expenseID)
	return err
}

// SumOfMinPaymentsForUser returns the total minimum payment per month for active debts (for plan/budget link).
func SumOfMinPaymentsForUser(db *sql.DB, userID int64) (int64, error) {
	var total sql.NullInt64
	err := db.QueryRow(`
SELECT COALESCE(SUM(min_payment_cents), 0) FROM debts WHERE user_id = $1 AND active = TRUE AND balance_cents > 0`, userID).Scan(&total)
	if err != nil {
		return 0, err
	}
	if total.Valid {
		return total.Int64, nil
	}
	return 0, nil
}
