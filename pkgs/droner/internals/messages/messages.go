package messages

import (
	"strings"

	z "github.com/Oudwins/zog"
)

type PartType string

const (
	PartTypeText PartType = "text"
)

var MessagePartSchema = z.Struct(z.Shape{
	"Type": z.StringLike[PartType]().OneOf([]PartType{PartTypeText}).Required(),
}).TestFunc(func(valPtr any, ctx z.Ctx) bool {
	m := valPtr.(*MessagePart)
	switch m.Type {
	case "text":
		return m.Text != ""
	default:
		return false
	}
}, z.Message("Invalid message part"))

type MessagePart struct {
	Type PartType `json:"type"`
	Text string   `json:"text,omitempty"`
}

func NewTextPart(text string) MessagePart {
	return MessagePart{
		Type: PartTypeText,
		Text: text,
	}
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

var MessageSchema = z.Struct(z.Shape{
	"ID":    z.String(),
	"Role":  z.StringLike[MessageRole]().OneOf([]MessageRole{MessageRoleUser, MessageRoleAgent}).Default(MessageRoleUser),
	"Parts": z.Slice(MessagePartSchema).Min(1).Required(),
})
