package tui

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
)

const maxAutocompleteResults = 8

type autocompleteMode string

const (
	autocompleteModeFile    autocompleteMode = "file"
	autocompleteModeCommand autocompleteMode = "command"
)

type autocompleteQuery struct {
	Mode  autocompleteMode
	Start int
	End   int
	Text  string
}

type autocompleteResult struct {
	Value       string
	Description string
}

func textareaCursorIndex(input textarea.Model) int {
	value := input.Value()
	row := input.Line()
	col := input.LineInfo().StartColumn + input.LineInfo().ColumnOffset
	lines := strings.Split(value, "\n")
	if row < 0 {
		row = 0
	}
	if row >= len(lines) {
		row = len(lines) - 1
	}
	index := 0
	for i := 0; i < row; i++ {
		index += len([]rune(lines[i])) + 1
	}
	if row >= 0 && row < len(lines) {
		lineLen := len([]rune(lines[row]))
		if col > lineLen {
			col = lineLen
		}
		if col < 0 {
			col = 0
		}
		index += col
	}
	return index
}

func detectAutocompleteQuery(text string, cursor int) (autocompleteQuery, bool) {
	if query, ok := detectCommandAutocompleteQuery(text, cursor); ok {
		return query, true
	}
	return detectFileAutocompleteQuery(text, cursor)
}

func detectCommandAutocompleteQuery(text string, cursor int) (autocompleteQuery, bool) {
	runes := []rune(text)
	if len(runes) == 0 || runes[0] != '/' || cursor < 0 || cursor > len(runes) {
		return autocompleteQuery{}, false
	}
	if cursor == 0 {
		return autocompleteQuery{Mode: autocompleteModeCommand, Start: 0, End: 1, Text: ""}, true
	}
	tokenEnd := 1
	for tokenEnd < len(runes) && !isCommandBoundaryRune(runes[tokenEnd]) {
		tokenEnd++
	}
	if cursor > tokenEnd {
		return autocompleteQuery{}, false
	}
	if cursor < len(runes) && !isCommandBoundaryRune(runes[cursor]) {
		return autocompleteQuery{}, false
	}
	return autocompleteQuery{
		Mode:  autocompleteModeCommand,
		Start: 0,
		End:   cursor,
		Text:  string(runes[1:cursor]),
	}, true
}

func detectFileAutocompleteQuery(text string, cursor int) (autocompleteQuery, bool) {
	runes := []rune(text)
	if cursor < 0 || cursor > len(runes) {
		return autocompleteQuery{}, false
	}
	if cursor < len(runes) && isFileRefQueryRune(runes[cursor]) {
		return autocompleteQuery{}, false
	}
	idx := cursor - 1
	for idx >= 0 && isFileRefQueryRune(runes[idx]) {
		idx--
	}
	if idx < 0 || runes[idx] != '@' {
		return autocompleteQuery{}, false
	}
	if idx > 0 && !isFileRefBoundaryRune(runes[idx-1]) {
		return autocompleteQuery{}, false
	}
	return autocompleteQuery{
		Mode:  autocompleteModeFile,
		Start: idx,
		End:   cursor,
		Text:  string(runes[idx+1 : cursor]),
	}, true
}

func isCommandBoundaryRune(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n'
}

func isFileRefQueryRune(r rune) bool {
	return r == '/' || r == '.' || r == '_' || r == '-' || ('0' <= r && r <= '9') || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z')
}

func isFileRefBoundaryRune(r rune) bool {
	return !isFileRefQueryRune(r) && r != '@'
}

func rankCommandSearchResults(commands []slashCommand, query string, limit int) []autocompleteResult {
	if limit <= 0 {
		limit = maxAutocompleteResults
	}
	type scoredResult struct {
		command autocompleteResult
		score   int
	}
	query = strings.ToLower(strings.TrimSpace(query))
	results := make([]scoredResult, 0, len(commands))
	for _, command := range commands {
		score, ok := commandSearchScore(command.Name, query)
		if !ok {
			continue
		}
		results = append(results, scoredResult{command: autocompleteResult{Value: command.Name, Description: command.Description}, score: score})
	}
	sort.Slice(results, func(i int, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score < results[j].score
		}
		if len(results[i].command.Value) != len(results[j].command.Value) {
			return len(results[i].command.Value) < len(results[j].command.Value)
		}
		return results[i].command.Value < results[j].command.Value
	})
	if len(results) > limit {
		results = results[:limit]
	}
	items := make([]autocompleteResult, 0, len(results))
	for _, result := range results {
		items = append(items, result.command)
	}
	return items
}

func commandSearchScore(candidate string, query string) (int, bool) {
	lowerCandidate := strings.ToLower(candidate)
	if query == "" {
		return 1, true
	}
	switch {
	case strings.HasPrefix(lowerCandidate, query):
		return 0, true
	case strings.Contains(lowerCandidate, query):
		return 1, true
	default:
		return 0, false
	}
}

func rankFileSearchResults(candidates []string, query string, limit int) []autocompleteResult {
	if limit <= 0 {
		limit = maxAutocompleteResults
	}
	type scoredResult struct {
		path  string
		score int
	}
	query = strings.ToLower(strings.TrimSpace(query))
	results := make([]scoredResult, 0, len(candidates))
	for _, candidate := range candidates {
		score, ok := fileSearchScore(candidate, query)
		if !ok {
			continue
		}
		results = append(results, scoredResult{path: candidate, score: score})
	}
	sort.Slice(results, func(i int, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score < results[j].score
		}
		if len(results[i].path) != len(results[j].path) {
			return len(results[i].path) < len(results[j].path)
		}
		return results[i].path < results[j].path
	})
	if len(results) > limit {
		results = results[:limit]
	}
	paths := make([]autocompleteResult, 0, len(results))
	for _, result := range results {
		paths = append(paths, autocompleteResult{Value: result.path})
	}
	return paths
}

func fileSearchScore(candidate string, query string) (int, bool) {
	lowerCandidate := strings.ToLower(filepath.ToSlash(candidate))
	if query == "" {
		return 4, true
	}
	base := strings.ToLower(filepath.Base(lowerCandidate))
	switch {
	case strings.HasPrefix(lowerCandidate, query):
		return 0, true
	case strings.HasPrefix(base, query):
		return 1, true
	case strings.Contains(lowerCandidate, "/"+query):
		return 2, true
	case strings.Contains(lowerCandidate, query):
		return 3, true
	default:
		return 0, false
	}
}
