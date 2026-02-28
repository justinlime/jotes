package handlers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RipgrepJSONMessage represents one JSON line emitted by ripgrep when the
// command runs with --json enabled.
type RipgrepJSONMessage struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber     int `json:"line_number"`
		AbsoluteOffset int `json:"absolute_offset"`
		Submatches     []struct {
			Start int `json:"start"`
			End   int `json:"end"`
		} `json:"submatches"`
	} `json:"data"`
}

// SearchResult represents one name-based or content-based search hit returned
// to the browser.
type SearchResult struct {
	Type   string   `json:"type"`
	Path   string   `json:"path"`
	Name   string   `json:"name"`
	Score  float64  `json:"score,omitempty"`
	Line   int      `json:"line,omitempty"`
	Text   string   `json:"text,omitempty"`
	Before []string `json:"before,omitempty"`
	After  []string `json:"after,omitempty"`
}

// SearchResponse is the normalized JSON payload returned by the unified search
// endpoint for filename, content, and auto search modes.
type SearchResponse struct {
	Results    []SearchResult `json:"results"`
	Total      int            `json:"total"`
	Query      string         `json:"query"`
	Mode       string         `json:"mode"`
	DurationMs int64          `json:"duration_ms"`
	Warning    string         `json:"warning,omitempty"`
}

type nameSearchCandidate struct {
	Path string
	Name string
}

// SearchHandler creates the unified search API endpoint used by the browser for
// filename, content, and auto search modes.
//
// Parameters:
//   - rootDir: the filesystem root that should be searched.
//
// Returns:
//   - http.HandlerFunc: a handler that accepts GET requests with q=<query> and mode=name|content|auto.
func SearchHandler(rootDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		mode := normalizedSearchMode(r.URL.Query().Get("mode"))

		if query == "" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(SearchResponse{
				Results: []SearchResult{},
				Total:   0,
				Query:   query,
				Mode:    mode,
			})
			return
		}

		startTime := time.Now()
		results, warning := searchResultsForMode(rootDir, mode, query)

		response := SearchResponse{
			Results:    results,
			Total:      len(results),
			Query:      query,
			Mode:       mode,
			DurationMs: time.Since(startTime).Milliseconds(),
			Warning:    warning,
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}
}

// normalizedSearchMode converts arbitrary user input into one of the supported
// search modes while preserving name search as the default fallback.
//
// Parameters:
//   - rawMode: the untrusted mode string supplied by the browser.
//
// Returns:
//   - string: one of "name", "content", or "auto".
func normalizedSearchMode(rawMode string) string {
	switch strings.ToLower(strings.TrimSpace(rawMode)) {
	case "auto":
		return "auto"
	case "content":
		return "content"
	default:
		return "name"
	}
}

// searchResultsForMode runs the backend search strategy for the requested mode
// and returns normalized results plus any non-fatal warning message.
//
// Parameters:
//   - rootDir: the filesystem root that should be searched.
//   - mode: the normalized search mode, expected to be name, content, or auto.
//   - query: the already-trimmed user query string.
//
// Returns:
//   - []SearchResult: the ordered results for the selected mode.
//   - string: an optional non-fatal warning that the browser may surface when the search was only partially successful.
func searchResultsForMode(rootDir, mode, query string) ([]SearchResult, string) {
	switch mode {
	case "content":
		return searchContentWithRipgrep(rootDir, query)
	case "auto":
		return searchAutoWithRipgrep(rootDir, query)
	default:
		return searchNameWithRipgrep(rootDir, query)
	}
}

