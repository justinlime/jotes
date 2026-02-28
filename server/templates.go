// Package server contains the HTTP server setup and template management.
package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"jotes/models"
)

// Templates wraps the compiled template sets used for directory, preview, and editor pages.
type Templates struct {
	dir           *template.Template
	preview       *template.Template
	editor        *template.Template
	login         *template.Template
	setup         *template.Template
	adminUsers    *template.Template
	adminUserForm *template.Template
}

var tmplFuncs = template.FuncMap{
	"add":           addInt,
	"toJSON":        toJSON,
	"avatarDataURI": avatarDataURI,
}

// addInt returns the sum of two integers for use inside Go templates.
//
// Parameters:
//   - a: the first integer operand.
//   - b: the second integer operand.
//
// Returns:
//   - int: the arithmetic sum a + b.
func addInt(a, b int) int {
	return a + b
}

// toJSON marshals one template value into JSON and marks the result safe for
// direct embedding inside a non-executable application/json script tag.
//
// Parameters:
//   - value: the template value that should be serialized into JSON.
//
// Returns:
//   - template.JS: the JSON-encoded value ready for inline template output.
//   - error: non-nil when the value cannot be marshaled into JSON.
func toJSON(value interface{}) (template.JS, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	return template.JS(encoded), nil
}

// avatarDataURI builds the default inline SVG avatar used when a Jotes user
// does not have a custom profile picture. The fallback avatar always shows the
// first letter of the username in bold Catppuccin mauve on a Catppuccin mantle
// background so header menus and admin lists stay visually consistent.
//
// Parameters:
//   - username: the username whose first letter should appear in the generated fallback avatar.
//
// Returns:
//   - template.URL: a trusted data:image/svg+xml URI safe to place directly in an <img src> attribute.
func avatarDataURI(username string) template.URL {
	trimmedUsername := strings.TrimSpace(username)
	if trimmedUsername == "" {
		trimmedUsername = "Jotes"
	}

	firstRune, _ := utf8.DecodeRuneInString(trimmedUsername)
	if firstRune == utf8.RuneError {
		firstRune = 'J'
	}
	label := strings.ToUpper(string(firstRune))

	svg := fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" role="img" aria-label="%s avatar"><rect x="0" y="0" width="64" height="64" rx="18" fill="#181825"/><text x="32" y="41" text-anchor="middle" font-family="Inter, Arial, sans-serif" font-size="31" font-weight="900" fill="#cba6f7">%s</text></svg>`,
		template.HTMLEscapeString(trimmedUsername),
		template.HTMLEscapeString(label),
	)

	return template.URL("data:image/svg+xml," + url.PathEscape(svg))
}

// LoadTemplates parses the embedded templates directory into isolated template
// sets so each Jotes page, including note, auth, and admin screens, can
// provide its own {{define "content"}} block.
//
// Parameters:
//   - tfs: the embedded filesystem containing the repository's templates/ subtree.
//
// Returns:
//   - *Templates: the compiled template wrapper ready for request-time rendering.
//   - error: non-nil when any template file cannot be found or parsed.
func LoadTemplates(tfs embed.FS) (*Templates, error) {
	sub, err := fs.Sub(tfs, "templates")
	if err != nil {
		return nil, fmt.Errorf("sub fs: %w", err)
	}

	base, err := template.New("").Funcs(tmplFuncs).ParseFS(sub, "base.html")
	if err != nil {
		return nil, fmt.Errorf("parse base: %w", err)
	}

	dir, err := cloneAndParse(base, sub, "directory.html")
	if err != nil {
		return nil, fmt.Errorf("parse directory template: %w", err)
	}

	prev, err := cloneAndParse(base, sub, "file-preview.html")
	if err != nil {
		return nil, fmt.Errorf("parse preview template: %w", err)
	}

	editor, err := cloneAndParse(base, sub, "edit.html")
	if err != nil {
		return nil, fmt.Errorf("parse editor template: %w", err)
	}

	login, err := cloneAndParse(base, sub, "login.html")
	if err != nil {
		return nil, fmt.Errorf("parse login template: %w", err)
	}

	setup, err := cloneAndParse(base, sub, "setup.html")
	if err != nil {
		return nil, fmt.Errorf("parse setup template: %w", err)
	}

	adminUsers, err := cloneAndParse(base, sub, "admin-users.html")
	if err != nil {
		return nil, fmt.Errorf("parse admin users template: %w", err)
	}

	adminUserForm, err := cloneAndParse(base, sub, "admin-user-form.html")
	if err != nil {
		return nil, fmt.Errorf("parse admin user form template: %w", err)
	}

	return &Templates{
		dir:           dir,
		preview:       prev,
		editor:        editor,
		login:         login,
		setup:         setup,
		adminUsers:    adminUsers,
		adminUserForm: adminUserForm,
	}, nil
}

