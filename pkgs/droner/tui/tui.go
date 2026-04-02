package tui

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/internals/cliutil"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
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
	maxPanelWidth         = 78
	minPanelWidth         = 44
	minRenderableWidth    = 24
	composerTextareaRows  = 4
	validationEmptyPrompt = "Enter a prompt to create a session."
)

var (
	appBackgroundColor      = lipgloss.Color("#050505")
	panelBackgroundColor    = lipgloss.Color("#1C1C1C")
	sectionBackgroundColor  = lipgloss.Color("#101010")
	textareaBackgroundColor = lipgloss.Color("#1F1F1F")
	borderColor             = lipgloss.Color("#2D2D2D")
	accentColor             = lipgloss.Color("#3B82F6")
	accentStrongColor       = lipgloss.Color("#60A5FA")
	textColor               = lipgloss.Color("#E7E5E4")
	mutedTextColor          = lipgloss.Color("#8A8A8A")
	logoMutedColor          = lipgloss.Color("#7A7A7A")
	warningBackgroundColor  = lipgloss.Color("#231A0B")
	errorTextColor          = lipgloss.Color("#FDBA74")
	tipAccentColor          = lipgloss.Color("#F59E0B")

	appStyle = lipgloss.NewStyle().
			Background(appBackgroundColor).
			Foreground(textColor)
	panelStyle = lipgloss.NewStyle().
			Padding(0, 0)
	repoBadgeStyle = lipgloss.NewStyle().
			Foreground(mutedTextColor)
	brandMutedStyle = lipgloss.NewStyle().
			Foreground(logoMutedColor).
			Bold(true)
	brandStyle = lipgloss.NewStyle().
			Foreground(textColor).
			Bold(true)
	subtitleStyle = lipgloss.NewStyle().
			Foreground(mutedTextColor)
	sectionTitleStyle = lipgloss.NewStyle().
				Foreground(mutedTextColor)
	sectionShellStyle = lipgloss.NewStyle().
				Background(sectionBackgroundColor).
				Padding(0, 1)
	inputCardStyle = lipgloss.NewStyle().
			Padding(0, 0)
	inputShellStyle = lipgloss.NewStyle().
			Background(textareaBackgroundColor).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(accentColor).
			BorderLeft(true).
			BorderTop(false).
			BorderRight(false).
			BorderBottom(false).
			Padding(0, 1, 0, 1)
	inputMetaStyle = lipgloss.NewStyle().
			Foreground(mutedTextColor).
			Padding(0, 1)
	helpStyle = lipgloss.NewStyle().
			Foreground(mutedTextColor)
	validationStyle = lipgloss.NewStyle().
			Foreground(errorTextColor).
			Background(warningBackgroundColor).
			Border(lipgloss.NormalBorder()).
			BorderForeground(errorTextColor).
			Padding(0, 1)
	imageMarkerStyle = lipgloss.NewStyle().
				Foreground(accentColor).
				Bold(true)
	promptGlyphStyle = lipgloss.NewStyle().
				Foreground(accentColor)
	attachmentLabelStyle = lipgloss.NewStyle().
				Foreground(mutedTextColor)
	agentChipStyle = lipgloss.NewStyle().
			Foreground(mutedTextColor)
	activeAgentChipStyle = lipgloss.NewStyle().
				Foreground(accentStrongColor)
	metaLabelStyle = lipgloss.NewStyle().
			Foreground(mutedTextColor)
	selectionStyle = lipgloss.NewStyle().
			Foreground(textColor).
			Background(sectionBackgroundColor).
			Bold(true)
	shortcutKeyStyle = lipgloss.NewStyle().
				Foreground(textColor)
	shortcutLabelStyle = lipgloss.NewStyle().
				Foreground(mutedTextColor)
	tipBulletStyle = lipgloss.NewStyle().
			Foreground(tipAccentColor).
			Bold(true)
	tipLabelStyle = lipgloss.NewStyle().
			Foreground(tipAccentColor)
)