// searchNameWithRipgrep lists candidate files through ripgrep and then applies
// the same order-independent token scoring rules that previously ran in the
// browser so filename search stays fuzzy while moving fully to the backend.
//
// Parameters:
//   - rootDir: the filesystem root whose files should be searched by path and basename.
//   - query: the already-trimmed user query string.
//
// Returns:
//   - []SearchResult: ranked filename/path hits ordered from strongest to weakest.
//   - string: an optional warning when ripgrep could not enumerate the file list cleanly.
func searchNameWithRipgrep(rootDir, query string) ([]SearchResult, string) {
	candidates, warning := listNameSearchCandidatesWithRipgrep(rootDir)
	if len(candidates) == 0 {
		return []SearchResult{}, warning
	}

	results := make([]SearchResult, 0, len(candidates))
	for _, candidate := range candidates {
		score := scoreNameSearchQuery(query, candidate)
		if score < 0 {
			continue
		}

		results = append(results, SearchResult{
			Type:  "name",
			Path:  candidate.Path,
			Name:  candidate.Name,
			Score: score,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].Name != results[j].Name {
			return strings.ToLower(results[i].Name) < strings.ToLower(results[j].Name)
		}
		return strings.ToLower(results[i].Path) < strings.ToLower(results[j].Path)
	})

	if len(results) > 100 {
		results = results[:100]
	}

	return results, warning
}

// searchAutoWithRipgrep runs both filename and content search, placing filename
// hits first and omitting duplicate content hits for files that already matched
// by name.
//
// Parameters:
//   - rootDir: the filesystem root that should be searched.
//   - query: the already-trimmed user query string.
//
// Returns:
//   - []SearchResult: merged results with filename hits first and content hits afterward.
//   - string: an optional warning composed from any non-fatal name or content search issues.
func searchAutoWithRipgrep(rootDir, query string) ([]SearchResult, string) {
	nameResults, nameWarning := searchNameWithRipgrep(rootDir, query)
	contentResults, contentWarning := searchContentWithRipgrep(rootDir, query)
	warnings := joinSearchWarnings(nameWarning, contentWarning)

	mergedResults := make([]SearchResult, 0, len(nameResults)+len(contentResults))
	seenPaths := make(map[string]struct{}, len(nameResults))

	for _, result := range nameResults {
		mergedResults = append(mergedResults, result)
		seenPaths[result.Path] = struct{}{}
	}

	for _, result := range contentResults {
		if _, exists := seenPaths[result.Path]; exists {
			continue
		}
		mergedResults = append(mergedResults, result)
	}

	if len(mergedResults) > 100 {
		mergedResults = mergedResults[:100]
	}

	return mergedResults, warnings
}

// listNameSearchCandidatesWithRipgrep enumerates every searchable file path via
// ripgrep so filename search can reuse ripgrep's filesystem traversal speed
// while leaving fuzzy scoring to Go.
//
// Parameters:
//   - rootDir: the filesystem root whose files should be listed.
//
// Returns:
//   - []nameSearchCandidate: every candidate file path returned by ripgrep, relative to rootDir.
//   - string: an optional warning when ripgrep could not enumerate files cleanly.
func listNameSearchCandidatesWithRipgrep(rootDir string) ([]nameSearchCandidate, string) {
	var stderr bytes.Buffer
	args := append([]string{"--files", "--hidden", "--no-ignore"}, ripgrepExcludeArgs()...)
	cmd := exec.Command("rg", args...)
	cmd.Dir = rootDir
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("name-search: failed to create stdout pipe: %v", err)
		return []nameSearchCandidate{}, "Filename search is temporarily unavailable."
	}

	if err := cmd.Start(); err != nil {
		log.Printf("name-search: failed to start ripgrep: %v", err)
		return []nameSearchCandidate{}, "Filename search is temporarily unavailable."
	}

	candidates := make([]nameSearchCandidate, 0, 256)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		relPath := normalizeRelativeSearchPath(scanner.Text())
		if relPath == "" {
			continue
		}

		candidates = append(candidates, nameSearchCandidate{
			Path: "/" + relPath,
			Name: filepath.Base(relPath),
		})
	}

	warning := ""
	if err := scanner.Err(); err != nil {
		log.Printf("name-search: failed to read ripgrep file list: %v", err)
		warning = "Filename search returned partial results."
	}

	if err := cmd.Wait(); err != nil {
		stderrText := normalizeRipgrepStderr(stderr.String())
		if exitError, ok := err.(*exec.ExitError); ok {
			switch exitError.ExitCode() {
			case 1:
				// No files matched the file-listing request, which is equivalent to an empty result set.
			default:
				if stderrText != "" {
					log.Printf("name-search: ripgrep exited with code %d: %s", exitError.ExitCode(), stderrText)
				} else {
					log.Printf("name-search: ripgrep exited with code %d", exitError.ExitCode())
				}
				if len(candidates) == 0 {
					warning = joinSearchWarnings(warning, "Filename search is temporarily unavailable.")
				} else {
					warning = joinSearchWarnings(warning, "Filename search returned partial results.")
				}
			}
		} else {
			log.Printf("name-search: ripgrep error: %v", err)
			if len(candidates) == 0 {
				warning = joinSearchWarnings(warning, "Filename search is temporarily unavailable.")
			} else {
				warning = joinSearchWarnings(warning, "Filename search returned partial results.")
			}
		}
	}

	return candidates, warning
}

