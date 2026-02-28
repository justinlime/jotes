package handlers

import (
	"bytes"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ownExtensions is checked before the OS MIME registry to prevent misclassification
// of extensions the OS may map to unrelated types (e.g. .mod -> audio/x-mod).
var ownExtensions = map[string]string{
	// --- markup / docs ---
	".md":       "text/markdown",
	".markdown": "text/markdown",
	".rst":      "text/x-rst",
	".adoc":     "text/x-asciidoc",
	".asciidoc": "text/x-asciidoc",
	".tex":      "text/x-tex",
	".latex":    "text/x-tex",
	".textile":  "text/x-textile",
	".wiki":     "text/x-wiki",
	".org":      "text/x-org", // Emacs Org-mode
	".html":     "text/html",
	".htm":      "text/html",
	".css":      "text/css",
	".xml":      "text/xml",
	".xsl":      "text/xml",
	".xslt":     "text/xml",
	".svg":      "image/svg+xml",

	// --- data / config formats ---
	".json":       "application/json",
	".jsonc":      "application/json",
	".json5":      "application/json",
	".yaml":       "text/yaml",
	".yml":        "text/yaml",
	".toml":       "text/x-toml",
	".ini":        "text/x-ini",
	".cfg":        "text/x-ini",
	".conf":       "text/x-ini",
	".properties": "text/x-java-properties",
	".env":        "text/plain",
	".csv":        "text/csv",
	".tsv":        "text/tab-separated-values",
	".sql":        "text/x-sql",
	".graphql":    "text/x-graphql",
	".gql":        "text/x-graphql",
	".proto":      "text/x-protobuf",
	".ron":        "text/x-ron", // Rusty Object Notation
	".kdl":        "text/x-kdl", // KDL document language
	".hcl":        "text/x-hcl", // HashiCorp config language
	".tf":         "text/x-hcl", // Terraform
	".tfvars":     "text/x-hcl",
	".nix":        "text/x-nix",

	// --- Go ---
	".go":  "text/x-go",
	".mod": "text/plain", // go.mod — OS maps this to audio/x-mod
	".sum": "text/plain", // go.sum

	// --- systems / compiled ---
	".c":    "text/x-csrc",
	".h":    "text/x-csrc",
	".cpp":  "text/x-c++src",
	".cxx":  "text/x-c++src",
	".cc":   "text/x-c++src",
	".hpp":  "text/x-c++src",
	".hxx":  "text/x-c++src",
	".rs":   "text/x-rust",
	".zig":  "text/x-zig",
	".v":    "text/x-vlang",
	".odin": "text/x-odin",
	".nim":  "text/x-nim",
	".d":    "text/x-d",
	".asm":  "text/x-asm",
	".s":    "text/x-asm",

	// --- JVM ---
	".java":   "text/x-java",
	".kt":     "text/x-kotlin",
	".kts":    "text/x-kotlin",
	".scala":  "text/x-scala",
	".groovy": "text/x-groovy",
	".gradle": "text/x-groovy",

	// --- .NET / JVM adjacent ---
	".cs":  "text/x-csharp",
	".fs":  "text/x-fsharp",
	".fsi": "text/x-fsharp",
	".fsx": "text/x-fsharp",
	".vb":  "text/x-vbnet",

	// --- scripting ---
	".py":    "text/x-python",
	".rb":    "text/x-ruby",
	".php":   "text/x-php",
	".lua":   "text/x-lua",
	".pl":    "text/x-perl",
	".pm":    "text/x-perl",
	".tcl":   "text/x-tcl",
	".awk":   "text/x-awk",
	".sed":   "text/x-sed",
	".r":     "text/x-r",
	".jl":    "text/x-julia",
	".dart":  "text/x-dart",
	".swift": "text/x-swift",

	// --- shell ---
	".sh":   "text/x-sh",
	".bash": "text/x-sh",
	".zsh":  "text/x-sh",
	".fish": "text/x-fish",
	".ksh":  "text/x-sh",
	".csh":  "text/x-csh",
	".nu":   "text/x-nushell",

	// --- functional ---
	".hs":   "text/x-haskell",
	".lhs":  "text/x-haskell",
	".ml":   "text/x-ocaml",
	".mli":  "text/x-ocaml",
	".ex":   "text/x-elixir",
	".exs":  "text/x-elixir",
	".erl":  "text/x-erlang",
	".hrl":  "text/x-erlang",
	".clj":  "text/x-clojure",
	".cljs": "text/x-clojure",
	".cljc": "text/x-clojure",

	// --- web / JS ecosystem ---
	".js":     "text/javascript",
	".mjs":    "text/javascript",
	".cjs":    "text/javascript",
	".ts":     "text/typescript",
	".tsx":    "text/typescript",
	".jsx":    "text/javascript",
	".vue":    "text/x-vue",
	".svelte": "text/x-svelte",

	// --- Emacs / editor ---
	".el":    "text/x-elisp",
	".elisp": "text/x-elisp",
	".vim":   "text/x-vim",

	// --- misc text ---
	".txt":   "text/plain",
	".text":  "text/plain",
	".log":   "text/plain",
	".lock":  "text/plain",
	".diff":  "text/x-diff",
	".patch": "text/x-diff",

	// --- documents ---
	".pdf": "application/pdf",
}

// ownBaseNames matches well-known filenames (with or without a leading dot,
// compared case-insensitively after stripping a leading dot).
var ownBaseNames = map[string]string{
	// --- build systems ---
	"makefile":         "text/x-makefile",
	"gnumakefile":      "text/x-makefile",
	"bsdmakefile":      "text/x-makefile",
	"cmakefile":        "text/x-cmake",
	"cmakelists.txt":   "text/x-cmake",
	"meson.build":      "text/x-meson",
	"build.gradle":     "text/x-groovy",
	"settings.gradle":  "text/x-groovy",
	"build.gradle.kts": "text/x-kotlin",

	// --- containers / infra ---
	"dockerfile":    "text/x-dockerfile",
	"containerfile": "text/x-dockerfile",
	"vagrantfile":   "text/x-ruby",
	"jenkinsfile":   "text/x-groovy",

	// --- Ruby ---
	"gemfile":   "text/x-ruby",
	"rakefile":  "text/x-ruby",
	"guardfile": "text/x-ruby",
	"capfile":   "text/x-ruby",
	"appfile":   "text/x-ruby",
	"fastfile":  "text/x-ruby",

	// --- Go ---
	"go.mod": "text/plain",
	"go.sum": "text/plain",

	// --- process / env ---
	"procfile": "text/plain",
	"env":      "text/plain",

	// --- git ---
	".gitignore":     "text/plain",
	".gitattributes": "text/plain",
	".gitmodules":    "text/plain",
	".gitconfig":     "text/x-ini",
	".gitmessage":    "text/plain",

	// --- editor / tooling dotfiles ---
	".editorconfig":   "text/x-ini",
	".eslintrc":       "application/json",
	".prettierrc":     "application/json",
	".babelrc":        "application/json",
	".npmrc":          "text/x-ini",
	".yarnrc":         "text/plain",
	".nvmrc":          "text/plain",
	".node-version":   "text/plain",
	".python-version": "text/plain",
	".ruby-version":   "text/plain",
	".tool-versions":  "text/plain", // asdf
	".envrc":          "text/x-sh",  // direnv
	".vimrc":          "text/x-vim",
	".gvimrc":         "text/x-vim",
	"init.vim":        "text/x-vim",
	"init.lua":        "text/x-lua",
	".emacs":          "text/x-elisp",
	".tmux.conf":      "text/plain",

	// --- CI ---
	".travis.yml": "text/yaml",
	"circle.yml":  "text/yaml",
	".circleci":   "text/yaml",

	// --- human-readable project files ---
	"license":      "text/plain",
	"licence":      "text/plain",
	"copying":      "text/plain",
	"notice":       "text/plain",
	"patents":      "text/plain",
	"readme":       "text/plain",
	"authors":      "text/plain",
	"contributors": "text/plain",
	"changelog":    "text/plain",
	"changes":      "text/plain",
	"history":      "text/plain",
	"todo":         "text/plain",
	"notes":        "text/plain",
	"install":      "text/plain",
	"hacking":      "text/plain",
}

// mimeForFile resolves the MIME type for a filesystem path, using content
// sniffing only as a final fallback.
//
// Parameters:
//   - fsPath: the filesystem path whose MIME type should be determined.
//
// Returns:
//   - string: the best MIME type guess based on explicit tables, OS mappings, or content sniffing.
func mimeForFile(fsPath string) string {
	name := filepath.Base(fsPath)
	ext := strings.ToLower(filepath.Ext(name))

	// 1. Own extension table.
	if ext != "" {
		if t, ok := ownExtensions[ext]; ok {
			return t
		}
		// 2. OS registry for unrecognised extensions.
		if t := mime.TypeByExtension(ext); t != "" {
			return t
		}
	}

	// 3. Extensionless base-name table.
	if t, ok := ownBaseNames[strings.ToLower(name)]; ok {
		return t
	}

	// 4. Content sniffing — only reached for truly unknown files.
	return sniffMIME(fsPath)
}

// mimeForName resolves a MIME type using only a file name and extension table
// lookups, without reading file contents.
//
// Parameters:
//   - name: the file name or path suffix whose MIME type should be inferred.
//
// Returns:
//   - string: the MIME type inferred from explicit mappings or application/octet-stream as a fallback.
func mimeForName(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ext != "" {
		if t, ok := ownExtensions[ext]; ok {
			return t
		}
		if t := mime.TypeByExtension(ext); t != "" {
			return t
		}
	}
	if t, ok := ownBaseNames[strings.ToLower(filepath.Base(name))]; ok {
		return t
	}
	return "application/octet-stream"
}

// sniffMIME inspects the first bytes of a file to distinguish readable text
// from binary content when extension-based lookup is inconclusive.
//
// Parameters:
//   - fsPath: the filesystem path whose leading bytes should be examined.
//
// Returns:
//   - string: a sniffed MIME type, usually text/plain or application/octet-stream.
func sniffMIME(fsPath string) string {
	f, err := os.Open(fsPath)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		// Empty file — treat as plain text so it can be previewed.
		return "text/plain"
	}
	buf = buf[:n]

	// Null bytes are a reliable indicator of binary content.
	if bytes.IndexByte(buf, 0) != -1 {
		return "application/octet-stream"
	}

	// Defer to the standard library's sniffing for known binary signatures
	// (PNG, JPEG, PDF, ZIP, etc.) before declaring something text.
	if detected := http.DetectContentType(buf); !strings.HasPrefix(detected, "text/") &&
		detected != "application/octet-stream" {
		return detected
	}

	// Valid UTF-8 with no nulls → treat as plain text.
	if utf8.Valid(buf) {
		return "text/plain"
	}

	return "application/octet-stream"
}

