// Package auth provides SQLite-backed user and session storage for Jotes.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

const (
	// SessionCookieName is the HTTP cookie name that stores the raw session token.
	SessionCookieName = "jotes_session"

	defaultSessionLifetime = 30 * 24 * time.Hour
)

var (
	// ErrInvalidCredentials reports that a username/password pair did not match a stored account.
	ErrInvalidCredentials = errors.New("invalid username or password")

	// ErrInvalidUsername reports that a submitted username failed basic format validation.
	ErrInvalidUsername = errors.New("username must be 3-64 characters and use only letters, numbers, dots, dashes, or underscores")

	// ErrWeakPassword reports that a submitted password did not meet the minimum length requirement.
	ErrWeakPassword = errors.New("password must be at least 8 characters long")

	// ErrUserExists reports that a submitted username is already present in the database.
	ErrUserExists = errors.New("that username is already in use")

	// ErrUserNotFound reports that the requested user record does not exist.
	ErrUserNotFound = errors.New("user not found")

	// ErrUsersAlreadyExist reports that the one-time setup flow was attempted after accounts already existed.
	ErrUsersAlreadyExist = errors.New("accounts already exist")

	// ErrLastAdmin reports that an operation would remove administrator access from the final admin account.
	ErrLastAdmin = errors.New("at least one administrator account must remain")

	// ErrCannotDeleteSelf reports that an administrator tried to delete their own account from the admin UI.
	ErrCannotDeleteSelf = errors.New("you cannot delete your own account while signed in")

	usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{3,64}$`)
)

// Store owns the SQLite database connection and the helper settings used by
// Jotes authentication handlers and middleware.
type Store struct {
	db              *sql.DB
	dbPath          string
	sessionLifetime time.Duration
	now             func() time.Time
}

// CurrentUser is the authenticated user identity attached to one request.
type CurrentUser struct {
	ID       int64
	Username string
	IsAdmin  bool
}

// UserRecord is one persisted account returned to admin pages for listing or editing.
type UserRecord struct {
	ID        int64
	Username  string
	IsAdmin   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// UserCreateInput describes one administrator-created account that should be inserted.
type UserCreateInput struct {
	Username string
	Password string
	IsAdmin  bool
}

// UserUpdateInput describes one existing account that should be updated from the admin UI.
type UserUpdateInput struct {
	ID       int64
	Username string
	Password string
	IsAdmin  bool
}

// OpenStore creates the configured data directory, opens the SQLite database,
// enables the required pragmas, and ensures the auth schema exists.
//
// Parameters:
//   - dataDir: the directory that should hold jotes.db and any future auth-related files.
//
// Returns:
//   - *Store: the ready-to-use authentication store.
//   - error: non-nil when the directory cannot be created, the database cannot be opened, or the schema cannot be initialized.
func OpenStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data directory %q: %w", dataDir, err)
	}

	dbPath := filepath.Join(dataDir, "jotes.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database %q: %w", dbPath, err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	store := &Store{
		db:              db,
		dbPath:          dbPath,
		sessionLifetime: defaultSessionLifetime,
		now:             time.Now,
	}

	if err := store.configureSQLite(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.ensureSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

// Close releases the SQLite connection pool owned by the store.
//
// Parameters:
//   - none: Close acts on the receiver's database handle.
//
// Returns:
//   - error: non-nil when the underlying sql.DB close operation fails.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}

	return s.db.Close()
}

// DatabasePath returns the full filesystem path of the SQLite database file.
//
// Parameters:
//   - none: DatabasePath reads the stored path from the receiver.
//
// Returns:
//   - string: the absolute or cleaned path to jotes.db used by this store.
func (s *Store) DatabasePath() string {
	if s == nil {
		return ""
	}

	return s.dbPath
}

// SessionLifetime returns the duration new login sessions remain valid before expiry.
//
// Parameters:
//   - none: SessionLifetime reads the receiver's configured value.
//
// Returns:
//   - time.Duration: the session TTL used for cookie expiry and database expiry timestamps.
func (s *Store) SessionLifetime() time.Duration {
	if s == nil {
		return 0
	}

	return s.sessionLifetime
}

// HasUsers reports whether any account rows currently exist in the database.
//
// Parameters:
//   - none: HasUsers queries the receiver's users table.
//
// Returns:
//   - bool: true when at least one account exists, otherwise false.
//   - error: non-nil when the presence check query fails.
func (s *Store) HasUsers() (bool, error) {
	var exists bool
	if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM users LIMIT 1)`).Scan(&exists); err != nil {
		return false, fmt.Errorf("query user existence: %w", err)
	}

	return exists, nil
}