// scoreNameSearchQuery applies the same token-by-token filename scoring rules
// across both basename and full path, while giving basename matches a small
// preference so obvious filename hits stay near the top.
//
// Parameters:
//   - query: the raw user-entered filename query.
//   - candidate: one candidate file path and basename returned by ripgrep.
//
// Returns:
//   - float64: a non-negative score for matches, or -1 when any token cannot be found.
func scoreNameSearchQuery(query string, candidate nameSearchCandidate) float64 {
	tokens := strings.Fields(strings.ToLower(query))
	if len(tokens) == 0 {
		return -1
	}

	nameLower := strings.ToLower(candidate.Name)
	pathLower := strings.ToLower(candidate.Path)
	totalScore := 0.0

	for _, token := range tokens {
		nameScore := scoreSearchToken(token, nameLower)
		pathScore := scoreSearchToken(token, pathLower)
		if nameScore < 0 && pathScore < 0 {
			return -1
		}

		if nameScore >= 0 {
			nameScore += 5
		}
		if nameScore > pathScore {
			totalScore += nameScore
		} else {
			totalScore += pathScore
		}
	}

	return totalScore
}

// scoreSearchToken scores one already-lowercased token against one already-
// lowercased candidate string using substring, boundary-acronym, and ordered
// fuzzy matching tiers.
//
// Parameters:
//   - token: the lowercase search token being matched.
//   - value: the lowercase candidate string to inspect.
//
// Returns:
//   - float64: a non-negative score for matches, or -1 when the token cannot be matched.
func scoreSearchToken(token, value string) float64 {
	if token == "" {
		return 0
	}
	if value == "" {
		return -1
	}

	if strings.Contains(value, token) {
		return 40
	}

	isBoundary := make([]bool, len(value))
	if len(isBoundary) > 0 {
		isBoundary[0] = true
	}
	for index := 1; index < len(value); index++ {
		switch value[index-1] {
		case '/', '-', '_', '.', ' ':
			isBoundary[index] = true
		}
	}

	tokenIndex := 0
	for valueIndex := 0; valueIndex < len(value) && tokenIndex < len(token); valueIndex++ {
		if isBoundary[valueIndex] && value[valueIndex] == token[tokenIndex] {
			tokenIndex++
		}
	}
	if tokenIndex == len(token) {
		return 20
	}

	orderedScore := 0.0
	tokenIndex = 0
	lastMatch := -1
	for valueIndex := 0; valueIndex < len(value) && tokenIndex < len(token); valueIndex++ {
		if value[valueIndex] != token[tokenIndex] {
			continue
		}

		bonus := 1.0
		if valueIndex == lastMatch+1 {
			bonus += 4
		}
		if isBoundary[valueIndex] {
			bonus += 2
		}
		orderedScore += bonus
		lastMatch = valueIndex
		tokenIndex++
	}
	if tokenIndex < len(token) {
		return -1
	}

	return orderedScore * float64(len(token)) / float64(len(value))
}

