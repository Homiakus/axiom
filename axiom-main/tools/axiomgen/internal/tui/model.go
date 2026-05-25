package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"axiom/tools/axiomgen/internal/generate"
	"charm.land/bubbles/v2/filepicker"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type step int

const (
	stepFile step = iota
	stepConfig
	stepPreview
	stepDone
)

type generatedMsg struct {
	result generate.Result
	err    error
}

type model struct {
	step    step
	width   int
	height  int
	picker  filepicker.Model
	out     textinput.Model
	pkg     textinput.Model
	focus   int
	pkgSet  bool
	file    string
	plan    *generate.Plan
	result  *generate.Result
	errText string
	busy    bool
}

var (
	accent = lipgloss.Color("39")
	muted  = lipgloss.Color("245")
	title  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	hint   = lipgloss.NewStyle().Foreground(muted)
	bad    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	good   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
)

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

func InitialModel(startDir string) model {
	picker := filepicker.New()
	picker.CurrentDirectory = startDir
	picker.AllowedTypes = []string{".axm"}
	picker.ShowHidden = true
	picker.AutoHeight = false
	picker.SetHeight(18)

	out := textinput.New()
	out.Prompt = "out dir  "
	out.Placeholder = "directory with selected .axm"
	out.SetWidth(56)

	pkg := textinput.New()
	pkg.Prompt = "package  "
	pkg.Placeholder = "generated"
	pkg.SetWidth(56)

	return model{
		step:   stepFile,
		width:  96,
		height: 28,
		picker: picker,
		out:    out,
		pkg:    pkg,
	}
}

func (m model) Init() tea.Cmd {
	return m.picker.Init()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(msg.Width, 72)
		m.height = max(msg.Height, 20)
		m.picker.SetHeight(max(8, m.height-10))
		m.out.SetWidth(max(24, min(m.width-20, 72)))
		m.pkg.SetWidth(max(24, min(m.width-20, 72)))
	case generatedMsg:
		m.busy = false
		if msg.err != nil {
			m.errText = msg.err.Error()
			return m, nil
		}
		m.result = &msg.result
		m.step = stepDone
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "esc":
			return m.back(), nil
		}
	}

	switch m.step {
	case stepFile:
		next, cmd := m.picker.Update(msg)
		m.picker = next
		if did, path := m.picker.DidSelectFile(msg); did {
			m.file = path
			if strings.TrimSpace(m.out.Value()) == "" {
				m.out.SetValue(filepath.Dir(path))
			}
			if !m.pkgSet {
				m.pkg.SetValue(generate.DefaultPackageName(m.out.Value()))
			}
			m.step = stepConfig
			m.focus = 0
			m.errText = ""
			return m, m.focusInput()
		}
		if did, path := m.picker.DidSelectDisabledFile(msg); did {
			m.errText = "Only .axm files can be selected: " + path
		}
		return m, cmd
	case stepConfig:
		return m.updateConfig(msg)
	case stepPreview:
		return m.updatePreview(msg)
	default:
		return m, nil
	}
}

func (m model) updateConfig(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "tab", "down":
			m.focus = (m.focus + 1) % 2
			return m, m.focusInput()
		case "shift+tab", "backtab", "up":
			m.focus = (m.focus + 1) % 2
			return m, m.focusInput()
		case "enter":
			plan, err := generate.Preview(generate.Request{
				File:        m.file,
				OutDir:      strings.TrimSpace(m.out.Value()),
				PackageName: strings.TrimSpace(m.pkg.Value()),
			})
			if err != nil {
				m.errText = err.Error()
				return m, nil
			}
			m.plan = plan
			m.errText = ""
			m.out.Blur()
			m.pkg.Blur()
			m.step = stepPreview
			return m, nil
		}
	}

	var cmd tea.Cmd
	if m.focus == 0 {
		next, c := m.out.Update(msg)
		m.out = next
		if !m.pkgSet {
			m.pkg.SetValue(generate.DefaultPackageName(m.out.Value()))
		}
		cmd = c
	} else {
		if key, ok := msg.(tea.KeyPressMsg); ok && isTextEditKey(key.String()) {
			m.pkgSet = true
		}
		next, c := m.pkg.Update(msg)
		m.pkg = next
		cmd = c
	}
	return m, cmd
}

func (m model) updatePreview(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "enter" && !m.busy {
			m.busy = true
			m.errText = ""
			req := generate.Request{File: m.file, OutDir: m.plan.OutDir, PackageName: m.plan.Package}
			return m, func() tea.Msg {
				result, err := generate.Run(req)
				return generatedMsg{result: result, err: err}
			}
		}
	}
	return m, nil
}

func (m model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	return view
}

