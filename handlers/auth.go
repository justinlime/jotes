package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"jotes/auth"
	"jotes/models"
)

// currentUserViewFromRequest converts the authenticated request context into
// the smaller template-facing user view used by Jotes page models.
//
// Parameters:
//   - r: the current HTTP request whose context may carry an authenticated user.
//
// Returns:
//   - *models.CurrentUserView: the template-ready authenticated user data, or nil when no user is attached to the request.
func currentUserViewFromRequest(r *http.Request) *models.CurrentUserView {
	user := auth.CurrentUserFromRequest(r)
	if user == nil {
		return nil
	}

	return &models.CurrentUserView{
		ID:       user.ID,
		Username: user.Username,
		IsAdmin:  user.IsAdmin,
	}
}

// sanitizeNextTarget normalizes one login redirect target so only local Jotes
// paths are accepted and login/setup/logout routes cannot cause redirect loops.
//
// Parameters:
//   - rawTarget: the untrusted redirect target read from a query string or form field.
//
// Returns:
//   - string: a safe relative path to redirect to after authentication, defaulting to "/".
func sanitizeNextTarget(rawTarget string) string {
	trimmedTarget := strings.TrimSpace(rawTarget)
	if trimmedTarget == "" || !strings.HasPrefix(trimmedTarget, "/") || strings.HasPrefix(trimmedTarget, "//") {
		return "/"
	}

	for _, blockedPrefix := range []string{"/jotes/login", "/jotes/setup", "/jotes/logout"} {
		if trimmedTarget == blockedPrefix || strings.HasPrefix(trimmedTarget, blockedPrefix+"?") {
			return "/"
		}
	}

	return trimmedTarget
}

// validateSameOriginFormPost rejects form submissions whose Origin or Referer
// headers do not match the active Jotes host, providing lightweight CSRF
// protection for login, logout, setup, and admin form posts.
//
// Parameters:
//   - r: the incoming HTTP request that intends to mutate authentication state or user records.
//
// Returns:
//   - error: non-nil when r is not a POST request or its Origin/Referer headers do not belong to the active host.
func validateSameOriginFormPost(r *http.Request) error {
	if r.Method != http.MethodPost {
		return errors.New("method not allowed")
	}

	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" && !requestURLMatchesHost(origin, r.Host) {
		return errors.New("form origin did not match the active Jotes host")
	}
	if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" && !requestURLMatchesHost(referer, r.Host) {
		return errors.New("form referer did not match the active Jotes host")
	}

	return nil
}

// LoginHandler renders the login page on GET and authenticates one user on
// POST, creating a persistent session cookie when credentials are valid.
//
// Parameters:
//   - store: the SQLite-backed auth store used to authenticate users and create sessions.
//   - siteName: the branding label shown in the login page title.
//   - defaultTheme: the HTML theme class applied to the page root.
//   - tmpl: any renderer capable of executing the login template.
//
// Returns:
//   - http.HandlerFunc: a handler that serves the login form and processes login submissions.
func LoginHandler(store *auth.Store, siteName, defaultTheme string, tmpl interface {
	ExecuteLogin(http.ResponseWriter, *models.LoginPageData) error
}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		currentUser := auth.CurrentUserFromRequest(r)
		if currentUser != nil {
			http.Redirect(w, r, sanitizeNextTarget(r.URL.Query().Get("next")), http.StatusSeeOther)
			return
		}

		hasUsers, err := store.HasUsers()
		if err != nil {
			http.Error(w, "Authentication is unavailable", http.StatusInternalServerError)
			return
		}
		if !hasUsers {
			http.Redirect(w, r, "/jotes/setup", http.StatusSeeOther)
			return
		}

		pageData := &models.LoginPageData{
			Title:        "Login",
			SiteName:     siteName,
			DefaultTheme: defaultTheme,
			ShowSearch:   false,
			Next:         sanitizeNextTarget(r.URL.Query().Get("next")),
		}
		pageData.NoticeMessage = loginNoticeMessage(r.URL.Query().Get("notice"))

		if r.Method == http.MethodGet {
			w.Header().Set("Cache-Control", "no-store")
			if err := tmpl.ExecuteLogin(w, pageData); err != nil {
				http.Error(w, "Template error", http.StatusInternalServerError)
			}
			return
		}

		if err := validateSameOriginFormPost(r); err != nil {
			http.Error(w, "Invalid login request", http.StatusBadRequest)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid login form", http.StatusBadRequest)
			return
		}

		pageData.Username = strings.TrimSpace(r.PostForm.Get("username"))
		pageData.Next = sanitizeNextTarget(r.PostForm.Get("next"))

		user, err := store.AuthenticateUser(pageData.Username, r.PostForm.Get("password"))
		if err != nil {
			pageData.ErrorMessage = "Incorrect username or password."
			w.Header().Set("Cache-Control", "no-store")
			if execErr := tmpl.ExecuteLogin(w, pageData); execErr != nil {
				http.Error(w, "Template error", http.StatusInternalServerError)
			}
			return
		}

		rawToken, err := store.CreateSession(user.ID)
		if err != nil {
			http.Error(w, "Could not create login session", http.StatusInternalServerError)
			return
		}

		auth.SetSessionCookie(w, rawToken, store.SessionLifetime(), r.TLS != nil)
		http.Redirect(w, r, pageData.Next, http.StatusSeeOther)
	}
}