type sessionComposerModel struct {
	input               textarea.Model
	prompt              composerPrompt
	repoRoot            string
	fileCandidates      []string
	slashCommands       []slashCommand
	agentNames          []string
	selectedAgentIndex  int
	autocompleteActive  bool
	autocompleteQuery   autocompleteQuery
	autocompleteResults []autocompleteResult
	autocompleteIndex   int
	width               int
	height              int
	ready               bool
	submitted           bool
	cancelled           bool
	validationMessage   string
	readClipboardImage  clipboardImageReader
	pasteTextCmd        tea.Cmd
	pasteFallbackActive bool
	pasteFallbackValue  string
}

func Run(client *sdk.Client) error {
	path, err := cliutil.RepoRootFromCwd()
	if err != nil {
		return err
	}
	fileCandidates, err := loadRepoFileCandidates(path)
	if err != nil {
		return err
	}
	slashCommands, err := loadGlobalOpencodeSlashCommands()
	if err != nil {
		return err
	}
	agentNames := conf.GetConfig().TUI.AgentNames
	prompt, rawInput, agentName, submitted, err := runSessionComposer(path, fileCandidates, slashCommands, agentNames)
	if err != nil {
		return err
	}
	if !submitted {
		return nil
	}
	if err := cliutil.EnsureDaemonRunning(client); err != nil {
		return err
	}
	request := buildSessionCreateRequest(path, agentName, rawInput, prompt, slashCommands)
	ctx, cancel := context.WithTimeout(context.Background(), timeouts.SecondLong)
	defer cancel()
	response, err := client.CreateSession(ctx, request)
	if err != nil {
		return err
	}
	cliutil.PrintSessionCreated(response)
	return nil
}

func runSessionComposer(repoRoot string, fileCandidates []string, slashCommands []slashCommand, agentNames []string) (*messages.Message, string, string, bool, error) {
	model := newSessionComposerModelWithCommands(repoRoot, fileCandidates, slashCommands, agentNames)
	program := tea.NewProgram(model, tea.WithAltScreen())
	result, err := program.Run()
	if err != nil {
		return nil, "", "", false, err
	}
	finalModel, ok := result.(sessionComposerModel)
	if !ok {
		return nil, "", "", false, nil
	}
	return extractComposerResult(finalModel)
}

func buildSessionCreateRequest(path string, agentName string, rawInput string, prompt *messages.Message, slashCommands []slashCommand) schemas.SessionCreateRequest {
	request := schemas.SessionCreateRequest{Path: path}
	command := commandInvocationFromPrompt(rawInput, prompt, slashCommands)
	if !messageHasContent(prompt) && (command == nil || !command.HasContent()) {
		return request
	}
	config := &schemas.SessionAgentConfig{AgentName: strings.TrimSpace(agentName)}
	if command != nil {
		config.Command = messages.CloneCommand(command)
	} else {
		config.Message = messages.CloneMessage(prompt)
	}
	request.AgentConfig = &schemas.SessionAgentConfig{
		AgentName: config.AgentName,
		Message:   config.Message,
		Command:   config.Command,
	}
	return request
}

func extractComposerResult(model sessionComposerModel) (*messages.Message, string, string, bool, error) {
	if model.cancelled || !model.submitted {
		return nil, "", "", false, nil
	}
	return model.prompt.Message(), model.prompt.PlainText(), model.selectedAgentName(), true, nil
}

func newSessionComposerModel(repoRoot string, fileCandidates []string, agentNames []string) sessionComposerModel {
	return newSessionComposerModelWithCommands(repoRoot, fileCandidates, nil, agentNames)
}