func (m model) render() string {
	var b strings.Builder
	fmt.Fprintln(&b, title.Render("Axiom codegen"))
	fmt.Fprintln(&b, hint.Render("q quit | esc back | enter continue"))
	fmt.Fprintln(&b)

	switch m.step {
	case stepFile:
		fmt.Fprintln(&b, "Choose a .axm module")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, m.picker.View())
	case stepConfig:
		fmt.Fprintf(&b, "File: %s\n\n", m.file)
		fmt.Fprintln(&b, m.out.View())
		fmt.Fprintln(&b, m.pkg.View())
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, hint.Render("tab switches fields; enter builds preview"))
	case stepPreview:
		fmt.Fprintln(&b, m.previewView())
	case stepDone:
		fmt.Fprintln(&b, m.doneView())
	}
	if m.errText != "" {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, bad.Render(m.errText))
	}
	return b.String()
}

func (m model) previewView() string {
	if m.plan == nil {
		return "No preview."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Domain:  %s\n", m.plan.Domain)
	fmt.Fprintf(&b, "Package: %s\n", m.plan.Package)
	fmt.Fprintf(&b, "Out:     %s\n", m.plan.OutDir)
	fmt.Fprintf(&b, "Hash:    %s\n\n", m.plan.Hash)
	for _, file := range m.plan.Files {
		fmt.Fprintf(&b, "%-9s %s\n", file.Action, file.Path)
	}
	// Show diff if available.
	if m.plan.Diff != nil && len(m.plan.Diff.Activities) > 0 {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, title.Render("Changes in .axm:"))
		for _, ad := range m.plan.Diff.Activities {
			switch ad.Kind {
			case "added":
				fmt.Fprintf(&b, "  %s activity %s\n", good.Render("+"), ad.Name)
				for _, f := range ad.InputDiffs {
					fmt.Fprintf(&b, "    input.%s: %s\n", f.Name, f.NewType)
				}
				for _, f := range ad.OutputDiffs {
					fmt.Fprintf(&b, "    output.%s: %s\n", f.Name, f.NewType)
				}
			case "removed":
				fmt.Fprintf(&b, "  %s activity %s  %s\n", bad.Render("-"), ad.Name, hint.Render("(method stays in _activities.go)"))
			case "changed":
				fmt.Fprintf(&b, "  %s activity %s\n", hint.Render("~"), ad.Name)
				for _, f := range ad.InputDiffs {
					switch f.Kind {
					case "added":
						fmt.Fprintf(&b, "    %s input.%s: %s\n", good.Render("+"), f.Name, f.NewType)
					case "removed":
						fmt.Fprintf(&b, "    %s input.%s  was %s\n", bad.Render("-"), f.Name, f.OldType)
					case "changed":
						fmt.Fprintf(&b, "    %s input.%s: %s → %s\n", hint.Render("~"), f.Name, f.OldType, f.NewType)
					}
				}
				for _, f := range ad.OutputDiffs {
					switch f.Kind {
					case "added":
						fmt.Fprintf(&b, "    %s output.%s: %s\n", good.Render("+"), f.Name, f.NewType)
					case "removed":
						fmt.Fprintf(&b, "    %s output.%s  was %s\n", bad.Render("-"), f.Name, f.OldType)
					case "changed":
						fmt.Fprintf(&b, "    %s output.%s: %s → %s\n", hint.Render("~"), f.Name, f.OldType, f.NewType)
					}
				}
			}
		}
	}
	fmt.Fprintln(&b)
	if m.busy {
		fmt.Fprintln(&b, "Generating...")
	} else {
		fmt.Fprintln(&b, hint.Render("enter writes files"))
	}
	return b.String()
}

func (m model) doneView() string {
	if m.result == nil {
		return "No result."
	}
	var b strings.Builder
	fmt.Fprintln(&b, good.Render("Generation complete."))
	fmt.Fprintf(&b, "Domain:  %s\n", m.result.Domain)
	fmt.Fprintf(&b, "Package: %s\n", m.result.Package)
	fmt.Fprintf(&b, "Out:     %s\n\n", m.result.OutDir)
	for _, path := range m.result.Written {
		fmt.Fprintf(&b, "written %s\n", path)
	}
	for _, path := range m.result.Skipped {
		fmt.Fprintf(&b, "skipped %s\n", path)
	}
	return b.String()
}

func (m model) back() model {
	m.errText = ""
	switch m.step {
	case stepConfig:
		m.step = stepFile
		m.out.Blur()
		m.pkg.Blur()
	case stepPreview:
		m.step = stepConfig
		_ = m.focusInput()
	case stepDone:
		m.step = stepPreview
	}
	return m
}

func (m *model) focusInput() tea.Cmd {
	m.out.Blur()
	m.pkg.Blur()
	if m.focus == 0 {
		return m.out.Focus()
	}
	return m.pkg.Focus()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func isTextEditKey(key string) bool {
	switch key {
	case "tab", "shift+tab", "backtab", "up", "down", "enter", "esc":
		return false
	default:
		return true
	}
}