// SetupHandler renders the first-run setup page on GET and creates the first
// administrator account plus login session on POST.
//
// Parameters:
//   - store: the SQLite-backed auth store used to bootstrap the first admin account.
//   - siteName: the branding label shown in the setup page title.
//   - defaultTheme: the HTML theme class applied to the page root.
//   - tmpl: any renderer capable of executing the setup template.
//
// Returns:
//   - http.HandlerFunc: a handler that serves the setup form and processes the first admin creation.
func SetupHandler(store *auth.Store, siteName, defaultTheme string, tmpl interface {
	ExecuteSetup(http.ResponseWriter, *models.SetupPageData) error
}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		currentUser := auth.CurrentUserFromRequest(r)
		if currentUser != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		hasUsers, err := store.HasUsers()
		if err != nil {
			http.Error(w, "Authentication is unavailable", http.StatusInternalServerError)
			return
		}
		if hasUsers {
			http.Redirect(w, r, "/jotes/login", http.StatusSeeOther)
			return
		}

		pageData := &models.SetupPageData{
			Title:        "Create administrator",
			SiteName:     siteName,
			DefaultTheme: defaultTheme,
			ShowSearch:   false,
		}
		pageData.NoticeMessage = setupNoticeMessage(r.URL.Query().Get("notice"))

		if r.Method == http.MethodGet {
			w.Header().Set("Cache-Control", "no-store")
			if err := tmpl.ExecuteSetup(w, pageData); err != nil {
				http.Error(w, "Template error", http.StatusInternalServerError)
			}
			return
		}

		if err := validateSameOriginFormPost(r); err != nil {
			http.Error(w, "Invalid setup request", http.StatusBadRequest)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid setup form", http.StatusBadRequest)
			return
		}

		pageData.Username = strings.TrimSpace(r.PostForm.Get("username"))
		password := r.PostForm.Get("password")
		passwordConfirm := r.PostForm.Get("password_confirm")
		if password != passwordConfirm {
			pageData.ErrorMessage = "The password confirmation did not match."
			w.Header().Set("Cache-Control", "no-store")
			if err := tmpl.ExecuteSetup(w, pageData); err != nil {
				http.Error(w, "Template error", http.StatusInternalServerError)
			}
			return
		}

		createdUser, err := store.BootstrapAdmin(pageData.Username, password)
		if err != nil {
			pageData.ErrorMessage = authErrorMessage(err)
			w.Header().Set("Cache-Control", "no-store")
			if execErr := tmpl.ExecuteSetup(w, pageData); execErr != nil {
				http.Error(w, "Template error", http.StatusInternalServerError)
			}
			return
		}

		rawToken, err := store.CreateSession(createdUser.ID)
		if err != nil {
			http.Error(w, "Could not create login session", http.StatusInternalServerError)
			return
		}

		auth.SetSessionCookie(w, rawToken, store.SessionLifetime(), r.TLS != nil)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

// LogoutHandler invalidates the current session token and clears the browser
// cookie before redirecting the user back to the login page.
//
// Parameters:
//   - store: the SQLite-backed auth store used to delete the persisted session row.
//
// Returns:
//   - http.HandlerFunc: a handler that accepts a POST logout form submission.
func LogoutHandler(store *auth.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := validateSameOriginFormPost(r); err != nil {
			http.Error(w, "Invalid logout request", http.StatusBadRequest)
			return
		}

		cookie, err := r.Cookie(auth.SessionCookieName)
		if err == nil {
			_ = store.DeleteSession(cookie.Value)
		}
		auth.ClearSessionCookie(w, r.TLS != nil)
		http.Redirect(w, r, "/jotes/login?notice=signed_out", http.StatusSeeOther)
	}
}