// searchContentWithRipgrep performs literal, case-insensitive content search
// through ripgrep's JSON output so users can enter punctuation safely without
// triggering regex parse failures.
//
// Parameters:
//   - rootDir: the filesystem root that should be searched.
//   - query: the already-trimmed user query string.
//
// Returns:
//   - []SearchResult: matching content hits with optional surrounding context lines.
//   - string: an optional warning when ripgrep reported a non-fatal issue and only partial results may be available.
func searchContentWithRipgrep(rootDir, query string) ([]SearchResult, string) {
	results := make([]SearchResult, 0, 32)
	warning := ""
	var stderr bytes.Buffer

	args := append([]string{
		"--json",
		"-C", "2",
		"-i",
		"--fixed-strings",
		"--max-count", "100",
		"--hidden",
		"--no-ignore",
	}, ripgrepExcludeArgs()...)
	args = append(args, "--", query, ".")

	cmd := exec.Command("rg", args...)
	cmd.Dir = rootDir
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("content-search: failed to create stdout pipe: %v", err)
		return results, "Content search is temporarily unavailable."
	}

	if err := cmd.Start(); err != nil {
		log.Printf("content-search: failed to start ripgrep: %v", err)
		return results, "Content search is temporarily unavailable."
	}

	type pendingMatch struct {
		result SearchResult
		before []string
	}

	var currentPending *pendingMatch
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		var message RipgrepJSONMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			log.Printf("content-search: failed to parse ripgrep JSON: %v", err)
			warning = joinSearchWarnings(warning, "Content search returned partial results.")
			continue
		}

		switch message.Type {
		case "match":
			if currentPending != nil && currentPending.result.Path != "" {
				currentPending.result.Before = reverseStrings(currentPending.before)
				results = append(results, currentPending.result)
			}

			relPath := normalizeRelativeSearchPath(message.Data.Path.Text)
			currentPending = &pendingMatch{
				result: SearchResult{
					Type:  "content",
					Path:  "/" + relPath,
					Name:  filepath.Base(relPath),
					Line:  message.Data.LineNumber,
					Text:  strings.TrimSpace(message.Data.Lines.Text),
					Score: 1,
				},
				before: []string{},
			}
		case "context":
			if currentPending == nil {
				continue
			}

			contextText := strings.TrimSpace(message.Data.Lines.Text)
			if contextText == "" {
				continue
			}

			if message.Data.LineNumber < currentPending.result.Line {
				currentPending.before = append(currentPending.before, contextText)
			} else if message.Data.LineNumber > currentPending.result.Line {
				currentPending.result.After = append(currentPending.result.After, contextText)
			}
		}
	}

	if currentPending != nil && currentPending.result.Path != "" {
		currentPending.result.Before = reverseStrings(currentPending.before)
		results = append(results, currentPending.result)
	}

	if err := scanner.Err(); err != nil {
		if err.Error() == "bufio.Scanner: token too long" {
			log.Printf("content-search: skipped an extremely long line and returned %d partial results", len(results))
			warning = joinSearchWarnings(warning, "Content search skipped an extremely long line.")
		} else {
			log.Printf("content-search: error reading ripgrep output: %v", err)
			warning = joinSearchWarnings(warning, "Content search returned partial results.")
		}
	}

	if err := cmd.Wait(); err != nil {
		stderrText := normalizeRipgrepStderr(stderr.String())
		if exitError, ok := err.(*exec.ExitError); ok {
			switch exitError.ExitCode() {
			case 1:
				// No matches found.
			default:
				if stderrText != "" {
					log.Printf("content-search: ripgrep exited with code %d: %s", exitError.ExitCode(), stderrText)
				} else {
					log.Printf("content-search: ripgrep exited with code %d", exitError.ExitCode())
				}
				if len(results) == 0 {
					warning = joinSearchWarnings(warning, "Content search is temporarily unavailable.")
				} else {
					warning = joinSearchWarnings(warning, "Content search returned partial results.")
				}
			}
		} else {
			log.Printf("content-search: ripgrep error: %v", err)
			if len(results) == 0 {
				warning = joinSearchWarnings(warning, "Content search is temporarily unavailable.")
			} else {
				warning = joinSearchWarnings(warning, "Content search returned partial results.")
			}
		}
	}

	if len(results) > 100 {
		results = results[:100]
	}

	return results, warning
}

