package tui

import (
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
)

type composerPrompt struct {
	message messages.Message
}

func newComposerPrompt() composerPrompt {
	return composerPrompt{
		message: messages.Message{Role: messages.MessageRoleUser},
	}
}

func (p *composerPrompt) SetPlainText(text string) {
	p.message.Role = messages.MessageRoleUser
	if text == "" {
		p.message.Parts = nil
		return
	}
	p.message.Parts = []messages.MessagePart{messages.NewTextPart(text)}
}

func (p composerPrompt) PlainText() string {
	if len(p.message.Parts) == 1 && p.message.Parts[0].Type == messages.PartTypeText {
		return p.message.Parts[0].Text
	}
	return messages.ToRawText(&p.message)
}

func (p composerPrompt) IsEmpty() bool {
	return !messageHasContent(&p.message)
}

func (p composerPrompt) Message() *messages.Message {
	return cloneMessage(&p.message)
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

func cloneMessage(message *messages.Message) *messages.Message {
	if message == nil {
		return nil
	}
	clone := *message
	if len(message.Parts) > 0 {
		clone.Parts = append([]messages.MessagePart(nil), message.Parts...)
	}
	return &clone
}