// AdminUsersHandler renders the administrator-only user management overview page.
//
// Parameters:
//   - store: the SQLite-backed auth store used to list accounts.
//   - siteName: the branding label shown in the admin page title.
//   - defaultTheme: the HTML theme class applied to the page root.
//   - tmpl: any renderer capable of executing the admin users template.
//
// Returns:
//   - http.HandlerFunc: a handler that renders the user-management list for administrators.
func AdminUsersHandler(store *auth.Store, siteName, defaultTheme string, tmpl interface {
	ExecuteAdminUsers(http.ResponseWriter, *models.AdminUsersPageData) error
}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		currentUser, ok := requireAdminRequestUser(w, r)
		if !ok {
			return
		}

		users, err := store.ListUsers()
		if err != nil {
			http.Error(w, "Could not load users", http.StatusInternalServerError)
			return
		}

		entries := make([]models.AdminUserEntry, 0, len(users))
		for _, user := range users {
			entries = append(entries, models.AdminUserEntry{
				ID:            user.ID,
				Username:      user.Username,
				IsAdmin:       user.IsAdmin,
				IsCurrentUser: user.ID == currentUser.ID,
				CreatedAt:     user.CreatedAt,
				UpdatedAt:     user.UpdatedAt,
			})
		}

		data := &models.AdminUsersPageData{
			Title:         "Admin settings",
			SiteName:      siteName,
			DefaultTheme:  defaultTheme,
			ShowSearch:    true,
			CurrentUser:   currentUserViewFromRequest(r),
			Users:         entries,
			ErrorMessage:  adminErrorMessage(r.URL.Query().Get("error")),
			NoticeMessage: adminNoticeMessage(r.URL.Query().Get("notice")),
		}
		if err := tmpl.ExecuteAdminUsers(w, data); err != nil {
			http.Error(w, "Template error", http.StatusInternalServerError)
		}
	}
}

