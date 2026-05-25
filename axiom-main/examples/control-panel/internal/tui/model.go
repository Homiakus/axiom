package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/filepicker"
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type tab int

const (
	tabFiles tab = iota
	tabValidation
	tabSummary
	tabRuntime
	tabHelp
)

var tabTitles = []string{"Файлы", "Валидация", "Сводка", "Runtime", "Помощь"}

type validationFinishedMsg struct {
	Report ValidationReport
}

type Model struct {
	startDir string

	activeTab tab
	width     int
	height    int

	picker       filepicker.Model
	spinner      spinner.Model
	progress     progress.Model
	help         help.Model
	diagTable    table.Model
	detailView   viewport.Model
	runtimeView  viewport.Model
	runtimeForm  runtimeForm
	runtime      *RuntimeSession
	report       *ValidationReport
	selectedPath string
	validating   bool
	notice       string
}

func Run(startDir string) error {
	if startDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		startDir = wd
	}
	_, err := tea.NewProgram(InitialModel(startDir)).Run()
	return err
}

func InitialModel(startDir string) Model {
	if startDir == "" {
		startDir = "."
	}
	picker := filepicker.New()
	picker.CurrentDirectory = startDir
	picker.AllowedTypes = []string{".axm"}
	picker.ShowHidden = true
	picker.AutoHeight = false
	picker.SetHeight(18)

	spin := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(accentColor)),
	)
	prog := progress.New()
	prog.SetWidth(48)

	form := newRuntimeForm()
	model := Model{
		startDir:    startDir,
		activeTab:   tabFiles,
		width:       100,
		height:      32,
		picker:      picker,
		spinner:     spin,
		progress:    prog,
		help:        help.New(),
		diagTable:   newDiagnosticsTable(96, 10),
		detailView:  viewport.New(viewport.WithWidth(96), viewport.WithHeight(18)),
		runtimeView: viewport.New(viewport.WithWidth(96), viewport.WithHeight(10)),
		runtimeForm: form,
	}
	model.runtimeForm.focus(runtimeFocusExecution)
	return model
}

func (m Model) Init() tea.Cmd {
	return m.picker.Init()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(msg.Width, 72)
		m.height = max(msg.Height, 24)
		m.resize()
	case validationFinishedMsg:
		m.validating = false
		report := msg.Report
		m.report = &report
		m.diagTable = diagnosticsTable(report, m.contentWidth(), max(5, m.height-16))
		m.detailView.SetContent(m.validationDetails())
		if report.OK() {
			m.runtime = NewRuntimeSession(report.Module)
			m.runtimeForm.configure(m.runtime)
			m.runtimeForm.focus(runtimeFocusExecution)
			m.runtimeView.SetContent("Runtime-песочница готова.")
			m.activeTab = tabSummary
			m.notice = "Валидация завершена."
		} else {
			m.runtime = nil
			m.activeTab = tabValidation
			m.notice = "Валидация не прошла."
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab":
			m.activeTab = m.nextTab(1)
			return m, nil
		case "shift+tab", "backtab":
			m.activeTab = m.nextTab(-1)
			return m, nil
		case "esc":
			if m.activeTab != tabFiles {
				m.activeTab = tabFiles
				return m, nil
			}
		case "r":
			if m.selectedPath != "" && m.activeTab != tabRuntime {
				return m.startValidation(m.selectedPath)
			}
		}
	}

	switch m.activeTab {
	case tabFiles:
		next, cmd := m.picker.Update(msg)
		m.picker = next
		cmds = append(cmds, cmd)
		if did, path := m.picker.DidSelectFile(msg); did {
			return m.startValidation(path)
		}
		if did, path := m.picker.DidSelectDisabledFile(msg); did {
			m.notice = "Можно выбрать только .axm файл: " + path
		}
	case tabValidation:
		next, cmd := m.diagTable.Update(msg)
		m.diagTable = next
		cmds = append(cmds, cmd)
		nextView, cmd := m.detailView.Update(msg)
		m.detailView = nextView
		cmds = append(cmds, cmd)
	case tabSummary:
		nextView, cmd := m.detailView.Update(msg)
		m.detailView = nextView
		cmds = append(cmds, cmd)
	case tabRuntime:
		var cmd tea.Cmd
		m, cmd = m.updateRuntime(msg)
		cmds = append(cmds, cmd)
	}

	if m.validating {
		next, cmd := m.spinner.Update(msg)
		m.spinner = next
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m Model) View() tea.View {
	content := baseStyle.Width(m.width).MaxWidth(m.width).Render(m.render())
	view := tea.NewView(content)
	view.AltScreen = true
	return view
}

