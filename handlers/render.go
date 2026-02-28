package handlers

import (
	"bytes"
	"fmt"
	"html/template"
	"path"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/microcosm-cc/bluemonday"
	"github.com/niklasfasching/go-org/org"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
	nethtml "golang.org/x/net/html"
)

// renderTheme is the Chroma style name used for code block highlighting inside
// document renders (Markdown, Org-mode). Defaults to catppuccin-mocha; set
// once at startup by InitRenderOptions.
var renderTheme = "catppuccin-mocha"

// docPolicy is the bluemonday sanitization policy applied to all rendered
// document output. Nil until InitRenderOptions is called; sanitizeHTML falls
// back to a conservative default if called before initialisation.
var docPolicy *bluemonday.Policy

// InitRenderOptions configures the shared document-rendering state used for
// rendered note previews.
//
// Parameters:
//   - theme: the Chroma style name used for syntax-highlighted code blocks inside rendered documents.
//
// Returns:
//   - none: package-level renderer settings are updated in place for future preview requests.
func InitRenderOptions(theme string) {
	renderTheme = theme
	docPolicy = buildDocPolicy()
}

// buildDocPolicy constructs the bluemonday allowlist policy used to sanitize
// rendered Markdown and Org-mode output.
//
// Parameters:
//   - none: the policy is built from the application's fixed note-rendering rules.
//
// Returns:
//   - *bluemonday.Policy: a sanitizer policy that preserves safe formatting, links, and images.
func buildDocPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()

	// --- Structural / block elements (including <div> for Markdown HTML blocks) ---
	p.AllowElements(
		"address", "article", "aside",
		"blockquote", "br",
		"caption", "col", "colgroup",
		"details", "div", "dl", "dt", "dd",
		"figure", "figcaption", "footer",
		"h1", "h2", "h3", "h4", "h5", "h6",
		"header", "hr",
		"li",
		"main",
		"nav",
		"ol",
		"p", "pre",
		"section", "summary",
		"table", "tbody", "td", "tfoot", "th", "thead", "tr",
		"ul",
	)

	// --- Inline / typographic elements ---
	p.AllowElements(
		"abbr", "acronym",
		"b", "cite", "code",
		"del", "dfn",
		"em",
		"i",
		"kbd",
		"mark",
		"q",
		"s", "samp", "small", "span", "strong", "sub", "sup",
		"tt",
		"u",
		"var", "wbr",
	)

	// --- Links ---
	// Only http, https, and mailto are permitted as href schemes.
	// Relative URLs (e.g. anchor links within a document) are also allowed.
	// RequireParseableURLs is intentionally NOT set: some real-world URLs (e.g.
	// badge URLs with unencoded spaces) fail strict RFC parsing but are otherwise
	// safe. The scheme allowlist below already prevents dangerous schemes such as
	// javascript: or vbscript: regardless of parseability.
	p.AllowAttrs("href", "title").OnElements("a")
	p.AllowURLSchemes("http", "https", "mailto")
	p.AllowRelativeURLs(true)

	// --- Images ---
	p.AllowAttrs("src", "alt", "title", "width", "height").OnElements("img")
	// Permits data: URIs for common raster image types only.
	// SVG data URIs are intentionally excluded by bluemonday because SVG
	// documents can contain <script> elements.
	p.AllowDataURIImages()

	// --- Global safe attributes ---
	// id and class are needed for heading anchors (goldmark WithAutoHeadingID)
	// and for the Chroma CSS class names on highlighted <code>/<span> elements.
	// align is needed for raw HTML blocks in Markdown/Org documents that use
	// e.g. <h1 align="center"> or <p align="center"> for centred content.
	p.AllowAttrs("id", "class", "lang", "title", "align").Globally()

	// --- Table layout attributes ---
	p.AllowAttrs("align", "valign", "colspan", "rowspan", "scope", "abbr", "headers").OnElements("td", "th")
	p.AllowAttrs("align", "valign", "span", "width").OnElements("col", "colgroup")
	p.AllowAttrs("align").OnElements("table", "tr", "tbody", "thead", "tfoot")
	p.AllowAttrs("border", "cellpadding", "cellspacing", "summary", "width").OnElements("table")

	// --- List attributes ---
	p.AllowAttrs("start", "type").OnElements("ol")
	p.AllowAttrs("type").OnElements("ul", "li")

	// --- Quotation source ---
	p.AllowAttrs("cite").OnElements("blockquote", "del", "q")

	return p
}