// AdminCreateUserHandler renders the administrator-only create-user form on
// GET and inserts the submitted user account on POST.
//
// Parameters:
//   - store: the SQLite-backed auth store used to create new accounts.
//   - siteName: the branding label shown in the page title.
//   - defaultTheme: the HTML theme class applied to the page root.
//   - tmpl: any renderer capable of executing the admin user form template.
//
// Returns:
//   - http.HandlerFunc: a handler that serves and processes the create-user form.
func AdminCreateUserHandler(store *auth.Store, siteName, defaultTheme string, tmpl interface {
	ExecuteAdminUserForm(http.ResponseWriter, *models.AdminUserFormPageData) error
}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := requireAdminRequestUser(w, r)
		if !ok {
			return
		}

		data := buildAdminUserFormPageData(r, siteName, defaultTheme)
		data.Title = "Create user"
		data.FormAction = "/jotes/admin/users/new"
		data.SubmitLabel = "Create user"
		data.PasswordHelp = "Passwords must be at least 8 characters long."
		data.CancelURL = "/jotes/admin"
		data.IsEdit = false

		if r.Method == http.MethodGet {
			if err := tmpl.ExecuteAdminUserForm(w, data); err != nil {
				http.Error(w, "Template error", http.StatusInternalServerError)
			}
			return
		}

		if err := validateSameOriginFormPost(r); err != nil {
			http.Error(w, "Invalid user creation request", http.StatusBadRequest)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid user form", http.StatusBadRequest)
			return
		}

		data.TargetUsername = strings.TrimSpace(r.PostForm.Get("username"))
		data.TargetIsAdmin = r.PostForm.Get("is_admin") == "1"

		_, err := store.CreateUser(auth.UserCreateInput{
			Username: data.TargetUsername,
			Password: r.PostForm.Get("password"),
			IsAdmin:  data.TargetIsAdmin,
		})
		if err != nil {
			data.ErrorMessage = authErrorMessage(err)
			if execErr := tmpl.ExecuteAdminUserForm(w, data); execErr != nil {
				http.Error(w, "Template error", http.StatusInternalServerError)
			}
			return
		}

		http.Redirect(w, r, "/jotes/admin?notice=user_created", http.StatusSeeOther)
	}
}

// AdminUserDetailHandler routes administrator-only edit and delete requests for
// one existing account identified by the /jotes/admin/users/{id} URL prefix.
//
// Parameters:
//   - store: the SQLite-backed auth store used to load, update, and delete accounts.
//   - siteName: the branding label shown in the page title.
//   - defaultTheme: the HTML theme class applied to the page root.
//   - tmpl: any renderer capable of executing the admin user form template.
//
// Returns:
//   - http.Handler: a handler that serves edit forms and delete actions under the user-id route prefix.
func AdminUserDetailHandler(store *auth.Store, siteName, defaultTheme string, tmpl interface {
	ExecuteAdminUserForm(http.ResponseWriter, *models.AdminUserFormPageData) error
}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		currentUser, ok := requireAdminRequestUser(w, r)
		if !ok {
			return
		}

		trimmedPath := strings.Trim(strings.TrimPrefix(r.URL.Path, "/jotes/admin/users/"), "/")
		if trimmedPath == "" {
			http.NotFound(w, r)
			return
		}

		if strings.HasSuffix(trimmedPath, "/delete") {
			userID, err := parseAdminUserID(strings.TrimSuffix(trimmedPath, "/delete"))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			handleAdminUserDelete(w, r, store, currentUser.ID, userID)
			return
		}

		userID, err := parseAdminUserID(trimmedPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		handleAdminUserEdit(w, r, store, siteName, defaultTheme, tmpl, userID)
	})
}

// buildAdminUserFormPageData creates the common page chrome shared by the
// create-user and edit-user admin forms.
//
// Parameters:
//   - r: the current HTTP request whose authenticated user should populate the header menu.
//   - siteName: the branding label shown in the page title.
//   - defaultTheme: the HTML theme class applied to the page root.
//
// Returns:
//   - *models.AdminUserFormPageData: the initialized page model ready for route-specific fields.
func buildAdminUserFormPageData(r *http.Request, siteName, defaultTheme string) *models.AdminUserFormPageData {
	return &models.AdminUserFormPageData{
		SiteName:      siteName,
		DefaultTheme:  defaultTheme,
		ShowSearch:    true,
		CurrentUser:   currentUserViewFromRequest(r),
		NoticeMessage: adminNoticeMessage(r.URL.Query().Get("notice")),
	}
}