// ripgrepExcludeArgs returns the exclusion globs shared by every ripgrep-based
// search so Jotes skips large generated or dependency directories while still
// scanning hidden files and files ignored by Git when they live beneath the
// configured note root.
//
// Parameters:
//   - none: the exclusion set is fixed for the current application.
//
// Returns:
//   - []string: ripgrep CLI arguments that exclude directories by glob pattern.
func ripgrepExcludeArgs() []string {
	return []string{
		"-g", "!node_modules/**",
		"-g", "!.git/**",
		"-g", "!vendor/**",
		"-g", "!__pycache__/**",
		"-g", "!.egg-info/**",
		"-g", "!dist/**",
		"-g", "!build/**",
		"-g", "!.next/**",
		"-g", "!.nuxt/**",
		"-g", "!.idea/**",
		"-g", "!.vscode/**",
		"-g", "!coverage/**",
	}
}

// joinSearchWarnings merges one or more user-facing warning strings into a
// single de-duplicated sentence sequence.
//
// Parameters:
//   - warnings: zero or more warning strings that may be empty or repeated.
//
// Returns:
//   - string: the combined warning text, or an empty string when every warning was empty.
func joinSearchWarnings(warnings ...string) string {
	seenWarnings := make(map[string]struct{}, len(warnings))
	mergedWarnings := make([]string, 0, len(warnings))

	for _, warning := range warnings {
		warning = strings.TrimSpace(warning)
		if warning == "" {
			continue
		}
		if _, exists := seenWarnings[warning]; exists {
			continue
		}
		seenWarnings[warning] = struct{}{}
		mergedWarnings = append(mergedWarnings, warning)
	}

	return strings.Join(mergedWarnings, " ")
}

// normalizeRipgrepStderr compacts ripgrep stderr into one log-friendly line so
// parse errors and filesystem warnings are readable in server logs.
//
// Parameters:
//   - stderrText: the raw stderr text emitted by ripgrep.
//
// Returns:
//   - string: stderr with repeated whitespace collapsed and surrounding space removed.
func normalizeRipgrepStderr(stderrText string) string {
	return strings.Join(strings.Fields(stderrText), " ")
}

// normalizeRelativeSearchPath converts one ripgrep-reported relative path into
// the clean slash-delimited form that Jotes uses for preview links and result
// de-duplication.
//
// Parameters:
//   - rawPath: the relative path text emitted by ripgrep, which may include prefixes such as ./.
//
// Returns:
//   - string: the cleaned relative path without a leading slash or ./ prefix.
func normalizeRelativeSearchPath(rawPath string) string {
	cleanedPath := filepath.ToSlash(filepath.Clean(strings.TrimSpace(rawPath)))
	cleanedPath = strings.TrimPrefix(cleanedPath, "./")
	cleanedPath = strings.TrimPrefix(cleanedPath, "/")
	if cleanedPath == "." {
		return ""
	}
	return cleanedPath
}

// reverseStrings reverses the order of strings in a slice so context lines
// collected before a match are returned in natural top-to-bottom order.
//
// Parameters:
//   - values: the slice whose order should be reversed in place.
//
// Returns:
//   - []string: the same slice after its elements have been reversed.
func reverseStrings(values []string) []string {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
	return values
}