// isRenderable reports whether a MIME type should keep using Jotes' rich
// document renderers instead of the read-only CodeMirror preview.
//
// Parameters:
//   - mimeType: the detected MIME type to evaluate for rich rendered-preview support.
//
// Returns:
//   - bool: true when the note should be rendered as Markdown, Org, or HTML instead of read-only source preview.
func isRenderable(mimeType string) bool {
	switch baseMIME(mimeType) {
	case "text/markdown", "text/x-org", "text/html":
		return true
	}
	return false
}

// renderContent attempts a rich render for one note document based on its MIME type.
// Markdown and Org notes render into sanitized HTML fragments, while HTML notes
// render inside a sandboxed iframe so the original document layout is preserved
// without injecting the note directly into the application DOM.
//
// Parameters:
//   - content: the raw document contents to render.
//   - mimeType: the detected MIME type used to choose the renderer.
//   - docURLPath: the preview URL path of the note itself, used to rewrite relative asset paths for Markdown/Org and to seed HTML iframe base URLs.
//
// Returns:
//   - template.HTML: the rendered HTML fragment ready for insertion into the preview template.
//   - error: non-nil when no rich renderer exists for mimeType or the selected renderer fails.
func renderContent(content, mimeType, docURLPath string) (template.HTML, error) {
	switch baseMIME(mimeType) {
	case "text/markdown":
		return renderMarkdown(content, path.Dir(docURLPath))
	case "text/x-org":
		return renderOrg(content, path.Dir(docURLPath))
	case "text/html":
		return renderHTML(content, docURLPath)
	}
	return "", fmt.Errorf("no renderer for %q", mimeType)
}

// renderMarkdown converts Markdown note content into sanitized HTML using
// goldmark plus Chroma syntax highlighting for fenced code blocks, then wraps
// body content that belongs directly to any heading so CSS can indent those
// section bodies without shifting the headings themselves.
//
// Parameters:
//   - content: the raw Markdown source to render.
//   - docURLDir: the preview URL directory used to rewrite relative image paths.
//
// Returns:
//   - template.HTML: sanitized rendered HTML suitable for inline preview output.
//   - error: non-nil when goldmark fails to render the document.
func renderMarkdown(content, docURLDir string) (template.HTML, error) {
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM, // tables, strikethrough, linkify, task lists
			extension.Footnote,
			extension.DefinitionList,
			extension.Typographer,
			highlighting.NewHighlighting(
				highlighting.WithStyle(renderTheme),
				highlighting.WithFormatOptions(
					chromahtml.WithClasses(true),
				),
			),
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			goldmarkhtml.WithUnsafe(), // raw HTML blocks pass through; sanitized below
		),
	)
	var buf bytes.Buffer
	if err := md.Convert([]byte(content), &buf); err != nil {
		return "", fmt.Errorf("markdown render: %w", err)
	}

	renderedHTML := wrapMarkdownHeadingBodies(buf.String())
	return template.HTML(sanitizeHTML(renderedHTML, docURLDir)), nil
}

