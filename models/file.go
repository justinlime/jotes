// Package models defines data structures shared between handlers and templates.
package models

import (
	"html/template"
	"time"
)

// FileEntry represents one filesystem entry shown inside a directory listing.
type FileEntry struct {
	Name                             string
	Path                             string // URL path relative to the single served root (for example "/notes/todo.md")
	IsDir                            bool
	ModTime                          time.Time
	MIMEType                         string
	IsPreview                        bool // true when the entry has an inline preview route worth linking to, including image, text, and PDF previews
	IsImage            bool  // true when the entry is an image preview
	IsText             bool  // true when the entry is a text/code/document preview
	IsNote             bool  // true when the file is a Markdown, Org, or HTML note rather than an auxiliary file
	IsHidden           bool  // true when the entry should be controlled by the generic hidden-files toggle; managed .jotes companion content is excluded and uses IsJotesCompanion instead
	IsJotesCompanion   bool  // true when the entry is a managed .jotes companion directory or a descendant stored beneath one
	VisibilityMask     uint8 // bitmask describing which directory-filter combinations should keep the row visible; bit 0 is no toggles, bit 1 adds auxiliary, bit 2 adds hidden, and bit 4 adds .jotes companion visibility
}

// DirListing contains all data required to render templates/directory.html.
type DirListing struct {
	Title        string
	SiteName     string           // branding name shown in the header and page title
	CurrentUser  *CurrentUserView // authenticated user shown in the header dropdown
	ShowSearch   bool             // true when the shared header search UI should be rendered on this page
	CurrentPath  string           // URL path of the directory currently being viewed
	Breadcrumbs  []Breadcrumb
	Entries      []FileEntry
	IsRoot       bool
	DefaultTheme string // server-configured theme class applied to the page root
}

// Breadcrumb describes one segment in the breadcrumb navigation UI.
type Breadcrumb struct {
	Name string
	Path string // URL path that clicking this breadcrumb should open
}

// PreviewData contains all data required to render templates/file-preview.html.
// Exactly one of IsDir, IsImage, IsText, IsPDF, or IsBinary should be true.
type PreviewData struct {
	Title        string
	SiteName     string           // branding name shown in the header and page title
	CurrentUser  *CurrentUserView // authenticated user shown in the header dropdown
	ShowSearch   bool             // true when the shared header search UI should be rendered on this page
	DefaultTheme string           // server-configured theme class applied to the page root
	FilePath     string           // URL path of the file or directory being previewed
	FileName     string

	IsDir    bool
	IsImage  bool
	IsText   bool
	IsPDF    bool
	IsBinary bool
	CanEdit  bool

	ViewURL string // inline-serving URL used by image and PDF previews

	MIMEType         string
	ModTime          time.Time
	EntryCount       int           // direct child count for directory previews
	RenderMode       string        // "document" for Markdown/Org rich HTML, "codemirror" for read-only CM6 previews, or "" when not applicable.
	PlaintextContent string        // raw note text embedded for client-side CodeMirror previews.
	RenderedContent  template.HTML // server-rendered Markdown/Org HTML used when RenderMode == "document".

	Breadcrumbs []Breadcrumb
}

// EditorData contains all data required to render templates/edit.html.
type EditorData struct {
	Title            string
	SiteName         string           // branding name shown in the header and page title
	CurrentUser      *CurrentUserView // authenticated user shown in the header dropdown
	ShowSearch       bool             // true when the shared header search UI should be rendered on this page
	DefaultTheme     string           // server-configured theme class applied to the page root
	FilePath         string           // URL path of the file being edited
	FileName         string
	MIMEType         string
	BackURL          string
	SaveURL          string
	RenderURL        string
	ImageUploadURL   string
	Revision         string
	PlaintextContent string
	RenderMode       string        // "document" for Markdown/Org rich HTML previews, "codemirror" for read-only CM6 previews.
	RenderedContent  template.HTML // server-rendered Markdown/Org HTML used when RenderMode == "document".
	Breadcrumbs      []Breadcrumb
}
