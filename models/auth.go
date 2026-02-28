package models

import "time"

// CurrentUserView contains the authenticated user data that page templates need
// for the header dropdown and admin-only navigation.
type CurrentUserView struct {
	ID       int64
	Username string
	IsAdmin  bool
}

// LoginPageData contains all data required to render the login template.
type LoginPageData struct {
	Title         string
	SiteName      string
	DefaultTheme  string
	ShowSearch    bool
	CurrentUser   *CurrentUserView
	Next          string
	Username      string
	ErrorMessage  string
	NoticeMessage string
}

// SetupPageData contains all data required to render the first-run admin setup template.
type SetupPageData struct {
	Title         string
	SiteName      string
	DefaultTheme  string
	ShowSearch    bool
	CurrentUser   *CurrentUserView
	Username      string
	ErrorMessage  string
	NoticeMessage string
}

// AdminUserEntry contains one account row for the admin user-management list.
type AdminUserEntry struct {
	ID            int64
	Username      string
	IsAdmin       bool
	IsCurrentUser bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// AdminUsersPageData contains all data required to render the admin user list page.
type AdminUsersPageData struct {
	Title         string
	SiteName      string
	DefaultTheme  string
	ShowSearch    bool
	CurrentUser   *CurrentUserView
	Users         []AdminUserEntry
	ErrorMessage  string
	NoticeMessage string
}

// AdminUserFormPageData contains all data required to render the create/edit user form page.
type AdminUserFormPageData struct {
	Title          string
	SiteName       string
	DefaultTheme   string
	ShowSearch     bool
	CurrentUser    *CurrentUserView
	ErrorMessage   string
	NoticeMessage  string
	FormAction     string
	SubmitLabel    string
	PasswordHelp   string
	CancelURL      string
	TargetUsername string
	TargetIsAdmin  bool
	IsEdit         bool
}