func (m Model) startValidation(path string) (tea.Model, tea.Cmd) {
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	m.selectedPath = path
	m.validating = true
	m.report = nil
	m.runtime = nil
	m.notice = "Проверяю " + path
	m.activeTab = tabValidation
	m.detailView.SetContent("")
	m.runtimeView.SetContent("")
	return m, tea.Batch(
		func() tea.Msg { return m.spinner.Tick() },
		func() tea.Msg { return validationFinishedMsg{Report: ValidateFile(path)} },
	)
}

func (m Model) nextTab(delta int) tab {
	n := len(tabTitles)
	next := (int(m.activeTab) + delta + n) % n
	return tab(next)
}

func (m *Model) resize() {
	bodyWidth := m.contentWidth()
	bodyHeight := max(8, m.height-12)
	m.help.SetWidth(bodyWidth)
	m.progress.SetWidth(min(56, max(24, bodyWidth-8)))
	m.picker.SetHeight(max(8, bodyHeight-2))
	m.diagTable.SetWidth(bodyWidth)
	m.diagTable.SetHeight(max(5, bodyHeight-8))
	m.detailView.SetWidth(bodyWidth)
	m.detailView.SetHeight(max(6, bodyHeight-4))
	m.runtimeView.SetWidth(bodyWidth)
	m.runtimeView.SetHeight(max(6, bodyHeight-19))
	m.runtimeForm.resize(bodyWidth)
}

func (m Model) contentWidth() int {
	return max(40, m.width-4)
}

func (m Model) render() string {
	parts := []string{
		headerStyle.Render("Axiom TUI Validator"),
		m.renderTabs(),
	}
	if m.notice != "" {
		parts = append(parts, subtleStyle.Render(m.notice))
	}
	parts = append(parts, m.renderBody(), m.renderHelpLine())
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m Model) renderTabs() string {
	var labels []string
	for i, title := range tabTitles {
		style := inactiveTabStyle
		if tab(i) == m.activeTab {
			style = activeTabStyle
		}
		if tab(i) == tabRuntime && (m.report == nil || !m.report.OK()) {
			style = disabledTabStyle
		}
		labels = append(labels, style.Render(title))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, labels...)
}

func (m Model) renderBody() string {
	switch m.activeTab {
	case tabFiles:
		return m.renderFiles()
	case tabValidation:
		return m.renderValidation()
	case tabSummary:
		return m.renderSummary()
	case tabRuntime:
		return m.renderRuntime()
	case tabHelp:
		return m.renderHelp()
	default:
		return ""
	}
}

func (m Model) renderFiles() string {
	selected := "Файл не выбран."
	if m.selectedPath != "" {
		selected = m.selectedPath
	}
	return lipgloss.JoinVertical(
		lipgloss.Left,
		sectionTitleStyle.Render("Выбор .axm файла"),
		subtleStyle.Render("Текущий: "+selected),
		m.picker.View(),
	)
}

func (m Model) renderValidation() string {
	if m.validating {
		return lipgloss.JoinVertical(
			lipgloss.Left,
			sectionTitleStyle.Render("Валидация"),
			m.spinner.View()+" "+subtleStyle.Render(m.selectedPath),
			m.progress.ViewAs(0.66),
			renderStages([]ValidationStage{
				{Name: "чтение файла", Status: StagePending},
				{Name: "parse / compile / validate", Status: StagePending},
				{Name: "сборка модуля", Status: StagePending},
			}),
		)
	}
	if m.report == nil {
		return subtleStyle.Render("Выберите .axm файл, чтобы запустить валидацию.")
	}
	blocks := []string{
		sectionTitleStyle.Render("Валидация"),
		m.reportMeta(),
		renderStages(m.report.Stages),
	}
	if len(m.report.Diagnostics) > 0 {
		blocks = append(blocks, sectionTitleStyle.Render("Diagnostics"), m.diagTable.View())
	}
	if m.report.RawError != "" {
		blocks = append(blocks, errorStyle.Render(m.report.RawError))
	}
	return lipgloss.JoinVertical(lipgloss.Left, blocks...)
}

