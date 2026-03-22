package tui

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
)

type fileRefSpan struct {
	Start int
	End   int
	Path  string
}

type composerPrompt struct {
	text  string
	spans []fileRefSpan
}

func newComposerPrompt() composerPrompt {
	return composerPrompt{}
}

func (p *composerPrompt) SetPlainText(text string) {
	p.text = text
	p.spans = nil
}

func (p *composerPrompt) SyncText(text string) {
	p.spans = remapFileRefSpans(p.spans, p.text, text)
	p.text = text
}

func (p *composerPrompt) AddFileRef(start int, end int, path string) {
	if start < 0 || end < start {
		return
	}
	runes := []rune(p.text)
	if end > len(runes) {
		return
	}
	cleanPath := filepath.ToSlash(filepath.Clean(path))
	span := fileRefSpan{Start: start, End: end, Path: cleanPath}
	p.spans = append(nonOverlappingFileRefSpans(p.spans, span), span)
	sort.Slice(p.spans, func(i int, j int) bool {
		return p.spans[i].Start < p.spans[j].Start
	})
}

func (p composerPrompt) PlainText() string {
	return p.text
}

func (p composerPrompt) IsEmpty() bool {
	return !messageHasContent(p.Message())
}

func (p composerPrompt) Message() *messages.Message {
	parts := make([]messages.MessagePart, 0, len(p.spans)*2+1)
	runes := []rune(p.text)
	position := 0
	for _, span := range p.sortedSpans() {
		if span.Start > len(runes) || span.End > len(runes) || span.Start >= span.End {
			continue
		}
		if position < span.Start {
			parts = appendTextPart(parts, string(runes[position:span.Start]))
		}
		parts = append(parts, messages.NewFilePart(span.Path))
		position = span.End
	}
	if position < len(runes) {
		parts = appendTextPart(parts, string(runes[position:]))
	}
	return &messages.Message{Role: messages.MessageRoleUser, Parts: parts}
}

func (p composerPrompt) sortedSpans() []fileRefSpan {
	if len(p.spans) == 0 {
		return nil
	}
	spans := append([]fileRefSpan(nil), p.spans...)
	sort.Slice(spans, func(i int, j int) bool {
		return spans[i].Start < spans[j].Start
	})
	return spans
}

func appendTextPart(parts []messages.MessagePart, text string) []messages.MessagePart {
	if text == "" {
		return parts
	}
	return append(parts, messages.NewTextPart(text))
}

func nonOverlappingFileRefSpans(spans []fileRefSpan, candidate fileRefSpan) []fileRefSpan {
	kept := spans[:0]
	for _, span := range spans {
		if span.End <= candidate.Start || span.Start >= candidate.End {
			kept = append(kept, span)
		}
	}
	return kept
}

func remapFileRefSpans(spans []fileRefSpan, oldText string, newText string) []fileRefSpan {
	if len(spans) == 0 || oldText == newText {
		return append([]fileRefSpan(nil), spans...)
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
	remapped := make([]fileRefSpan, 0, len(spans))
	for _, span := range spans {
		switch {
		case span.End <= commonPrefix:
			remapped = append(remapped, span)
		case span.Start >= oldChangeEnd:
			remapped = append(remapped, fileRefSpan{Start: span.Start + delta, End: span.End + delta, Path: span.Path})
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