// cloneAndParse clones a base template set and parses one additional template
// from an fs.FS.
//
// Parameters:
//   - base: the already parsed shared template set, usually containing base.html.
//   - fsys: the filesystem that holds the additional template file.
//   - name: the file name to parse into the cloned template set.
//
// Returns:
//   - *template.Template: the cloned and extended template set.
//   - error: non-nil when cloning or parsing fails.
func cloneAndParse(base *template.Template, fsys fs.FS, name string) (*template.Template, error) {
	t, err := base.Clone()
	if err != nil {
		return nil, err
	}
	return t.ParseFS(fsys, name)
}

// ExecuteDir renders the directory listing page into the provided response writer.
//
// Parameters:
//   - w: the HTTP response writer that receives the rendered HTML.
//   - data: the directory listing view model expected by templates/directory.html.
//
// Returns:
//   - error: non-nil when template execution fails after headers are prepared.
func (t *Templates) ExecuteDir(w http.ResponseWriter, data *models.DirListing) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return t.dir.ExecuteTemplate(w, "base", data)
}

// ExecutePreview renders the file preview page into the provided response writer.
//
// Parameters:
//   - w: the HTTP response writer that receives the rendered HTML.
//   - data: the preview view model expected by templates/file-preview.html.
//
// Returns:
//   - error: non-nil when template execution fails after headers are prepared.
func (t *Templates) ExecutePreview(w http.ResponseWriter, data *models.PreviewData) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return t.preview.ExecuteTemplate(w, "base", data)
}

// ExecuteEditor renders the note editor page into the provided response writer.
//
// Parameters:
//   - w: the HTTP response writer that receives the rendered HTML.
//   - data: the editor view model expected by templates/edit.html.
//
// Returns:
//   - error: non-nil when template execution fails after headers are prepared.
func (t *Templates) ExecuteEditor(w http.ResponseWriter, data *models.EditorData) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return t.editor.ExecuteTemplate(w, "base", data)
}

// ExecuteLogin renders the login page into the provided response writer.
//
// Parameters:
//   - w: the HTTP response writer that receives the rendered HTML.
//   - data: the login page view model expected by templates/login.html.
//
// Returns:
//   - error: non-nil when template execution fails after headers are prepared.
func (t *Templates) ExecuteLogin(w http.ResponseWriter, data *models.LoginPageData) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return t.login.ExecuteTemplate(w, "base", data)
}

// ExecuteSetup renders the first-run admin setup page into the provided response writer.
//
// Parameters:
//   - w: the HTTP response writer that receives the rendered HTML.
//   - data: the setup page view model expected by templates/setup.html.
//
// Returns:
//   - error: non-nil when template execution fails after headers are prepared.
func (t *Templates) ExecuteSetup(w http.ResponseWriter, data *models.SetupPageData) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return t.setup.ExecuteTemplate(w, "base", data)
}

// ExecuteAdminUsers renders the admin user-management list page into the provided response writer.
//
// Parameters:
//   - w: the HTTP response writer that receives the rendered HTML.
//   - data: the admin users page view model expected by templates/admin-users.html.
//
// Returns:
//   - error: non-nil when template execution fails after headers are prepared.
func (t *Templates) ExecuteAdminUsers(w http.ResponseWriter, data *models.AdminUsersPageData) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return t.adminUsers.ExecuteTemplate(w, "base", data)
}

// ExecuteAdminUserForm renders the create/edit user form page into the provided response writer.
//
// Parameters:
//   - w: the HTTP response writer that receives the rendered HTML.
//   - data: the admin user form view model expected by templates/admin-user-form.html.
//
// Returns:
//   - error: non-nil when template execution fails after headers are prepared.
func (t *Templates) ExecuteAdminUserForm(w http.ResponseWriter, data *models.AdminUserFormPageData) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return t.adminUserForm.ExecuteTemplate(w, "base", data)
}