func (m Model) renderSummary() string {
	if m.report == nil || !m.report.OK() {
		return subtleStyle.Render("Сводка модуля доступна после успешной валидации.")
	}
	m.detailView.SetContent(m.summaryDetails())
	return lipgloss.JoinVertical(
		lipgloss.Left,
		sectionTitleStyle.Render("Сводка модуля"),
		m.detailView.View(),
	)
}

func (m Model) renderRuntime() string {
	if m.report == nil || !m.report.OK() || m.runtime == nil {
		return subtleStyle.Render("Runtime доступен после успешной валидации.")
	}
	return lipgloss.JoinVertical(
		lipgloss.Left,
		sectionTitleStyle.Render("Runtime-песочница"),
		m.runtimeForm.render(m.runtime),
		sectionTitleStyle.Render("Вывод"),
		m.runtimeView.View(),
	)
}

func (m Model) renderHelp() string {
	lines := []string{
		sectionTitleStyle.Render("Помощь"),
		"tab / shift+tab  переключить экран",
		"enter            выбрать файл или запустить выделенное runtime-действие",
		"esc              вернуться к выбору файла",
		"r                повторить валидацию выбранного файла",
		"ctrl+n/ctrl+p    переместить фокус в Runtime",
		"q                выйти",
		"",
		"JSON-поля Runtime принимают объекты. Пустые initial context и signal payload трактуются как nil.",
		"Activities без зарегистрированных handlers остаются pending и видны в pendingActivities.",
	}
	return helpTextStyle.Render(strings.Join(lines, "\n"))
}

func (m Model) renderHelpLine() string {
	return m.help.ShortHelpView([]key.Binding{
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "экраны")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "выбор/запуск")),
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "повтор")),
		key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "выход")),
	})
}

func (m Model) reportMeta() string {
	if m.report == nil {
		return ""
	}
	status := okStyle.Render("OK")
	if !m.report.OK() {
		status = errorStyle.Render("FAILED")
	}
	rows := []string{
		"status:   " + status,
		"path:     " + m.report.Path,
		"size:     " + formatBytes(m.report.Size),
		"duration: " + m.report.Duration.Round(time.Millisecond).String(),
	}
	return strings.Join(rows, "\n")
}

func (m Model) validationDetails() string {
	if m.report == nil {
		return ""
	}
	blocks := []string{m.reportMeta(), "", renderStagesPlain(m.report.Stages)}
	if len(m.report.Diagnostics) > 0 {
		blocks = append(blocks, "", "Diagnostics:")
		for _, diagnostic := range m.report.Diagnostics {
			blocks = append(blocks, diagnostic.Code+" "+diagnostic.Message)
		}
	}
	if m.report.RawError != "" {
		blocks = append(blocks, "", "Ошибка:", m.report.RawError)
	}
	return strings.Join(blocks, "\n")
}

func (m Model) summaryDetails() string {
	if m.report == nil || m.report.Module == nil {
		return ""
	}
	s := m.report.Summary
	lines := []string{
		"domain:     " + s.Domain,
		"signals:    " + strconv.Itoa(s.Signals),
		"contexts:   " + strconv.Itoa(s.Contexts),
		"computed:   " + strconv.Itoa(s.Computeds),
		"facts:      " + strconv.Itoa(s.Facts),
		"activities: " + strconv.Itoa(s.Activities),
		"rules:      " + strconv.Itoa(s.Rules),
		"claims:     " + strconv.Itoa(s.Claims),
		"queries:    " + strconv.Itoa(s.Queries),
		"",
		"Signals:    " + strings.Join(sortedKeys(m.report.Module.Signals), ", "),
		"Contexts:   " + strings.Join(sortedKeys(m.report.Module.Contexts), ", "),
		"Activities: " + strings.Join(sortedKeys(m.report.Module.Activities), ", "),
		"Rules:      " + strings.Join(sortedKeys(m.report.Module.Rules), ", "),
		"Queries:    " + strings.Join(sortedKeys(m.report.Module.Queries), ", "),
	}
	return strings.Join(lines, "\n")
}