// BootstrapAdmin inserts the very first administrator account during the
// one-time setup flow, refusing to run once any accounts already exist.
//
// Parameters:
//   - username: the requested administrator username.
//   - password: the plaintext password that should be bcrypt-hashed before storage.
//
// Returns:
//   - *UserRecord: the newly created administrator account.
//   - error: non-nil when validation fails, another user already exists, or the insert cannot be completed.
func (s *Store) BootstrapAdmin(username, password string) (*UserRecord, error) {
	normalizedUsername, err := validateUsername(username)
	if err != nil {
		return nil, err
	}
	if err := validatePassword(password); err != nil {
		return nil, err
	}

	passwordHash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}

	nowUnix := s.now().UTC().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin bootstrap transaction: %w", err)
	}
	defer tx.Rollback()

	hasUsers, err := transactionHasUsers(tx)
	if err != nil {
		return nil, err
	}
	if hasUsers {
		return nil, ErrUsersAlreadyExist
	}

	result, err := tx.Exec(
		`INSERT INTO users (username, password_hash, is_admin, created_at, updated_at) VALUES (?, ?, 1, ?, ?)`,
		normalizedUsername,
		passwordHash,
		nowUnix,
		nowUnix,
	)
	if err != nil {
		if isUniqueUserConstraintError(err) {
			return nil, ErrUserExists
		}
		return nil, fmt.Errorf("insert bootstrap admin: %w", err)
	}

	userID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("read bootstrap admin id: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit bootstrap transaction: %w", err)
	}

	return &UserRecord{
		ID:        userID,
		Username:  normalizedUsername,
		IsAdmin:   true,
		CreatedAt: time.Unix(nowUnix, 0).UTC(),
		UpdatedAt: time.Unix(nowUnix, 0).UTC(),
	}, nil
}

// AuthenticateUser verifies one username/password pair and returns the
// authenticated user identity when the credentials match a stored account.
//
// Parameters:
//   - username: the submitted username, matched case-insensitively against the users table.
//   - password: the submitted plaintext password to compare with the stored bcrypt hash.
//
// Returns:
//   - *CurrentUser: the authenticated user identity ready to attach to a request or session.
//   - error: ErrInvalidCredentials when the username is missing or the password hash comparison fails, otherwise any database error.
func (s *Store) AuthenticateUser(username, password string) (*CurrentUser, error) {
	trimmedUsername := strings.TrimSpace(username)
	if trimmedUsername == "" || password == "" {
		return nil, ErrInvalidCredentials
	}

	var user CurrentUser
	var passwordHash string
	row := s.db.QueryRow(
		`SELECT id, username, is_admin, password_hash FROM users WHERE username = ? COLLATE NOCASE`,
		trimmedUsername,
	)
	if err := row.Scan(&user.ID, &user.Username, &user.IsAdmin, &passwordHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("query user for authentication: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	return &user, nil
}

// CreateSession inserts a new persistent login session row and returns the raw
// cookie token that the browser should store.
//
// Parameters:
//   - userID: the account id that should own the newly created session.
//
// Returns:
//   - string: the raw opaque token that should be sent to the browser as a cookie value.
//   - error: non-nil when token generation or session insertion fails.
func (s *Store) CreateSession(userID int64) (string, error) {
	rawToken, err := generateSessionToken()
	if err != nil {
		return "", err
	}

	nowTime := s.now().UTC()
	nowUnix := nowTime.Unix()
	expiresUnix := nowTime.Add(s.sessionLifetime).Unix()
	if _, err := s.db.Exec(
		`INSERT INTO sessions (user_id, token_hash, created_at, last_seen_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
		userID,
		hashSessionToken(rawToken),
		nowUnix,
		nowUnix,
		expiresUnix,
	); err != nil {
		return "", fmt.Errorf("insert session: %w", err)
	}

	return rawToken, nil
}

// LookupSession resolves one raw cookie token to its active user identity,
// deleting expired rows and extending valid sessions as they are used.
//
// Parameters:
//   - rawToken: the opaque cookie token supplied by the browser.
//
// Returns:
//   - *CurrentUser: the authenticated user identity when the session is valid, or nil when the token is empty, expired, or unknown.
//   - error: non-nil when the lookup or session refresh query fails.
func (s *Store) LookupSession(rawToken string) (*CurrentUser, error) {
	if strings.TrimSpace(rawToken) == "" {
		return nil, nil
	}

	nowTime := s.now().UTC()
	nowUnix := nowTime.Unix()
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at <= ?`, nowUnix); err != nil {
		return nil, fmt.Errorf("purge expired sessions: %w", err)
	}

	var user CurrentUser
	row := s.db.QueryRow(
		`SELECT users.id, users.username, users.is_admin
		 FROM sessions
		 JOIN users ON users.id = sessions.user_id
		 WHERE sessions.token_hash = ? AND sessions.expires_at > ?`,
		hashSessionToken(rawToken),
		nowUnix,
	)
	if err := row.Scan(&user.ID, &user.Username, &user.IsAdmin); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup session: %w", err)
	}

	if _, err := s.db.Exec(
		`UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE token_hash = ?`,
		nowUnix,
		nowTime.Add(s.sessionLifetime).Unix(),
		hashSessionToken(rawToken),
	); err != nil {
		return nil, fmt.Errorf("refresh session timestamps: %w", err)
	}

	return &user, nil
}

// DeleteSession removes one persistent login session identified by its raw
// cookie token, ignoring empty or already-missing tokens.
//
// Parameters:
//   - rawToken: the opaque cookie token that should be invalidated.
//
// Returns:
//   - error: non-nil when the delete statement fails.
func (s *Store) DeleteSession(rawToken string) error {
	if strings.TrimSpace(rawToken) == "" {
		return nil
	}

	if _, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hashSessionToken(rawToken)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}

	return nil
}