// isImage reports whether a MIME type should be treated as an image preview.
//
// Parameters:
//   - mimeType: the MIME type to classify.
//
// Returns:
//   - bool: true when the MIME type begins with image/.
func isImage(mimeType string) bool {
	return strings.HasPrefix(mimeType, "image/")
}

// isPDF reports whether a MIME type should be treated as a PDF document
// preview instead of falling back to the generic binary-file path.
//
// Parameters:
//   - mimeType: the MIME type to classify.
//
// Returns:
//   - bool: true when mimeType resolves to application/pdf after parameter stripping.
func isPDF(mimeType string) bool {
	return baseMIME(mimeType) == "application/pdf"
}

// isText reports whether a MIME type should be treated as text for preview and
// syntax-highlighting purposes.
//
// Parameters:
//   - mimeType: the MIME type to classify.
//
// Returns:
//   - bool: true when the type is text/* or one of the explicitly allowed textual MIME types that should stay editable even when their top-level type is not text/.
func isText(mimeType string) bool {
	// Strip any parameters (e.g. "text/html; charset=utf-8").
	base := strings.SplitN(mimeType, ";", 2)[0]
	base = strings.TrimSpace(base)
	if strings.HasPrefix(base, "text/") {
		return true
	}
	switch base {
	case "application/json", "application/xml", "application/javascript", "image/svg+xml":
		return true
	}
	return false
}