func (m Model) updateRuntime(msg tea.Msg) (Model, tea.Cmd) {
	var cmds []tea.Cmd
	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		switch keyMsg.String() {
		case "ctrl+n":
			cmds = append(cmds, m.runtimeForm.focusNext())
			return m, tea.Batch(cmds...)
		case "ctrl+p":
			cmds = append(cmds, m.runtimeForm.focusPrev())
			return m, tea.Batch(cmds...)
		case "enter":
			if m.runtimeForm.focusedAction() {
				m.performRuntimeAction()
				return m, nil
			}
		}
	}

	var cmd tea.Cmd
	m.runtimeForm, cmd = m.runtimeForm.update(msg)
	cmds = append(cmds, cmd)
	if m.runtimeForm.focused == runtimeFocusOutput {
		next, cmd := m.runtimeView.Update(msg)
		m.runtimeView = next
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m *Model) performRuntimeAction() {
	if m.runtime == nil {
		return
	}
	ctx := context.Background()
	var (
		result string
		err    error
		label  string
	)
	switch m.runtimeForm.focused {
	case runtimeFocusStart:
		label = "Старт execution"
		result, err = m.runtime.StartExecution(ctx, m.runtimeForm.executionID.Value(), m.runtimeForm.initialJSON.Value())
	case runtimeFocusSignal:
		label = "Отправка signal"
		result, err = m.runtime.SendSignal(ctx, m.runtimeForm.signalName.Value(), m.runtimeForm.signalPayload.Value())
	case runtimeFocusPatch:
		label = "Patch context"
		result, err = m.runtime.PatchContext(ctx, m.runtimeForm.patchJSON.Value())
	case runtimeFocusRunIdle:
		label = "Run until idle"
		result, err = m.runtime.RunUntilIdle(ctx)
	case runtimeFocusQuery:
		label = "Query"
		result, err = m.runtime.Query(ctx, m.runtimeForm.queryName.Value())
	default:
		return
	}
	if err != nil {
		m.runtimeView.SetContent(errorStyle.Render(label + ": " + err.Error()))
		m.notice = label + ": ошибка."
		return
	}
	m.runtimeView.SetContent(result)
	m.notice = label + ": готово."
}

type runtimeFocus int

const (
	runtimeFocusExecution runtimeFocus = iota
	runtimeFocusInitial
	runtimeFocusStart
	runtimeFocusSignalName
	runtimeFocusSignalPayload
	runtimeFocusSignal
	runtimeFocusPatchPayload
	runtimeFocusPatch
	runtimeFocusRunIdle
	runtimeFocusQueryName
	runtimeFocusQuery
	runtimeFocusOutput
	runtimeFocusCount
)

type runtimeForm struct {
	focused       runtimeFocus
	executionID   textinput.Model
	initialJSON   textarea.Model
	signalName    textinput.Model
	signalPayload textarea.Model
	patchJSON     textarea.Model
	queryName     textinput.Model
}

func newRuntimeForm() runtimeForm {
	executionID := textinput.New()
	executionID.Prompt = "executionID "
	executionID.Placeholder = "execution-1"
	executionID.SetValue("execution-1")

	signalName := textinput.New()
	signalName.Prompt = "signal      "
	signalName.Placeholder = "SignalName"

	queryName := textinput.New()
	queryName.Prompt = "query       "
	queryName.SetValue("state")
	queryName.SetSuggestions([]string{"facts", "history", "pendingActivities", "state"})
	queryName.ShowSuggestions = true

	initial := newJSONArea("initial context JSON")
	initial.SetValue("{}")
	payload := newJSONArea("signal payload JSON")
	payload.SetValue("{}")
	patch := newJSONArea("patch JSON")
	patch.SetValue("{}")

	return runtimeForm{
		focused:       runtimeFocusExecution,
		executionID:   executionID,
		initialJSON:   initial,
		signalName:    signalName,
		signalPayload: payload,
		patchJSON:     patch,
		queryName:     queryName,
	}
}