// handleAdminUserEdit serves the edit-user form on GET and applies submitted
// edits on POST for one administrator-managed account.
//
// Parameters:
//   - w: the HTTP response writer that should receive the result.
//   - r: the current HTTP request whose method decides whether to render or update.
//   - store: the SQLite-backed auth store used to load and update the target account.
//   - siteName: the branding label shown in the page title.
//   - defaultTheme: the HTML theme class applied to the page root.
//   - tmpl: the renderer capable of executing the admin user form template.
//   - userID: the account id being edited.
//
// Returns:
//   - none: the function writes the rendered page, redirect, or error response directly to w.
func handleAdminUserEdit(w http.ResponseWriter, r *http.Request, store *auth.Store, siteName, defaultTheme string, tmpl interface {
	ExecuteAdminUserForm(http.ResponseWriter, *models.AdminUserFormPageData) error
}, userID int64) {
	userRecord, err := store.GetUser(userID)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "Could not load user", http.StatusInternalServerError)
		return
	}

	data := buildAdminUserFormPageData(r, siteName, defaultTheme)
	data.Title = "Edit user"
	data.FormAction = "/jotes/admin/users/" + strconv.FormatInt(userID, 10)
	data.SubmitLabel = "Save changes"
	data.PasswordHelp = "Leave the password blank to keep the current password unchanged."
	data.CancelURL = "/jotes/admin"
	data.TargetUsername = userRecord.Username
	data.TargetIsAdmin = userRecord.IsAdmin
	data.IsEdit = true

	if r.Method == http.MethodGet {
		if err := tmpl.ExecuteAdminUserForm(w, data); err != nil {
			http.Error(w, "Template error", http.StatusInternalServerError)
		}
		return
	}

	if err := validateSameOriginFormPost(r); err != nil {
		http.Error(w, "Invalid user update request", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid user form", http.StatusBadRequest)
		return
	}

	data.TargetUsername = strings.TrimSpace(r.PostForm.Get("username"))
	data.TargetIsAdmin = r.PostForm.Get("is_admin") == "1"

	if err := store.UpdateUser(auth.UserUpdateInput{
		ID:       userID,
		Username: data.TargetUsername,
		Password: r.PostForm.Get("password"),
		IsAdmin:  data.TargetIsAdmin,
	}); err != nil {
		data.ErrorMessage = authErrorMessage(err)
		if execErr := tmpl.ExecuteAdminUserForm(w, data); execErr != nil {
			http.Error(w, "Template error", http.StatusInternalServerError)
		}
		return
	}

	http.Redirect(w, r, "/jotes/admin?notice=user_updated", http.StatusSeeOther)
}

