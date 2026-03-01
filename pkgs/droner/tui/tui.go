package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/internals/cliutil"
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	"github.com/Oudwins/droner/pkgs/droner/internals/timeouts"
	"github.com/Oudwins/droner/pkgs/droner/sdk"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type newSessionModel struct {
	inputs    []textinput.Model
	focus     int
	submitted bool
	cancelled bool
}

func Run(client *sdk.Client) error {
	prompt, id, submitted, err := runNewSessionForm()
	if err != nil {
		return err
	}
	if !submitted {
		return nil
	}
	path, err := cliutil.RepoRootFromCwd()
	if err != nil {
		return err
	}
	if err := cliutil.EnsureDaemonRunning(client); err != nil {
		return err
	}
	request := schemas.SessionCreateRequest{
		Path:      path,
		SessionID: schemas.NewSSessionID(strings.TrimSpace(id)),
	}
	if strings.TrimSpace(prompt) != "" {
		request.AgentConfig = &schemas.SessionAgentConfig{
			Message: &messages.Message{
				Role:  messages.MessageRoleUser,
				Parts: []messages.MessagePart{messages.NewTextPart(prompt)}},
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeouts.SecondShort)
	defer cancel()
	response, err := client.CreateSession(ctx, request)
	if err != nil {
		if errors.Is(err, sdk.ErrAuthRequired) {
			if err := cliutil.RunGitHubAuthFlow(client); err != nil {
				return err
			}
			ctx, retryCancel := context.WithTimeout(context.Background(), timeouts.SecondDefault)
			defer retryCancel()
			response, err = client.CreateSession(ctx, request)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	cliutil.PrintSessionCreated(response)
	return nil
}

func runNewSessionForm() (string, string, bool, error) {
	model := newNewSessionModel()
	program := tea.NewProgram(model)
	result, err := program.Run()
	if err != nil {
		return "", "", false, err
	}
	finalModel, ok := result.(newSessionModel)
	if !ok {
		return "", "", false, nil
	}
	if finalModel.cancelled || !finalModel.submitted {
		return "", "", false, nil
	}
	prompt := strings.TrimSpace(finalModel.inputs[0].Value())
	id := strings.TrimSpace(finalModel.inputs[1].Value())
	return prompt, id, true, nil
}

func newNewSessionModel() newSessionModel {
	prompt := textinput.New()
	prompt.Prompt = "Prompt (optional): "

	id := textinput.New()
	id.Prompt = "ID (optional): "

	inputs := []textinput.Model{prompt, id}
	inputs[0].Focus()
	return newSessionModel{inputs: inputs}
}

func (m newSessionModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m newSessionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "tab":
			return m.moveFocus(1)
		case "shift+tab":
			return m.moveFocus(-1)
		case "enter":
			if m.focus == len(m.inputs)-1 {
				m.submitted = true
				return m, tea.Quit
			}
			return m.moveFocus(1)
		}
	}

	var cmd tea.Cmd
	m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
	return m, cmd
}

func (m newSessionModel) View() string {
	lines := []string{"New session", ""}
	for i, input := range m.inputs {
		marker := " "
		if i == m.focus {
			marker = ">"
		}
		lines = append(lines, fmt.Sprintf("%s %s", marker, input.View()))
	}
	lines = append(lines, "", "Tab: next field  Enter: submit  Ctrl+C: cancel")
	return strings.Join(lines, "\n")
}

func (m newSessionModel) moveFocus(delta int) (tea.Model, tea.Cmd) {
	if len(m.inputs) == 0 {
		return m, nil
	}
	m.inputs[m.focus].Blur()
	count := len(m.inputs)
	m.focus = (m.focus + delta + count) % count
	return m, m.inputs[m.focus].Focus()
}