// ListUsers returns every account ordered by username for the admin settings UI.
//
// Parameters:
//   - none: ListUsers queries the receiver's users table.
//
// Returns:
//   - []UserRecord: the complete user list sorted case-insensitively by username.
//   - error: non-nil when the listing query fails.
func (s *Store) ListUsers() ([]UserRecord, error) {
	rows, err := s.db.Query(`SELECT id, username, is_admin, created_at, updated_at FROM users ORDER BY username COLLATE NOCASE ASC`)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	users := make([]UserRecord, 0)
	for rows.Next() {
		var user UserRecord
		var createdUnix int64
		var updatedUnix int64
		if err := rows.Scan(&user.ID, &user.Username, &user.IsAdmin, &createdUnix, &updatedUnix); err != nil {
			return nil, fmt.Errorf("scan user row: %w", err)
		}
		user.CreatedAt = time.Unix(createdUnix, 0).UTC()
		user.UpdatedAt = time.Unix(updatedUnix, 0).UTC()
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}

	return users, nil
}

// GetUser returns one account by id for the admin edit form.
//
// Parameters:
//   - userID: the primary-key id of the account to load.
//
// Returns:
//   - *UserRecord: the requested account record.
//   - error: ErrUserNotFound when no matching row exists, otherwise any query error.
func (s *Store) GetUser(userID int64) (*UserRecord, error) {
	var user UserRecord
	var createdUnix int64
	var updatedUnix int64
	row := s.db.QueryRow(`SELECT id, username, is_admin, created_at, updated_at FROM users WHERE id = ?`, userID)
	if err := row.Scan(&user.ID, &user.Username, &user.IsAdmin, &createdUnix, &updatedUnix); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("query user %d: %w", userID, err)
	}
	user.CreatedAt = time.Unix(createdUnix, 0).UTC()
	user.UpdatedAt = time.Unix(updatedUnix, 0).UTC()

	return &user, nil
}