func newSessionComposerModelWithCommands(repoRoot string, fileCandidates []string, slashCommands []slashCommand, agentNames []string) sessionComposerModel {
	focusedStyle, blurredStyle := textarea.DefaultStyles()
	focusedStyle.Base = lipgloss.NewStyle().Foreground(textColor)
	focusedStyle.Text = lipgloss.NewStyle().Foreground(textColor)
	focusedStyle.Placeholder = lipgloss.NewStyle().Foreground(mutedTextColor)
	focusedStyle.Prompt = lipgloss.NewStyle().Foreground(accentColor)
	focusedStyle.CursorLine = lipgloss.NewStyle()
	focusedStyle.EndOfBuffer = lipgloss.NewStyle()

	blurredStyle = focusedStyle
	blurredStyle.Prompt = lipgloss.NewStyle().Foreground(mutedTextColor)

	input := textarea.New()
	input.Prompt = "  "
	input.SetPromptFunc(2, func(lineIdx int) string {
		if lineIdx == 0 {
			return promptGlyphStyle.Render("›") + " "
		}
		return "  "
	})
	input.Placeholder = "Ask anything... \"Fix broken tests\""
	input.ShowLineNumbers = false
	input.CharLimit = 0
	input.EndOfBufferCharacter = ' '
	input.SetHeight(composerTextareaRows)
	input.FocusedStyle = focusedStyle
	input.BlurredStyle = blurredStyle
	input.Focus()

	model := sessionComposerModel{
		input:              input,
		prompt:             newComposerPrompt(),
		repoRoot:           repoRoot,
		fileCandidates:     append([]string(nil), fileCandidates...),
		slashCommands:      append([]slashCommand(nil), slashCommands...),
		agentNames:         append([]string(nil), agentNames...),
		width:              defaultPanelWidth,
		height:             composerTextareaRows + 8,
		readClipboardImage: defaultReadClipboardImage,
		pasteTextCmd:       textarea.Paste,
	}
	model.syncPromptFromInput()
	model.refreshAutocomplete()
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
		m.syncPromptFromInput()
		if m.autocompleteActive {
			switch msg.String() {
			case "esc":
				m.clearAutocomplete()
				return m, nil
			case "up", "ctrl+p":
				m.moveAutocomplete(-1)
				return m, nil
			case "down", "ctrl+n":
				m.moveAutocomplete(1)
				return m, nil
			case "tab", "enter":
				if m.applyAutocompleteSelection() {
					return m, nil
				}
				m.clearAutocomplete()
				return m, nil
			}
		}
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			if m.prompt.IsEmpty() {
				m.validationMessage = validationEmptyPrompt
				return m, nil
			}
			m.submitted = true
			return m, tea.Quit
		case "alt+enter", "ctrl+j":
			m.input.InsertString("\n")
			m.syncPromptFromInput()
			m.validationMessage = ""
			return m, nil
		case "tab":
			m.cycleAgent(1)
			return m, nil
		}
		if msg.Type == tea.KeyCtrlV {
			handled, cmd := m.handleClipboardPaste()
			if handled {
				return m, cmd
			}
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.syncPromptFromInput()
	m.resolvePasteFallback()
	m.refreshAutocomplete()
	if !m.prompt.IsEmpty() {
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

	sections := []string{m.headerView(panelInnerWidth), m.renderInputSection(panelInnerWidth)}
	if attachmentView := m.imageAttachmentView(panelInnerWidth); attachmentView != "" {
		sections = append(sections, attachmentView)
	}
	if autocompleteView := m.autocompleteView(panelInnerWidth); autocompleteView != "" {
		sections = append(sections, autocompleteView)
	}
	if m.validationMessage != "" {
		sections = append(sections, validationStyle.Width(panelInnerWidth).Render(m.validationMessage))
	}
	sections = append(sections, m.helpView(panelInnerWidth), m.tipView(panelInnerWidth))

	panel := panelStyle.Render(lipgloss.JoinVertical(lipgloss.Left, sections...))
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
	m.input.SetWidth(composerInputWidth(panelInnerWidth))
	m.input.SetHeight(composerTextareaRows)
	if width >= minRenderableWidth {
		m.ready = true
	}
	if !m.input.Focused() {
		m.input.Focus()
	}
}

func (m *sessionComposerModel) syncPromptFromInput() {
	m.prompt.SyncText(m.input.Value())
}

func (m *sessionComposerModel) cycleAgent(delta int) {
	if len(m.agentNames) == 0 {
		return
	}
	m.selectedAgentIndex = (m.selectedAgentIndex + delta + len(m.agentNames)) % len(m.agentNames)
}

func (m sessionComposerModel) selectedAgentName() string {
	if len(m.agentNames) == 0 {
		return ""
	}
	if m.selectedAgentIndex < 0 || m.selectedAgentIndex >= len(m.agentNames) {
		return ""
	}
	return m.agentNames[m.selectedAgentIndex]
}

func (m *sessionComposerModel) refreshAutocomplete() {
	query, ok := detectAutocompleteQuery(m.input.Value(), textareaCursorIndex(m.input))
	if !ok {
		m.clearAutocomplete()
		return
	}
	var results []autocompleteResult
	switch query.Mode {
	case autocompleteModeCommand:
		results = rankCommandSearchResults(m.slashCommands, query.Text, maxAutocompleteResults)
	default:
		results = rankFileSearchResults(m.fileCandidates, query.Text, maxAutocompleteResults)
	}
	if !m.autocompleteActive || m.autocompleteQuery.Mode != query.Mode || m.autocompleteQuery.Start != query.Start || m.autocompleteQuery.End != query.End || m.autocompleteQuery.Text != query.Text {
		m.autocompleteIndex = 0
	}
	m.autocompleteActive = true
	m.autocompleteQuery = query
	m.autocompleteResults = results
	if len(results) == 0 {
		m.autocompleteIndex = 0
		return
	}
	if m.autocompleteIndex >= len(results) {
		m.autocompleteIndex = len(results) - 1
	}
}

func (m *sessionComposerModel) clearAutocomplete() {
	m.autocompleteActive = false
	m.autocompleteQuery = autocompleteQuery{}
	m.autocompleteResults = nil
	m.autocompleteIndex = 0
}

func (m *sessionComposerModel) moveAutocomplete(delta int) {
	if len(m.autocompleteResults) == 0 {
		return
	}
	m.autocompleteIndex = (m.autocompleteIndex + delta + len(m.autocompleteResults)) % len(m.autocompleteResults)
}

func (m *sessionComposerModel) applyAutocompleteSelection() bool {
	if !m.autocompleteActive || len(m.autocompleteResults) == 0 {
		return false
	}
	selected := m.autocompleteResults[m.autocompleteIndex]
	replacement := fileRefToken(selected.Value)
	if m.autocompleteQuery.Mode == autocompleteModeCommand {
		replacement = "/" + selected.Value
	}
	for i := 0; i < m.autocompleteQuery.End-m.autocompleteQuery.Start; i++ {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		_ = cmd
	}
	m.input.InsertString(replacement)
	m.syncPromptFromInput()
	if m.autocompleteQuery.Mode == autocompleteModeFile {
		m.prompt.AddFileRef(m.autocompleteQuery.Start, m.autocompleteQuery.Start+len([]rune(replacement)), selected.Value)
	}
	m.validationMessage = ""
	m.clearAutocomplete()
	return true
}

func (m *sessionComposerModel) handleClipboardPaste() (bool, tea.Cmd) {
	if m.readClipboardImage == nil {
		m.startPasteFallback()
		return true, m.pasteTextCmd
	}
	image, ok, err := m.readClipboardImage()
	if err != nil {
		m.validationMessage = err.Error()
		return true, nil
	}
	if !ok {
		m.startPasteFallback()
		return true, m.pasteTextCmd
	}
	m.clearPasteFallback()
	image, err = normalizeClipboardImage(image, m.nextImageIndex())
	if err != nil {
		m.validationMessage = err.Error()
		return true, nil
	}
	label := m.nextImageLabel()
	dataURL := fmt.Sprintf("data:%s;base64,%s", image.Mime, base64.StdEncoding.EncodeToString(image.Bytes))
	part := messages.NewDataURLFilePart(image.Mime, image.Filename, dataURL)
	m.insertStructuredMarker(label, part)
	m.validationMessage = ""
	m.refreshAutocomplete()
	return true, nil
}

func (m *sessionComposerModel) startPasteFallback() {
	m.pasteFallbackActive = true
	m.pasteFallbackValue = m.input.Value()
}

func (m *sessionComposerModel) clearPasteFallback() {
	m.pasteFallbackActive = false
	m.pasteFallbackValue = ""
}

func (m *sessionComposerModel) resolvePasteFallback() {
	if !m.pasteFallbackActive {
		return
	}
	defer m.clearPasteFallback()
	if m.input.Value() == m.pasteFallbackValue {
		m.validationMessage = "No clipboard image was detected."
	}
}

func (m *sessionComposerModel) insertStructuredMarker(display string, part messages.MessagePart) {
	start := textareaCursorIndex(m.input)
	m.input.InsertString(display)
	m.syncPromptFromInput()
	m.prompt.AddStructuredPart(start, start+len([]rune(display)), display, part)
}

func (m sessionComposerModel) headerView(width int) string {
	repoName := filepath.Base(strings.TrimSpace(m.repoRoot))
	if repoName == "" || repoName == "." || repoName == string(filepath.Separator) {
		repoName = "repo"
	}
	brand := lipgloss.JoinHorizontal(
		lipgloss.Left,
		brandMutedStyle.Render("D R O "),
		brandStyle.Render("N E R"),
	)
	meta := subtitleStyle.Render(fmt.Sprintf("new session for %s", repoBadgeStyle.Render(repoName)))
	return lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Bold(true).Render(brand),
		lipgloss.NewStyle().Width(width).Align(lipgloss.Center).MarginBottom(1).Render(meta),
	)
}

