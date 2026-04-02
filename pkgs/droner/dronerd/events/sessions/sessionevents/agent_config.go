package sessionevents

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/internals/backends"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
)

func (s *System) agentConfigFromJSON(harness conf.HarnessID, raw string) (backends.AgentConfig, error) {
	agentConfig, err := s.defaultAgentConfig(harness)
	if err != nil {
		return backends.AgentConfig{}, err
	}
	if strings.TrimSpace(raw) == "" {
		return agentConfig, nil
	}

	var persisted schemas.SessionAgentConfig
	if err := json.Unmarshal([]byte(raw), &persisted); err != nil {
		return backends.AgentConfig{}, err
	}
	if strings.TrimSpace(persisted.Model) != "" {
		agentConfig.Model = persisted.Model
	}
	agentConfig.AgentName = persisted.AgentName
	agentConfig.Message = persisted.Message
	agentConfig.Command = persisted.Command
	if !messageHasContent(agentConfig.Message) {
		agentConfig.Message = nil
	}
	if !commandHasContent(agentConfig.Command) {
		agentConfig.Command = nil
	}
	return agentConfig, nil
}

func (s *System) defaultAgentConfig(harness conf.HarnessID) (backends.AgentConfig, error) {
	switch harness {
	case "", conf.HarnessOpenCode:
		return backends.AgentConfig{
			Harness:  conf.HarnessOpenCode,
			Model:    s.config.Sessions.Harness.Providers.OpenCode.DefaultModel,
			Opencode: s.config.Sessions.Harness.Providers.OpenCode,
		}, nil
	default:
		return backends.AgentConfig{}, fmt.Errorf("unsupported harness %q", harness)
	}
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

func commandHasContent(command *messages.CommandInvocation) bool {
	return command != nil && command.HasContent()
}
