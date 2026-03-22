package messages

import (
	"log/slog"
	"mime"
	"path/filepath"
	"strings"

	z "github.com/Oudwins/zog"
)

type PartType string

const (
	PartTypeText PartType = "text"
	PartTypeFile PartType = "file"
)

var MessagePartSchema = z.Struct(z.Shape{
	"Type": z.StringLike[PartType]().OneOf([]PartType{PartTypeText, PartTypeFile}).Required(),
}).TestFunc(func(valPtr any, ctx z.Ctx) bool {
	m := valPtr.(*MessagePart)
	return m.isValid()
}, z.Message("Invalid message part"))

type MessagePart struct {
	Type PartType      `json:"type"`
	Text string        `json:"text,omitempty"`
	File *FilePartData `json:"file,omitempty"`
}

type FilePartData struct {
	URL      *string             `json:"url"`
	Mime     string              `json:"mime,omitempty"`
	Filename string              `json:"filename,omitempty"`
	Source   *FilePartSourceData `json:"source,omitempty"`
}

type FilePartSourceData struct {
	Type string                  `json:"type,omitempty"`
	Path string                  `json:"path,omitempty"`
	Text *FilePartSourceTextData `json:"text,omitempty"`
}

type FilePartSourceTextData struct {
	Start int64  `json:"start"`
	End   int64  `json:"end"`
	Value string `json:"value"`
}

func NewTextPart(text string) MessagePart {
	return MessagePart{
		Type: PartTypeText,
		Text: text,
	}
}

func NewFilePart(path string) MessagePart {
	cleanPath := filepath.Clean(path)
	return MessagePart{
		Type: PartTypeFile,
		File: &FilePartData{
			URL:      nil,
			Mime:     mimeTypeForPath(cleanPath),
			Filename: filepath.Base(cleanPath),
			Source: &FilePartSourceData{
				Type: "file",
				Path: cleanPath,
				Text: &FilePartSourceTextData{},
			},
		},
	}
}

func (p MessagePart) isValid() bool {
	switch p.Type {
	case PartTypeText:
		return p.Text != "" && p.File == nil
	case PartTypeFile:
		return p.Text == "" && p.File != nil && p.File.isValid()
	default:
		return false
	}
}

func (f *FilePartData) isValid() bool {
	if f == nil || f.Source == nil {
		return false
	}
	if strings.TrimSpace(f.Filename) == "" {
		return false
	}
	return f.Source.isValid()
}

func (s *FilePartSourceData) isValid() bool {
	if s == nil || s.Text == nil {
		return false
	}
	if strings.TrimSpace(s.Type) != "file" {
		return false
	}
	return isValidRepoRelativePath(s.Path)
}

func isValidRepoRelativePath(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return false
	}
	cleanPath := filepath.Clean(trimmed)
	if cleanPath == "." || cleanPath == ".." {
		return false
	}
	if filepath.IsAbs(cleanPath) {
		return false
	}
	if cleanPath != trimmed {
		return false
	}
	return !strings.HasPrefix(cleanPath, ".."+string(filepath.Separator))
}

type MessageMeta struct {
}

type MessageRole string

const (
	MessageRoleUser  MessageRole = "user"
	MessageRoleAgent MessageRole = "assistant"
)

type Message struct {
	ID    string
	Role  MessageRole
	Parts []MessagePart `json:"parts"`
	Meta  MessageMeta   // TODO: Actually store some info in here
}

func (m Message) LogValue() slog.Value {
	attrs := []slog.Attr{
		slog.String("id", m.ID),
		slog.String("role", string(m.Role)),
	}

	if len(m.Parts) > 0 {
		attrs = append(attrs, slog.Any("parts", messagePartsForLog(m.Parts)))
	}

	return slog.GroupValue(attrs...)
}

func messagePartsForLog(parts []MessagePart) []map[string]any {
	values := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		item := map[string]any{
			"type": string(part.Type),
		}

		if part.Text != "" {
			item["text"] = part.Text
		}
		if part.File != nil {
			fileItem := map[string]any{}
			if part.File.URL != nil {
				fileItem["url"] = *part.File.URL
			} else {
				fileItem["url"] = nil
			}
			if part.File.Mime != "" {
				fileItem["mime"] = part.File.Mime
			}
			if part.File.Filename != "" {
				fileItem["filename"] = part.File.Filename
			}
			if part.File.Source != nil {
				sourceItem := map[string]any{}
				if part.File.Source.Type != "" {
					sourceItem["type"] = part.File.Source.Type
				}
				if part.File.Source.Path != "" {
					sourceItem["path"] = part.File.Source.Path
				}
				if part.File.Source.Text != nil {
					sourceItem["text"] = map[string]any{
						"start": part.File.Source.Text.Start,
						"end":   part.File.Source.Text.End,
						"value": part.File.Source.Text.Value,
					}
				}
				fileItem["source"] = sourceItem
			}
			item["file"] = fileItem
		}

		values = append(values, item)
	}

	return values
}

func mimeTypeForPath(path string) string {
	if detected := mime.TypeByExtension(filepath.Ext(path)); detected != "" {
		return detected
	}
	return "text/plain"
}

// ToRawText concatenates all text parts and ignores non-text parts.
// Text parts are joined with newlines.
func ToRawText(m *Message) string {
	if m == nil || len(m.Parts) == 0 {
		return ""
	}
	var parts []string
	for _, p := range m.Parts {
		if p.Type != PartTypeText {
			continue
		}
		if strings.TrimSpace(p.Text) == "" {
			continue
		}
		parts = append(parts, p.Text)
	}
	return strings.Join(parts, "\n")
}

func CloneMessage(message *Message) *Message {
	if message == nil {
		return nil
	}
	clone := *message
	if len(message.Parts) > 0 {
		clone.Parts = append([]MessagePart(nil), message.Parts...)
		for i := range clone.Parts {
			clone.Parts[i] = clone.Parts[i].clone()
		}
	}
	return &clone
}

func (p MessagePart) clone() MessagePart {
	clone := p
	if p.File != nil {
		fileClone := *p.File
		if p.File.URL != nil {
			urlClone := *p.File.URL
			fileClone.URL = &urlClone
		}
		if p.File.Source != nil {
			sourceClone := *p.File.Source
			if p.File.Source.Text != nil {
				textClone := *p.File.Source.Text
				sourceClone.Text = &textClone
			}
			fileClone.Source = &sourceClone
		}
		clone.File = &fileClone
	}
	return clone
}

var MessageSchema = z.Struct(z.Shape{
	"ID":    z.String(),
	"Role":  z.StringLike[MessageRole]().OneOf([]MessageRole{MessageRoleUser, MessageRoleAgent}).Default(MessageRoleUser),
	"Parts": z.Slice(MessagePartSchema).Min(1).Required(),
})