func (m sessionComposerModel) agentTabsView() string {
	if len(m.agentNames) == 0 {
		return helpStyle.Render("No agents configured")
	}
	chips := make([]string, 0, len(m.agentNames))
	for i, agentName := range m.agentNames {
		style := agentChipStyle
		if i == m.selectedAgentIndex {
			style = activeAgentChipStyle
		}
		chips = append(chips, style.Render(agentName))
	}
	return strings.Join(chips, "  ")
}

func (m sessionComposerModel) renderInputView() string {
	view := m.input.View()
	for _, token := range m.inlineImageTokens() {
		view = strings.ReplaceAll(view, token.Display, imageMarkerStyle.Render(token.Display))
	}
	return view
}

func (m sessionComposerModel) renderInputSection(width int) string {
	innerWidth := composerInputWidth(width)
	inputBody := lipgloss.NewStyle().
		Width(innerWidth).
		Height(composerTextareaRows).
		Render(m.renderInputView())
	content := inputShellStyle.Width(width).Render(inputBody)
	agentSummary := inputMetaStyle.Width(width).Render(metaLabelStyle.Render(fmt.Sprintf("Agent: %s", m.selectedAgentName())))
	metaLine := lipgloss.JoinHorizontal(lipgloss.Left, m.agentTabsView(), "   ", subtitleStyle.Render("GPT-5.4 OpenAI"))
	meta := inputMetaStyle.Width(width).Render(metaLine)
	return inputCardStyle.Render(lipgloss.JoinVertical(lipgloss.Left, content, agentSummary, meta))
}

