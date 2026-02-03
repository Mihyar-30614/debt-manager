# Debt Manager

A multi-user debt management application built with Go and PostgreSQL.

## Features

- Track multiple debts (cards and loans)
- Record payments
- View payoff plans with interest calculations
- Support for avalanche and snowball payoff strategies
- User authentication and data isolation
- Secure session management
- CSRF protection
- Rate limiting on authentication endpoints

## Development

### Running the Server

Normal mode:
```bash
go run .
```

### Hot Reload (Development)

For automatic server restart on file changes, use Air:

1. **Install Air** (if not already installed):
   ```bash
   make install-air
   ```
   Or manually:
   ```bash
   go install github.com/cosmtrek/air@v1.49.0
   ```
   
   Make sure `$(go env GOPATH)/bin` is in your PATH.

2. **Run with hot reload**:
   ```bash
   make dev
   ```
   
   The Makefile will automatically find `air` in your GOPATH/bin directory even if it's not in your PATH.
   
   Or run directly:
   ```bash
   air
   # or
   $(go env GOPATH)/bin/air
   ```

The server will automatically restart when you modify `.go` or `.html` files.

### Configuration

Hot reload is configured via `.air.toml`. The configuration watches:
- All `.go` files
- All `.html` template files

Build output and temporary files are stored in the `tmp/` directory (gitignored).

## Usage

1. Start the server (default: http://localhost:8100)
2. Log in with your password (default: `admin` - see Configuration below)
3. Add debts via the "Add debt" page
4. Record payments on individual debt pages
5. View payoff plans on the "Payoff plan" page

## Configuration

All configuration is managed through a `.env` file. Copy `.env.example` to `.env` and customize:

```bash
cp .env.example .env
```

### Configuration Options

**Server Configuration:**
- `PORT` - Server port (default: 8100)

**HTTPS (optional):**
- `TLS_CERT_FILE` - Path to TLS certificate file
- `TLS_KEY_FILE` - Path to TLS private key file

**Security Keys (auto-generated if not set):**
- `SESSION_KEY` - Secret key for session validation (auto-generated on first run)
- `CSRF_KEY` - Secret key for CSRF token generation (auto-generated on first run)

**PostgreSQL:**
- `DB_HOST` - Database host (default: localhost)
- `DB_PORT` - Database port (default: 5432)
- `DB_USER` - Database user (default: postgres)
- `DB_PASSWORD` - Database password
- `DB_NAME` - Database name (default: debtapp)
- `DB_SSLMODE` - SSL mode (default: disable for local)
- `DB_MAX_OPEN` - Max open connections (optional)

Create the database before first run: `createdb debtapp` (or use your DB_NAME).

**SMTP Configuration (optional, for password reset emails):**
- `SMTP_HOST` - SMTP server hostname
- `SMTP_PORT` - SMTP server port (default: 587)
- `SMTP_USER` - SMTP username/email
- `SMTP_PASSWORD` - SMTP password
- `SMTP_FROM` - From email address (defaults to SMTP_USER if not set)

If SMTP is not configured, password reset links will be logged to the console.

**Note:** Environment variables take precedence over `.env` file values, so you can still override settings using `export` commands if needed.

### Security Features

- **User Data Isolation**: Each user can only access their own debts and payments
- **CSRF Protection**: All POST forms require CSRF tokens
- **Rate Limiting**: Authentication endpoints are rate-limited:
  - Signup/Login: 5 attempts per 15 minutes
  - Password Reset: 3 attempts per hour
- **Input Sanitization**: All user input is sanitized to prevent XSS attacks
- **Secure Cookies**: Session cookies use Secure flag (requires HTTPS)
- **Error Handling**: Internal errors are logged but not exposed to users

## Database

The app uses **PostgreSQL**. Configure connection via `.env` (see Configuration). Create the database before first run, e.g.:

```bash
createdb debtapp
```

Schema is applied automatically on startup (tables and indexes created if not present).