// wrapMarkdownHeadingBodies rewrites one rendered Markdown HTML fragment so
// non-heading nodes that belong directly to any heading are wrapped in
// dedicated container elements. The wrappers let CSS indent only those
// section bodies while keeping every heading and every deeper subsection
// heading aligned with the main document flow.
//
// Parameters:
//   - input: the rendered Markdown HTML fragment before sanitization.
//
// Returns:
//   - string: the same fragment with wrapper divs inserted after h1-h6 elements when parsing succeeds, or the original fragment when the HTML fragment cannot be safely rewritten.
func wrapMarkdownHeadingBodies(input string) string {
	if !strings.Contains(input, "<h") {
		return input
	}

	fragmentRoot := &nethtml.Node{Type: nethtml.ElementNode, Data: "div"}
	parsedNodes, err := nethtml.ParseFragment(strings.NewReader(input), fragmentRoot)
	if err != nil {
		return input
	}

	for _, parsedNode := range parsedNodes {
		fragmentRoot.AppendChild(parsedNode)
	}

	var fragmentChildren []*nethtml.Node
	for child := fragmentRoot.FirstChild; child != nil; {
		nextSibling := child.NextSibling
		fragmentRoot.RemoveChild(child)
		if !isIgnorableFragmentNode(child) {
			fragmentChildren = append(fragmentChildren, child)
		}
		child = nextSibling
	}

	for index := 0; index < len(fragmentChildren); index++ {
		child := fragmentChildren[index]
		fragmentRoot.AppendChild(child)

		headingLevel, shouldWrapBody := markdownHeadingIndentLevel(child)
		if !shouldWrapBody {
			continue
		}

		bodyWrapper := newMarkdownHeadingBodyWrapper(headingLevel)
		hasBodyContent := false
		for index+1 < len(fragmentChildren) {
			nextChild := fragmentChildren[index+1]
			if isHTMLHeadingNode(nextChild) {
				break
			}
			index++
			bodyWrapper.AppendChild(nextChild)
			hasBodyContent = true
		}

		if hasBodyContent {
			fragmentRoot.AppendChild(bodyWrapper)
		}
	}

	var buf bytes.Buffer
	for child := fragmentRoot.FirstChild; child != nil; child = child.NextSibling {
		if err := nethtml.Render(&buf, child); err != nil {
			return input
		}
	}

	return buf.String()
}

// isIgnorableFragmentNode reports whether one top-level parsed HTML node only
// exists to preserve insignificant whitespace between block elements inside a
// rendered document fragment.
//
// Parameters:
//   - node: the parsed HTML node being evaluated.
//
// Returns:
//   - bool: true when node is a whitespace-only text node that can be dropped without changing the rendered document meaning.
func isIgnorableFragmentNode(node *nethtml.Node) bool {
	return node.Type == nethtml.TextNode && strings.TrimSpace(node.Data) == ""
}

// markdownHeadingIndentLevel reports whether one parsed Markdown fragment node
// is a heading whose direct body content should receive the preview indent
// wrapper.
//
// Parameters:
//   - node: the parsed HTML node being evaluated.
//
// Returns:
//   - int: the numeric heading level, from 1 through 6, when node should own an indented body wrapper.
//   - bool: true when node is an h1, h2, h3, h4, h5, or h6 element that should trigger body wrapping.
func markdownHeadingIndentLevel(node *nethtml.Node) (int, bool) {
	if !isHTMLHeadingNode(node) {
		return 0, false
	}

	return int(node.Data[1] - '0'), true
}

// isHTMLHeadingNode reports whether one parsed HTML node is any heading
// element from h1 through h6. Markdown body wrappers stop before the next
// heading so deeper subsection headings do not inherit their parent section's
// left indent.
//
// Parameters:
//   - node: the parsed HTML node being evaluated.
//
// Returns:
//   - bool: true when node is an h1, h2, h3, h4, h5, or h6 element.
func isHTMLHeadingNode(node *nethtml.Node) bool {
	if node.Type != nethtml.ElementNode || len(node.Data) != 2 || node.Data[0] != 'h' {
		return false
	}

	return node.Data[1] >= '1' && node.Data[1] <= '6'
}

// newMarkdownHeadingBodyWrapper constructs one wrapper element for the body
// content that belongs directly to a rendered Markdown heading.
//
// Parameters:
//   - headingLevel: the source heading level, from 1 through 6, that owns the wrapped body content.
//
// Returns:
//   - *nethtml.Node: a detached div node whose CSS classes identify it as an indented Markdown section body.
func newMarkdownHeadingBodyWrapper(headingLevel int) *nethtml.Node {
	return &nethtml.Node{
		Type: nethtml.ElementNode,
		Data: "div",
		Attr: []nethtml.Attribute{{
			Key: "class",
			Val: fmt.Sprintf("rendered-heading-body rendered-heading-body--level-%d", headingLevel),
		}},
	}
}

// renderOrg converts Org-mode note content into sanitized HTML using go-org
// plus Chroma syntax highlighting for source blocks.
//
// Parameters:
//   - content: the raw Org-mode source to render.
//   - docURLDir: the preview URL directory used to rewrite relative image paths.
//
// Returns:
//   - template.HTML: sanitized rendered HTML suitable for inline preview output.
//   - error: non-nil when go-org fails to render the document.
func renderOrg(content, docURLDir string) (template.HTML, error) {
	doc := org.New().Parse(strings.NewReader(content), "")
	w := org.NewHTMLWriter()
	w.HighlightCodeBlock = func(source, lang string, inline bool, _ map[string]string) string {
		return chromaHighlightBlock(source, lang)
	}
	out, err := doc.Write(w)
	if err != nil {
		return "", fmt.Errorf("org render: %w", err)
	}
	return template.HTML(sanitizeHTML(out, docURLDir)), nil
}

