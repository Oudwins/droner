package tui

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
)

type structuredPromptToken struct {
	Start   int
	End     int
	Display string
	Part    messages.MessagePart
}

type composerPrompt struct {
	text   string
	tokens []structuredPromptToken
}

func newComposerPrompt() composerPrompt {
	return composerPrompt{}
}

func (p *composerPrompt) SetPlainText(text string) {
	p.text = text
	p.tokens = nil
}

func (p *composerPrompt) SyncText(text string) {
	p.tokens = remapStructuredPromptTokens(p.tokens, p.text, text)
	p.text = text
}

func (p *composerPrompt) AddFileRef(start int, end int, path string) {
	cleanPath := filepath.ToSlash(filepath.Clean(path))
	p.AddStructuredPart(start, end, fileRefToken(cleanPath), messages.NewFilePart(cleanPath))
}

func (p *composerPrompt) AddStructuredPart(start int, end int, display string, part messages.MessagePart) {
	if start < 0 || end < start {
		return
	}
	runes := []rune(p.text)
	if end > len(runes) {
		return
	}
	if display == "" || string(runes[start:end]) != display {
		return
	}
	token := structuredPromptToken{Start: start, End: end, Display: display, Part: part}
	p.tokens = append(nonOverlappingStructuredPromptTokens(p.tokens, token), token)
	sort.Slice(p.tokens, func(i int, j int) bool {
		return p.tokens[i].Start < p.tokens[j].Start
	})
}

func (p composerPrompt) PlainText() string {
	return p.text
}

func (p composerPrompt) IsEmpty() bool {
	return !messageHasContent(p.Message())
}

func (p composerPrompt) Message() *messages.Message {
	parts := make([]messages.MessagePart, 0, len(p.tokens)*2+1)
	runes := []rune(p.text)
	position := 0
	for _, token := range p.sortedTokens() {
		if !token.matches(runes) {
			continue
		}
		if position < token.Start {
			parts = appendTextPart(parts, string(runes[position:token.Start]))
		}
		parts = append(parts, token.Part)
		position = token.End
	}
	if position < len(runes) {
		parts = appendTextPart(parts, string(runes[position:]))
	}
	return &messages.Message{Role: messages.MessageRoleUser, Parts: parts}
}

func (p composerPrompt) sortedTokens() []structuredPromptToken {
	if len(p.tokens) == 0 {
		return nil
	}
	tokens := append([]structuredPromptToken(nil), p.tokens...)
	sort.Slice(tokens, func(i int, j int) bool {
		return tokens[i].Start < tokens[j].Start
	})
	return tokens
}

func (t structuredPromptToken) matches(runes []rune) bool {
	if t.Start < 0 || t.End <= t.Start || t.End > len(runes) {
		return false
	}
	return string(runes[t.Start:t.End]) == t.Display
}

func appendTextPart(parts []messages.MessagePart, text string) []messages.MessagePart {
	if text == "" {
		return parts
	}
	return append(parts, messages.NewTextPart(text))
}

func nonOverlappingStructuredPromptTokens(tokens []structuredPromptToken, candidate structuredPromptToken) []structuredPromptToken {
	kept := tokens[:0]
	for _, token := range tokens {
		if token.End <= candidate.Start || token.Start >= candidate.End {
			kept = append(kept, token)
		}
	}
	return kept
}

func remapStructuredPromptTokens(tokens []structuredPromptToken, oldText string, newText string) []structuredPromptToken {
	if len(tokens) == 0 || oldText == newText {
		return append([]structuredPromptToken(nil), tokens...)
	}
	oldRunes := []rune(oldText)
	newRunes := []rune(newText)
	commonPrefix := 0
	for commonPrefix < len(oldRunes) && commonPrefix < len(newRunes) && oldRunes[commonPrefix] == newRunes[commonPrefix] {
		commonPrefix++
	}
	commonSuffix := 0
	for commonSuffix < len(oldRunes)-commonPrefix && commonSuffix < len(newRunes)-commonPrefix && oldRunes[len(oldRunes)-1-commonSuffix] == newRunes[len(newRunes)-1-commonSuffix] {
		commonSuffix++
	}
	oldChangeEnd := len(oldRunes) - commonSuffix
	newChangeEnd := len(newRunes) - commonSuffix
	delta := newChangeEnd - oldChangeEnd
	remapped := make([]structuredPromptToken, 0, len(tokens))
	for _, token := range tokens {
		switch {
		case token.End <= commonPrefix:
			remapped = append(remapped, token)
		case token.Start >= oldChangeEnd:
			shifted := token
			shifted.Start += delta
			shifted.End += delta
			remapped = append(remapped, shifted)
		default:
			continue
		}
	}
	return remapped
}

func messageHasContent(message *messages.Message) bool {
	if message == nil || len(message.Parts) == 0 {
		return false
	}
	for _, part := range message.Parts {
		switch part.Type {
		case messages.PartTypeText:
			if strings.TrimSpace(part.Text) != "" {
				return true
			}
		default:
			return true
		}
	}
	return false
}