func newJSONArea(placeholder string) textarea.Model {
	model := textarea.New()
	model.Prompt = "  "
	model.Placeholder = placeholder
	model.ShowLineNumbers = false
	model.SetHeight(3)
	model.CharLimit = 20000
	return model
}

func (f *runtimeForm) configure(session *RuntimeSession) {
	signals := session.SignalNames()
	f.signalName.SetSuggestions(signals)
	f.signalName.ShowSuggestions = true
	if f.signalName.Value() == "" && len(signals) > 0 {
		f.signalName.SetValue(signals[0])
	}
	queries := session.QueryNames()
	f.queryName.SetSuggestions(queries)
	if f.queryName.Value() == "" {
		f.queryName.SetValue("state")
	}
}

func (f *runtimeForm) resize(width int) {
	inputWidth := max(24, min(width-16, 72))
	f.executionID.SetWidth(inputWidth)
	f.signalName.SetWidth(inputWidth)
	f.queryName.SetWidth(inputWidth)
	areaWidth := max(24, min(width-4, 96))
	f.initialJSON.SetWidth(areaWidth)
	f.signalPayload.SetWidth(areaWidth)
	f.patchJSON.SetWidth(areaWidth)
}

func (f *runtimeForm) update(msg tea.Msg) (runtimeForm, tea.Cmd) {
	switch f.focused {
	case runtimeFocusExecution:
		next, cmd := f.executionID.Update(msg)
		f.executionID = next
		return *f, cmd
	case runtimeFocusInitial:
		next, cmd := f.initialJSON.Update(msg)
		f.initialJSON = next
		return *f, cmd
	case runtimeFocusSignalName:
		next, cmd := f.signalName.Update(msg)
		f.signalName = next
		return *f, cmd
	case runtimeFocusSignalPayload:
		next, cmd := f.signalPayload.Update(msg)
		f.signalPayload = next
		return *f, cmd
	case runtimeFocusPatchPayload:
		next, cmd := f.patchJSON.Update(msg)
		f.patchJSON = next
		return *f, cmd
	case runtimeFocusQueryName:
		next, cmd := f.queryName.Update(msg)
		f.queryName = next
		return *f, cmd
	default:
		return *f, nil
	}
}

func (f *runtimeForm) focusNext() tea.Cmd {
	return f.focus((f.focused + 1) % runtimeFocusCount)
}

func (f *runtimeForm) focusPrev() tea.Cmd {
	return f.focus((f.focused - 1 + runtimeFocusCount) % runtimeFocusCount)
}

func (f *runtimeForm) focus(next runtimeFocus) tea.Cmd {
	f.executionID.Blur()
	f.initialJSON.Blur()
	f.signalName.Blur()
	f.signalPayload.Blur()
	f.patchJSON.Blur()
	f.queryName.Blur()
	f.focused = next
	switch next {
	case runtimeFocusExecution:
		return f.executionID.Focus()
	case runtimeFocusInitial:
		return f.initialJSON.Focus()
	case runtimeFocusSignalName:
		return f.signalName.Focus()
	case runtimeFocusSignalPayload:
		return f.signalPayload.Focus()
	case runtimeFocusPatchPayload:
		return f.patchJSON.Focus()
	case runtimeFocusQueryName:
		return f.queryName.Focus()
	default:
		return nil
	}
}

func (f runtimeForm) focusedAction() bool {
	switch f.focused {
	case runtimeFocusStart, runtimeFocusSignal, runtimeFocusPatch, runtimeFocusRunIdle, runtimeFocusQuery:
		return true
	default:
		return false
	}
}