// chromaHighlightBlock syntax-highlights one source block for rendered Org-mode previews.
//
// Parameters:
//   - source: the raw code block contents to highlight.
//   - lang: the declared language name supplied by the document renderer.
//
// Returns:
//   - string: a Chroma-generated HTML fragment, or an empty string when highlighting fails and the caller should fall back.
func chromaHighlightBlock(source, lang string) string {
	l := lexers.Get(lang)
	if l == nil {
		l = lexers.Fallback
	}
	l = chroma.Coalesce(l)

	style := styles.Get(renderTheme)
	if style == nil {
		style = styles.Fallback
	}

	f := chromahtml.New(
		chromahtml.WithClasses(true),
	)

	it, err := l.Tokenise(nil, source)
	if err != nil {
		return ""
	}

	var buf bytes.Buffer
	if err := f.Format(&buf, style, it); err != nil {
		return ""
	}
	return buf.String()
}

// renderHTML wraps one HTML note in a sandboxed iframe so the note keeps its
// own document rendering behavior without being injected directly into the
// surrounding Jotes page DOM.
//
// Parameters:
//   - content: the raw HTML document or fragment that should be previewed.
//   - docURLPath: the preview URL path of the HTML note, used to seed a <base> tag so relative assets keep resolving like the original file.
//
// Returns:
//   - template.HTML: a sandboxed iframe element whose srcdoc contains the escaped HTML note.
//   - error: always nil today; the second return value keeps the renderer interface uniform with Markdown and Org.
func renderHTML(content, docURLPath string) (template.HTML, error) {
	previewDocument := buildHTMLPreviewDocument(content, docURLPath)
	escaped := htmlAttrEscape(previewDocument)
	iframe := `<iframe class="html-preview-frame" srcdoc="` + escaped + `" sandbox="allow-scripts" referrerpolicy="no-referrer"></iframe>`
	return template.HTML(iframe), nil
}

// buildHTMLPreviewDocument prepares one HTML note for iframe srcdoc rendering
// while preserving relative asset resolution against the note's /view path.
//
// Parameters:
//   - content: the raw HTML document or fragment that should be previewed.
//   - docURLPath: the preview URL path of the HTML note whose /view location should be used as the base URL.
//
// Returns:
//   - string: an HTML document or fragment with an injected <base> tag so relative resources resolve like the original file.
func buildHTMLPreviewDocument(content, docURLPath string) string {
	baseTag := `<base href="` + htmlAttrEscape(`/view`+docURLPath) + `">`
	lowerContent := strings.ToLower(content)

	if headIndex := strings.Index(lowerContent, "<head"); headIndex >= 0 {
		if tagEndIndex := strings.Index(content[headIndex:], ">"); tagEndIndex >= 0 {
			insertionIndex := headIndex + tagEndIndex + 1
			return content[:insertionIndex] + baseTag + content[insertionIndex:]
		}
	}

	if htmlIndex := strings.Index(lowerContent, "<html"); htmlIndex >= 0 {
		if tagEndIndex := strings.Index(content[htmlIndex:], ">"); tagEndIndex >= 0 {
			insertionIndex := htmlIndex + tagEndIndex + 1
			return content[:insertionIndex] + `<head>` + baseTag + `</head>` + content[insertionIndex:]
		}
	}

	return `<!DOCTYPE html><html><head>` + baseTag + `</head><body>` + content + `</body></html>`
}

// buildRenderStatusMessageHTML builds one small safe paragraph fragment that
// can be embedded inside rendered-preview containers when a rich renderer is
// unavailable or fails.
//
// Parameters:
//   - className: the CSS class applied to the generated paragraph element.
//   - message: the user-facing text that should be shown inside the paragraph.
//
// Returns:
//   - template.HTML: a safe paragraph fragment with escaped class and message text.
func buildRenderStatusMessageHTML(className, message string) template.HTML {
	return template.HTML(`<p class="` + template.HTMLEscapeString(className) + `">` + template.HTMLEscapeString(message) + `</p>`)
}