func (m sessionComposerModel) imageAttachmentView(width int) string {
	tokens := m.inlineImageTokens()
	if len(tokens) == 0 {
		return ""
	}
	labels := make([]string, 0, len(tokens))
	for _, token := range tokens {
		labels = append(labels, imageMarkerStyle.Render(token.Display))
	}
	body := attachmentLabelStyle.Render("Images:") + " " + strings.Join(labels, "  ")
	return lipgloss.JoinVertical(
		lipgloss.Left,
		sectionTitleStyle.Width(width).Render("Attachments"),
		sectionShellStyle.Width(width).Render(body),
	)
}

func (m sessionComposerModel) inlineImageTokens() []structuredPromptToken {
	tokens := make([]structuredPromptToken, 0)
	for _, token := range m.prompt.sortedTokens() {
		if token.Part.Type != messages.PartTypeFile || token.Part.File == nil || token.Part.File.URL == nil {
			continue
		}
		if strings.TrimSpace(*token.Part.File.URL) == "" {
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens
}

func (m sessionComposerModel) nextImageIndex() int {
	return len(m.inlineImageTokens()) + 1
}

func (m sessionComposerModel) nextImageLabel() string {
	return fmt.Sprintf("[Image %d]", m.nextImageIndex())
}

func (m sessionComposerModel) autocompleteView(width int) string {
	if !m.autocompleteActive {
		return ""
	}
	if len(m.autocompleteResults) == 0 {
		title := "Files"
		empty := "No matching files"
		if m.autocompleteQuery.Mode == autocompleteModeCommand {
			title = "Commands"
			empty = "No matching commands"
		}
		return lipgloss.JoinVertical(
			lipgloss.Left,
			sectionTitleStyle.Width(width).Render(title),
			sectionShellStyle.Width(width).Render(helpStyle.Render(empty)),
		)
	}
	lines := make([]string, 0, len(m.autocompleteResults)+1)
	for i, result := range m.autocompleteResults {
		prefix := "  "
		style := helpStyle
		if i == m.autocompleteIndex {
			prefix = "> "
			style = selectionStyle
		}
		lines = append(lines, style.Width(width).Render(prefix+result.Value))
	}
	title := "Files"
	if m.autocompleteQuery.Mode == autocompleteModeCommand {
		title = "Commands"
	}
	return lipgloss.JoinVertical(
		lipgloss.Left,
		sectionTitleStyle.Width(width).Render(title),
		sectionShellStyle.Width(width).Render(lipgloss.JoinVertical(lipgloss.Left, lines...)),
	)
}

func (m sessionComposerModel) helpView(width int) string {
	items := []string{
		shortcutKeyStyle.Render("Tab") + " " + shortcutLabelStyle.Render("agent"),
		shortcutKeyStyle.Render("/") + " " + shortcutLabelStyle.Render("command"),
		shortcutKeyStyle.Render("@") + " " + shortcutLabelStyle.Render("file ref"),
		shortcutKeyStyle.Render("ctrl+v") + " " + shortcutLabelStyle.Render("paste"),
		shortcutKeyStyle.Render("enter") + " " + shortcutLabelStyle.Render("submit"),
		shortcutKeyStyle.Render("alt+enter") + " " + shortcutLabelStyle.Render("newline"),
		shortcutKeyStyle.Render("esc") + " " + shortcutLabelStyle.Render("cancel"),
	}
	return lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(strings.Join(items, "   "))
}

func (m sessionComposerModel) tipView(width int) string {
	content := tipBulletStyle.Render("•") + " " + tipLabelStyle.Render("Tip") + " " + helpStyle.Render("Press Ctrl+Alt+G or End to jump to the most recent message")
	return lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(content)
}

func composerInputWidth(panelInnerWidth int) int {
	width := panelInnerWidth - inputShellStyle.GetHorizontalFrameSize()
	if width < 12 {
		return 12
	}
	return width
}

func composerPanelWidth(totalWidth int) int {
	if totalWidth <= 0 {
		return defaultPanelWidth
	}

	width := totalWidth - 16
	if width > maxPanelWidth {
		width = maxPanelWidth
	}
	if width < minPanelWidth {
		width = totalWidth - 6
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
