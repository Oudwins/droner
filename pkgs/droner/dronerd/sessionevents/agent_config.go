package sessionevents

import (
	"encoding/json"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/internals/backends"
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
)

func (s *System) agentConfigFromJSON(raw string) (backends.AgentConfig, error) {
	agentConfig := backends.AgentConfig{
		Model:    s.config.Sessions.Agent.DefaultModel,
		Opencode: s.config.Sessions.Agent.Providers.OpenCode,
	}
	if strings.TrimSpace(raw) == "" {
		return agentConfig, nil
	}

	var persisted schemas.SessionAgentConfig
	if err := json.Unmarshal([]byte(raw), &persisted); err != nil {
		return agentConfig, err
	}
	if strings.TrimSpace(persisted.Model) != "" {
		agentConfig.Model = persisted.Model
	}
	agentConfig.AgentName = persisted.AgentName
	agentConfig.Message = persisted.Message
	if !messageHasContent(agentConfig.Message) {
		agentConfig.Message = nil
	}
	return agentConfig, nil
}

func messageHasContent(msg *messages.Message) bool {
	if msg == nil {
		return false
	}
	for _, part := range msg.Parts {
		if strings.TrimSpace(part.Text) != "" {
			return true
		}
		if part.File != nil {
			return true
		}
	}
	return false
}