// baseMIME strips parameters from a MIME type string so routing logic can
// compare only the stable media type.
//
// Parameters:
//   - mimeType: a MIME string that may include parameters such as charset.
//
// Returns:
//   - string: the media type without any trailing parameters.
func baseMIME(mimeType string) string {
	return strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0])
}

// sanitizeHTML rewrites image URLs and sanitizes rendered document HTML before
// it is embedded into a preview page.
//
// Parameters:
//   - input: the raw rendered HTML fragment produced by a document renderer.
//   - docURLDir: the preview URL directory used when rewriting relative image paths.
//
// Returns:
//   - string: sanitized HTML that is safe to embed in the Jotes preview templates.
func sanitizeHTML(input, docURLDir string) string {
	input = rewriteImgSrcURLs(input, docURLDir)
	p := docPolicy
	if p == nil {
		p = buildDocPolicy()
	}
	return p.Sanitize(input)
}

// rewriteImgSrcURLs rewrites img src attributes in rendered HTML so note
// images resolve correctly through the server's /view/ route.
//
// Parameters:
//   - html: the rendered HTML fragment whose image sources should be normalized.
//   - docURLDir: the preview URL directory used to resolve relative image paths.
//
// Returns:
//   - string: HTML with image source attributes rewritten for safe preview rendering.
func rewriteImgSrcURLs(html, docURLDir string) string {
	const needle = `src="`
	if !strings.Contains(html, needle) {
		return html
	}

	var b strings.Builder
	b.Grow(len(html) + 64)
	remaining := html
	for {
		idx := strings.Index(remaining, needle)
		if idx == -1 {
			b.WriteString(remaining)
			break
		}
		// Copy everything up to and including src="
		b.WriteString(remaining[:idx+len(needle)])
		remaining = remaining[idx+len(needle):]

		// Find the closing quote of the attribute value.
		end := strings.IndexByte(remaining, '"')
		if end == -1 {
			// Malformed — emit as-is and stop.
			b.WriteString(remaining)
			break
		}
		rawSrc := remaining[:end]
		remaining = remaining[end:] // closing quote stays in remaining

		b.WriteString(resolveImgSrc(rawSrc, docURLDir))
	}
	return b.String()
}

// resolveImgSrc transforms one image source value into the URL that a preview
// page should actually use.
//
// Parameters:
//   - src: the original image source value found in rendered HTML.
//   - docURLDir: the preview URL directory used to resolve relative paths.
//
// Returns:
//   - string: the normalized image source that should be embedded in the final preview HTML.
func resolveImgSrc(src, docURLDir string) string {
	switch {
	case strings.HasPrefix(src, "https://") || strings.HasPrefix(src, "http://"):
		return encodeURLSpaces(src)

	case src == "" || strings.HasPrefix(src, "data:") || strings.HasPrefix(src, "#"):
		return src

	case strings.HasPrefix(src, "/"):
		// Already an absolute path — pass through. This covers cases like
		// /view/… paths that a document author has written explicitly.
		return src

	default:
		// Relative path — resolve against the document's directory using the
		// /view/ route so ViewHandler can serve the file inline.
		//
		// path.Join cleans away any ".." or "." segments, which prevents a
		// crafted relative path from escaping the served directory tree. The
		// resolvePath check in ViewHandler adds a second layer of defence.
		joined := path.Join("/view", docURLDir, src)
		return joined
	}
}

// encodeURLSpaces replaces literal spaces in a URL string with %20 so the
// sanitizer keeps otherwise valid external image URLs.
//
// Parameters:
//   - rawURL: the URL string that may contain literal space characters.
//
// Returns:
//   - string: the same URL with spaces percent-encoded.
func encodeURLSpaces(rawURL string) string {
	if !strings.Contains(rawURL, " ") {
		return rawURL
	}
	return strings.ReplaceAll(rawURL, " ", "%20")
}

// htmlAttrEscape escapes one raw string for safe embedding inside a
// double-quoted HTML attribute such as iframe srcdoc.
//
// Parameters:
//   - rawText: the raw string that should be escaped for attribute-context insertion.
//
// Returns:
//   - string: the escaped attribute-safe string.
func htmlAttrEscape(rawText string) string {
	var b strings.Builder
	b.Grow(len(rawText))
	for _, r := range rawText {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&#39;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
