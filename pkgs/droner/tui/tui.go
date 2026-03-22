package tui

import (
	"context"
	"errors"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/internals/cliutil"
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
	"github.com/Oudwins/droner/pkgs/droner/internals/timeouts"
	"github.com/Oudwins/droner/pkgs/droner/sdk"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	defaultPanelWidth     = 72
	maxPanelWidth         = 88
	minPanelWidth         = 44
	minRenderableWidth    = 24
	composerTextareaRows  = 7
	validationEmptyPrompt = "Enter a prompt to create a session."
)

var (
	appBackgroundColor      = lipgloss.Color("#111315")
	panelBackgroundColor    = lipgloss.Color("#1B1F24")
	textareaBackgroundColor = lipgloss.Color("#161A1F")
	borderColor             = lipgloss.Color("#2E3944")
	accentColor             = lipgloss.Color("#6EA8C8")
	textColor               = lipgloss.Color("#E5E7EB")
	mutedTextColor          = lipgloss.Color("#8B97A3")
	errorTextColor          = lipgloss.Color("#D8A36B")

	appStyle   = lipgloss.NewStyle().Background(appBackgroundColor).Foreground(textColor)
	panelStyle = lipgloss.NewStyle().
			Background(panelBackgroundColor).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Padding(1, 2)
	titleStyle = lipgloss.NewStyle().
			Foreground(textColor).
			Bold(true)
	subtitleStyle = lipgloss.NewStyle().
			Foreground(mutedTextColor)
	helpStyle = lipgloss.NewStyle().
			Foreground(mutedTextColor)
	validationStyle = lipgloss.NewStyle().
			Foreground(errorTextColor)
)

type sessionComposerModel struct {
	input             textarea.Model
	width             int
	height            int
	ready             bool
	submitted         bool
	cancelled         bool
	validationMessage string
}

func Run(client *sdk.Client) error {
	prompt, submitted, err := runSessionComposer()
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
	request := buildSessionCreateRequest(path, prompt)
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

func runSessionComposer() (string, bool, error) {
	model := newSessionComposerModel()
	program := tea.NewProgram(model, tea.WithAltScreen())
	result, err := program.Run()
	if err != nil {
		return "", false, err
	}
	finalModel, ok := result.(sessionComposerModel)
	if !ok {
		return "", false, nil
	}
	return extractComposerResult(finalModel)
}

func buildSessionCreateRequest(path string, prompt string) schemas.SessionCreateRequest {
	request := schemas.SessionCreateRequest{Path: path}
	if strings.TrimSpace(prompt) == "" {
		return request
	}
	request.AgentConfig = &schemas.SessionAgentConfig{
		Message: &messages.Message{
			Role:  messages.MessageRoleUser,
			Parts: []messages.MessagePart{messages.NewTextPart(prompt)},
		},
	}
	return request
}

func extractComposerResult(model sessionComposerModel) (string, bool, error) {
	if model.cancelled || !model.submitted {
		return "", false, nil
	}
	return model.input.Value(), true, nil
}

func newSessionComposerModel() sessionComposerModel {
	focusedStyle, blurredStyle := textarea.DefaultStyles()
	focusedStyle.Base = lipgloss.NewStyle().Background(textareaBackgroundColor).Foreground(textColor)
	focusedStyle.Text = lipgloss.NewStyle().Foreground(textColor)
	focusedStyle.Placeholder = lipgloss.NewStyle().Foreground(mutedTextColor)
	focusedStyle.Prompt = lipgloss.NewStyle().Foreground(accentColor)
	focusedStyle.CursorLine = lipgloss.NewStyle().Background(textareaBackgroundColor)
	focusedStyle.EndOfBuffer = lipgloss.NewStyle().Foreground(textareaBackgroundColor)

	blurredStyle = focusedStyle
	blurredStyle.Prompt = lipgloss.NewStyle().Foreground(mutedTextColor)

	input := textarea.New()
	input.Prompt = "› "
	input.Placeholder = "Describe what you want to do..."
	input.ShowLineNumbers = false
	input.CharLimit = 0
	input.EndOfBufferCharacter = ' '
	input.SetHeight(composerTextareaRows)
	input.FocusedStyle = focusedStyle
	input.BlurredStyle = blurredStyle
	input.Focus()

	model := sessionComposerModel{
		input:  input,
		width:  defaultPanelWidth,
		height: composerTextareaRows + 8,
	}
	model.syncLayout(defaultPanelWidth, model.height)
	return model
}

func (m sessionComposerModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m sessionComposerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.ready = true
		m.syncLayout(msg.Width, msg.Height)
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			if strings.TrimSpace(m.input.Value()) == "" {
				m.validationMessage = validationEmptyPrompt
				return m, nil
			}
			m.submitted = true
			return m, tea.Quit
		case "alt+enter", "ctrl+j":
			m.input.InsertString("\n")
			m.validationMessage = ""
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if strings.TrimSpace(m.input.Value()) != "" {
		m.validationMessage = ""
	}
	return m, cmd
}

func (m sessionComposerModel) View() string {
	if !m.ready {
		return appStyle.Width(m.width).Height(m.height).Render("")
	}

	panelWidth := composerPanelWidth(m.width)
	panelInnerWidth := panelWidth - panelStyle.GetHorizontalFrameSize()
	if panelInnerWidth < 1 {
		panelInnerWidth = 1
	}

	titleBlock := lipgloss.JoinVertical(
		lipgloss.Left,
		titleStyle.Width(panelInnerWidth).Render("New Session"),
		subtitleStyle.Width(panelInnerWidth).Render("Describe what you want to do"),
	)

	helpBlock := helpStyle.Width(panelInnerWidth).Render("Enter submit   Alt+Enter newline   Esc cancel")

	sections := []string{titleBlock, m.input.View(), helpBlock}
	if m.validationMessage != "" {
		sections = append(sections, validationStyle.Width(panelInnerWidth).Render(m.validationMessage))
	}

	panel := panelStyle.Width(panelWidth).Render(lipgloss.JoinVertical(lipgloss.Left, sections...))
	canvas := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, panel)
	return appStyle.Width(m.width).Height(m.height).Render(canvas)
}

func (m *sessionComposerModel) syncLayout(width int, height int) {
	if width < 1 {
		width = defaultPanelWidth
	}
	if height < 1 {
		height = composerTextareaRows + 8
	}
	m.width = width
	m.height = height

	panelWidth := composerPanelWidth(width)
	panelInnerWidth := panelWidth - panelStyle.GetHorizontalFrameSize()
	if panelInnerWidth < 12 {
		panelInnerWidth = 12
	}
	m.input.SetWidth(panelInnerWidth)
	m.input.SetHeight(composerTextareaRows)
	if width >= minRenderableWidth {
		m.ready = true
	}
	if !m.input.Focused() {
		m.input.Focus()
	}
}

func composerPanelWidth(totalWidth int) int {
	if totalWidth <= 0 {
		return defaultPanelWidth
	}

	width := totalWidth - 10
	if width > maxPanelWidth {
		width = maxPanelWidth
	}
	if width < minPanelWidth {
		width = totalWidth - 4
	}
	if width < 12 {
		width = totalWidth
	}
	if width > totalWidth {
		width = totalWidth
	}
	if width < 1 {
		return 1
	}
	return width
}