// handleAdminUserDelete validates and applies one administrator-issued delete
// request for the target user id, redirecting back to the admin list page with
// a notice or error code.
//
// Parameters:
//   - w: the HTTP response writer that should receive the redirect or error response.
//   - r: the current HTTP request whose method and headers should be validated.
//   - store: the SQLite-backed auth store used to delete the target account.
//   - actingUserID: the currently signed-in administrator performing the delete.
//   - userID: the account id that should be removed.
//
// Returns:
//   - none: the function writes a redirect or error response directly to w.
func handleAdminUserDelete(w http.ResponseWriter, r *http.Request, store *auth.Store, actingUserID, userID int64) {
	if err := validateSameOriginFormPost(r); err != nil {
		http.Error(w, "Invalid user deletion request", http.StatusBadRequest)
		return
	}

	if err := store.DeleteUser(userID, actingUserID); err != nil {
		http.Redirect(w, r, "/jotes/admin?error="+adminErrorCode(err), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/jotes/admin?notice=user_deleted", http.StatusSeeOther)
}

// requireAdminRequestUser ensures the current request carries an authenticated
// administrator account before an admin-only handler continues.
//
// Parameters:
//   - w: the HTTP response writer that should receive a 403 when authorization fails.
//   - r: the current HTTP request whose auth context should be inspected.
//
// Returns:
//   - *auth.CurrentUser: the authenticated administrator account when authorization succeeds.
//   - bool: true when the request is authorized, otherwise false after writing the response.
func requireAdminRequestUser(w http.ResponseWriter, r *http.Request) (*auth.CurrentUser, bool) {
	user := auth.CurrentUserFromRequest(r)
	if user == nil || !user.IsAdmin {
		http.Error(w, "Administrator access is required", http.StatusForbidden)
		return nil, false
	}

	return user, true
}

// parseAdminUserID validates and converts the trailing user-id path segment in
// administrator user-management URLs.
//
// Parameters:
//   - rawID: the untrusted path segment extracted from the request URL.
//
// Returns:
//   - int64: the parsed positive user id.
//   - error: non-nil when rawID is empty, contains slashes, or is not a positive integer.
func parseAdminUserID(rawID string) (int64, error) {
	trimmedID := strings.TrimSpace(rawID)
	if trimmedID == "" || strings.Contains(trimmedID, "/") {
		return 0, errors.New("invalid user id")
	}

	userID, err := strconv.ParseInt(trimmedID, 10, 64)
	if err != nil || userID < 1 {
		return 0, errors.New("invalid user id")
	}

	return userID, nil
}

// loginNoticeMessage maps one login-page notice code to the user-facing text
// that should be displayed above the login form.
//
// Parameters:
//   - code: the short notice code from the login page query string.
//
// Returns:
//   - string: the user-facing notice message, or an empty string when code is unknown.
func loginNoticeMessage(code string) string {
	switch code {
	case "signed_out":
		return "You have been signed out."
	default:
		return ""
	}
}

// setupNoticeMessage maps one setup-page notice code to the user-facing text
// that should be displayed above the first-run admin form.
//
// Parameters:
//   - code: the short notice code from the setup page query string.
//
// Returns:
//   - string: the user-facing notice message, or an empty string when code is unknown.
func setupNoticeMessage(code string) string {
	switch code {
	case "login_required":
		return "Create the first administrator account before opening any notes."
	default:
		return ""
	}
}

// adminNoticeMessage maps one admin-page notice code to the user-facing text
// that should be displayed above the user list or user form.
//
// Parameters:
//   - code: the short notice code from the admin page query string.
//
// Returns:
//   - string: the user-facing notice message, or an empty string when code is unknown.
func adminNoticeMessage(code string) string {
	switch code {
	case "user_created":
		return "The new account was created successfully."
	case "user_updated":
		return "The account was updated successfully."
	case "user_deleted":
		return "The account was deleted successfully."
	default:
		return ""
	}
}

// adminErrorMessage maps one admin-page error code to the user-facing text
// that should be displayed above the user list after a redirect.
//
// Parameters:
//   - code: the short error code from the admin page query string.
//
// Returns:
//   - string: the user-facing error message, or an empty string when code is unknown.
func adminErrorMessage(code string) string {
	switch code {
	case "cannot_delete_self":
		return auth.ErrCannotDeleteSelf.Error()
	case "last_admin":
		return auth.ErrLastAdmin.Error()
	case "user_missing":
		return auth.ErrUserNotFound.Error()
	default:
		return ""
	}
}

// adminErrorCode maps one auth-store error to the short query-string code used
// when redirecting back to the admin users page after a failed delete action.
//
// Parameters:
//   - err: the auth-store error returned by a delete attempt.
//
// Returns:
//   - string: the compact error code suitable for a redirect query parameter.
func adminErrorCode(err error) string {
	switch {
	case errors.Is(err, auth.ErrCannotDeleteSelf):
		return "cannot_delete_self"
	case errors.Is(err, auth.ErrLastAdmin):
		return "last_admin"
	case errors.Is(err, auth.ErrUserNotFound):
		return "user_missing"
	default:
		return "user_missing"
	}
}

// authErrorMessage converts one auth-store validation or state error into the
// user-facing text that setup and admin forms should show inline.
//
// Parameters:
//   - err: the auth-store error returned by setup or admin form processing.
//
// Returns:
//   - string: the user-facing error message to display in the rendered form.
func authErrorMessage(err error) string {
	switch {
	case errors.Is(err, auth.ErrInvalidUsername),
		errors.Is(err, auth.ErrWeakPassword),
		errors.Is(err, auth.ErrUserExists),
		errors.Is(err, auth.ErrUsersAlreadyExist),
		errors.Is(err, auth.ErrLastAdmin),
		errors.Is(err, auth.ErrCannotDeleteSelf),
		errors.Is(err, auth.ErrUserNotFound):
		return err.Error()
	default:
		return "Jotes could not complete that account change right now."
	}
}