// CreateUser inserts one new account for the admin settings UI after
// validating the submitted username and password.
//
// Parameters:
//   - input: the submitted username, plaintext password, and admin flag for the new account.
//
// Returns:
//   - *UserRecord: the newly created account record.
//   - error: non-nil when validation fails, the username already exists, or the insert cannot be completed.
func (s *Store) CreateUser(input UserCreateInput) (*UserRecord, error) {
	normalizedUsername, err := validateUsername(input.Username)
	if err != nil {
		return nil, err
	}
	if err := validatePassword(input.Password); err != nil {
		return nil, err
	}

	passwordHash, err := hashPassword(input.Password)
	if err != nil {
		return nil, err
	}

	nowUnix := s.now().UTC().Unix()
	result, err := s.db.Exec(
		`INSERT INTO users (username, password_hash, is_admin, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		normalizedUsername,
		passwordHash,
		boolToInteger(input.IsAdmin),
		nowUnix,
		nowUnix,
	)
	if err != nil {
		if isUniqueUserConstraintError(err) {
			return nil, ErrUserExists
		}
		return nil, fmt.Errorf("insert user: %w", err)
	}

	userID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("read created user id: %w", err)
	}

	return &UserRecord{
		ID:        userID,
		Username:  normalizedUsername,
		IsAdmin:   input.IsAdmin,
		CreatedAt: time.Unix(nowUnix, 0).UTC(),
		UpdatedAt: time.Unix(nowUnix, 0).UTC(),
	}, nil
}

// UpdateUser applies one administrator-submitted user edit, optionally
// changing the password when a non-empty plaintext password is provided.
//
// Parameters:
//   - input: the target user id plus the desired username, optional new password, and admin flag.
//
// Returns:
//   - error: non-nil when validation fails, the user is missing, the username collides, or the final admin would be removed.
func (s *Store) UpdateUser(input UserUpdateInput) error {
	normalizedUsername, err := validateUsername(input.Username)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.Password) != "" {
		if err := validatePassword(input.Password); err != nil {
			return err
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin user update transaction: %w", err)
	}
	defer tx.Rollback()

	var existingIsAdmin bool
	if err := tx.QueryRow(`SELECT is_admin FROM users WHERE id = ?`, input.ID).Scan(&existingIsAdmin); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUserNotFound
		}
		return fmt.Errorf("query existing user before update: %w", err)
	}

	if existingIsAdmin && !input.IsAdmin {
		adminCount, err := transactionAdminCount(tx)
		if err != nil {
			return err
		}
		if adminCount <= 1 {
			return ErrLastAdmin
		}
	}

	nowUnix := s.now().UTC().Unix()
	if strings.TrimSpace(input.Password) == "" {
		_, err = tx.Exec(
			`UPDATE users SET username = ?, is_admin = ?, updated_at = ? WHERE id = ?`,
			normalizedUsername,
			boolToInteger(input.IsAdmin),
			nowUnix,
			input.ID,
		)
	} else {
		passwordHash, hashErr := hashPassword(input.Password)
		if hashErr != nil {
			return hashErr
		}
		_, err = tx.Exec(
			`UPDATE users SET username = ?, password_hash = ?, is_admin = ?, updated_at = ? WHERE id = ?`,
			normalizedUsername,
			passwordHash,
			boolToInteger(input.IsAdmin),
			nowUnix,
			input.ID,
		)
	}
	if err != nil {
		if isUniqueUserConstraintError(err) {
			return ErrUserExists
		}
		return fmt.Errorf("update user %d: %w", input.ID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit user update transaction: %w", err)
	}

	return nil
}

// DeleteUser removes one account and all of its sessions, while preventing the
// current admin from deleting their own account and preserving at least one
// administrator account in the system.
//
// Parameters:
//   - userID: the account id that should be deleted.
//   - actingUserID: the currently signed-in administrator performing the delete action.
//
// Returns:
//   - error: non-nil when the target user is missing, the current user tried to delete themselves, or the last admin would be removed.
func (s *Store) DeleteUser(userID, actingUserID int64) error {
	if userID == actingUserID {
		return ErrCannotDeleteSelf
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin user delete transaction: %w", err)
	}
	defer tx.Rollback()

	var targetIsAdmin bool
	if err := tx.QueryRow(`SELECT is_admin FROM users WHERE id = ?`, userID).Scan(&targetIsAdmin); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUserNotFound
		}
		return fmt.Errorf("query existing user before delete: %w", err)
	}

	if targetIsAdmin {
		adminCount, err := transactionAdminCount(tx)
		if err != nil {
			return err
		}
		if adminCount <= 1 {
			return ErrLastAdmin
		}
	}

	result, err := tx.Exec(`DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		return fmt.Errorf("delete user %d: %w", userID, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted user row count: %w", err)
	}
	if rowsAffected == 0 {
		return ErrUserNotFound
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit user delete transaction: %w", err)
	}

	return nil
}

// configureSQLite enables the SQLite pragmas required for foreign keys and
// sensible write behavior in Jotes' small local database.
//
// Parameters:
//   - none: configureSQLite acts on the receiver's open database handle.
//
// Returns:
//   - error: non-nil when one of the required pragmas cannot be applied.
func (s *Store) configureSQLite() error {
	statements := []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("configure sqlite with %q: %w", statement, err)
		}
	}

	return nil
}