func (f runtimeForm) render(session *RuntimeSession) string {
	status := "execution не запущен"
	if session.Started() {
		status = "execution: " + session.ExecutionID()
	}
	parts := []string{
		subtleStyle.Render(status),
		f.focusLabel(runtimeFocusExecution, f.executionID.View()),
		f.focusLabel(runtimeFocusInitial, "initial context\n"+f.initialJSON.View()),
		f.action(runtimeFocusStart, "Старт execution"),
		"",
		f.focusLabel(runtimeFocusSignalName, f.signalName.View()),
		f.focusLabel(runtimeFocusSignalPayload, "signal payload\n"+f.signalPayload.View()),
		f.action(runtimeFocusSignal, "Отправить signal"),
		"",
		f.focusLabel(runtimeFocusPatchPayload, "patch\n"+f.patchJSON.View()),
		f.action(runtimeFocusPatch, "Применить patch") + "  " + f.action(runtimeFocusRunIdle, "Run until idle"),
		"",
		f.focusLabel(runtimeFocusQueryName, f.queryName.View()),
		f.action(runtimeFocusQuery, "Выполнить query"),
	}
	return strings.Join(parts, "\n")
}

func (f runtimeForm) focusLabel(target runtimeFocus, value string) string {
	if f.focused == target {
		return focusStyle.Render(value)
	}
	return value
}

func (f runtimeForm) action(target runtimeFocus, label string) string {
	style := buttonStyle
	if f.focused == target {
		style = activeButtonStyle
	}
	return style.Render(label)
}

func diagnosticsTable(report ValidationReport, width int, height int) table.Model {
	rows := make([]table.Row, 0, len(report.Diagnostics))
	for _, diagnostic := range report.Diagnostics {
		rows = append(rows, table.Row{diagnostic.Code, diagnostic.Message})
	}
	model := newDiagnosticsTable(width, height)
	model.SetRows(rows)
	model.Focus()
	return model
}

func newDiagnosticsTable(width int, height int) table.Model {
	codeWidth := 10
	messageWidth := max(20, width-codeWidth-8)
	styles := table.DefaultStyles()
	styles.Header = styles.Header.Bold(true).Foreground(accentColor)
	styles.Selected = styles.Selected.Foreground(lipgloss.Color("15")).Background(lipgloss.Color("1"))
	return table.New(
		table.WithColumns([]table.Column{
			{Title: "code", Width: codeWidth},
			{Title: "message", Width: messageWidth},
		}),
		table.WithHeight(height),
		table.WithWidth(width),
		table.WithFocused(true),
		table.WithStyles(styles),
	)
}

func renderStages(stages []ValidationStage) string {
	lines := make([]string, 0, len(stages))
	for _, stage := range stages {
		mark := statusMark(stage.Status)
		line := mark + " " + stage.Name
		if stage.Detail != "" {
			line += " - " + stage.Detail
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func renderStagesPlain(stages []ValidationStage) string {
	lines := make([]string, 0, len(stages))
	for _, stage := range stages {
		line := string(stage.Status) + " " + stage.Name
		if stage.Detail != "" {
			line += " - " + stage.Detail
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func statusMark(status StageStatus) string {
	switch status {
	case StageOK:
		return okStyle.Render("[ok]")
	case StageError:
		return errorStyle.Render("[error]")
	case StageSkipped:
		return subtleStyle.Render("[skip]")
	default:
		return subtleStyle.Render("[...]")
	}
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return []string{"-"}
	}
	return keys
}

func formatBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	value := float64(size)
	for _, suffix := range []string{"KB", "MB", "GB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f TB", value/unit)
}

var (
	accentColor = lipgloss.Color("6")

	baseStyle = lipgloss.NewStyle().
			Padding(1, 2)
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("4")).
			Padding(0, 1)
	sectionTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(accentColor).
				MarginTop(1)
	subtleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))
	okStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("2")).
		Bold(true)
	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("1")).
			Bold(true)
	activeTabStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")).
			Background(accentColor).
			Padding(0, 1).
			MarginRight(1)
	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("7")).
				Background(lipgloss.Color("0")).
				Padding(0, 1).
				MarginRight(1)
	disabledTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("8")).
				Padding(0, 1).
				MarginRight(1)
	buttonStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("7")).
			Background(lipgloss.Color("0")).
			Padding(0, 1)
	activeButtonStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("15")).
				Background(accentColor).
				Padding(0, 1)
	focusStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(accentColor).
			PaddingLeft(1)
	helpTextStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("7"))
)