// ensureSchema creates the users and sessions tables when they do not yet
// exist so the rest of the auth feature can rely on a stable schema.
//
// Parameters:
//   - none: ensureSchema acts on the receiver's open database handle.
//
// Returns:
//   - error: non-nil when one of the schema statements fails.
func (s *Store) ensureSchema() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE COLLATE NOCASE,
			password_hash TEXT NOT NULL,
			is_admin INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			token_hash TEXT NOT NULL UNIQUE,
			created_at INTEGER NOT NULL,
			last_seen_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("ensure auth schema with %q: %w", statement, err)
		}
	}

	return nil
}

// validateUsername trims one submitted username and enforces the small
// filesystem-friendly character set used by Jotes accounts.
//
// Parameters:
//   - username: the raw username string submitted by setup or admin forms.
//
// Returns:
//   - string: the normalized trimmed username to persist when validation succeeds.
//   - error: ErrInvalidUsername when the trimmed value is empty or contains unsupported characters.
func validateUsername(username string) (string, error) {
	normalized := strings.TrimSpace(username)
	if !usernamePattern.MatchString(normalized) {
		return "", ErrInvalidUsername
	}

	return normalized, nil
}

// validatePassword enforces the minimum password length used for all Jotes accounts.
//
// Parameters:
//   - password: the raw plaintext password submitted by setup, login, or admin forms.
//
// Returns:
//   - error: ErrWeakPassword when the password is shorter than the minimum required length, otherwise nil.
func validatePassword(password string) error {
	if len(password) < 8 {
		return ErrWeakPassword
	}

	return nil
}

// hashPassword bcrypt-hashes one plaintext password for storage in the users table.
//
// Parameters:
//   - password: the plaintext password that should never be stored directly.
//
// Returns:
//   - string: the bcrypt hash string that should be persisted in the database.
//   - error: non-nil when bcrypt fails to generate a hash.
func hashPassword(password string) (string, error) {
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}

	return string(hashed), nil
}

// generateSessionToken creates one random opaque token suitable for use as a
// browser session cookie value.
//
// Parameters:
//   - none: generateSessionToken reads from crypto/rand.
//
// Returns:
//   - string: a base64url-encoded random token.
//   - error: non-nil when secure random bytes cannot be generated.
func generateSessionToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// hashSessionToken converts one raw cookie token into the SHA-256 digest that
// Jotes stores in SQLite instead of the raw token itself.
//
// Parameters:
//   - rawToken: the opaque browser cookie token.
//
// Returns:
//   - string: the lowercase hexadecimal SHA-256 digest of rawToken.
func hashSessionToken(rawToken string) string {
	digest := sha256.Sum256([]byte(rawToken))
	return fmt.Sprintf("%x", digest[:])
}

// transactionHasUsers reports whether any accounts exist inside the supplied
// transaction, allowing setup-time race checks to happen atomically.
//
// Parameters:
//   - tx: the open SQL transaction whose users table should be checked.
//
// Returns:
//   - bool: true when at least one user row exists.
//   - error: non-nil when the query fails.
func transactionHasUsers(tx *sql.Tx) (bool, error) {
	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM users LIMIT 1)`).Scan(&exists); err != nil {
		return false, fmt.Errorf("query user existence inside transaction: %w", err)
	}

	return exists, nil
}

// transactionAdminCount returns the number of administrator accounts visible
// inside the supplied transaction.
//
// Parameters:
//   - tx: the open SQL transaction whose admin rows should be counted.
//
// Returns:
//   - int: the number of users whose is_admin flag is true.
//   - error: non-nil when the count query fails.
func transactionAdminCount(tx *sql.Tx) (int, error) {
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE is_admin = 1`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count admins inside transaction: %w", err)
	}

	return count, nil
}

// boolToInteger converts one Go boolean into the 0/1 representation used by SQLite.
//
// Parameters:
//   - value: the boolean that should be converted for a SQL statement.
//
// Returns:
//   - int: 1 when value is true, otherwise 0.
func boolToInteger(value bool) int {
	if value {
		return 1
	}

	return 0
}

// isUniqueUserConstraintError reports whether one SQL error came from the
// case-insensitive unique users.username constraint.
//
// Parameters:
//   - err: the database error returned by an INSERT or UPDATE statement.
//
// Returns:
//   - bool: true when err represents the username uniqueness constraint, otherwise false.
func isUniqueUserConstraintError(err error) bool {
	if err == nil {
		return false
	}

	return strings.Contains(err.Error(), "UNIQUE constraint failed: users.username")
}
