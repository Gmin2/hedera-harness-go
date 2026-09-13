package tui

import (
	"image"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/layout"
	"github.com/charmbracelet/ultraviolet/screen"

	"github.com/Gmin2/hedera-harness-go/internal/tui/logo"
)

const (
	compactWidth   = 120
	compactHeight  = 30
	sidebarWidth   = 32
	detailsMaxRows = 20
)

// regions holds the rectangles every region draws into. It is recomputed on
// each update from the window size and state.
type regions struct {
	header  image.Rectangle
	main    image.Rectangle
	sidebar image.Rectangle
	editor  image.Rectangle
	status  image.Rectangle
	details image.Rectangle
}

func (m *Model) generateLayout(w, h int) regions {
	area := image.Rect(0, 0, w, h)

	helpHeight := 1
	if m.help.ShowAll {
		for _, col := range m.FullHelp() {
			helpHeight = max(helpHeight, len(col))
		}
	}
	editorHeight := m.editor.Height() + editorMargin

	// One blank row above the app and between it and the help, one column
	// of margin on both sides, and the last terminal row left empty.
	var app, help image.Rectangle
	layout.Vertical(layout.Len(area.Dy()-helpHeight), layout.Fill(1)).Split(area).Assign(&app, &help)
	app.Min.Y++
	app.Max.Y--
	help.Min.Y--
	app.Min.X++
	app.Max.X--

	l := regions{status: help}

	switch {
	case m.state == stateLanding:
		app.Min.X++
		app.Max.X--
		var header, main, editor image.Rectangle
		layout.Vertical(layout.Len(logo.Height), layout.Fill(1)).Split(app).Assign(&header, &main)
		layout.Vertical(layout.Len(main.Dy()-editorHeight), layout.Fill(1)).Split(main).Assign(&main, &editor)
		editor.Min.X--
		editor.Max.X++
		l.header, l.main, l.editor = header, main, editor

	case m.compact:
		var header, main, editor image.Rectangle
		layout.Vertical(layout.Len(1), layout.Fill(1)).Split(app).Assign(&header, &main)
		main.Min.Y++
		layout.Vertical(layout.Len(main.Dy()-editorHeight), layout.Fill(1)).Split(main).Assign(&main, &editor)
		main.Max.X--
		main.Max.Y--
		l.header, l.main, l.editor = header, main, editor

		details := app
		details.Min.Y = header.Max.Y
		details.Max.Y = min(app.Max.Y, details.Min.Y+detailsMaxRows)
		l.details = details

	default:
		var main, side, editor image.Rectangle
		layout.Horizontal(layout.Len(app.Dx()-sidebarWidth), layout.Fill(1)).Split(app).Assign(&main, &side)
		side.Min.X++
		layout.Vertical(layout.Len(main.Dy()-editorHeight), layout.Fill(1)).Split(main).Assign(&main, &editor)
		main.Max.X--
		main.Max.Y--
		l.main, l.sidebar, l.editor = main, side, editor
	}
	return l
}

// relayout recomputes the layout and pushes the new sizes into components.
// The textarea can change height when its width changes, so it runs twice
// in that case.
func (m *Model) relayout() {
	m.compact = m.width < compactWidth || m.height < compactHeight
	if !m.compact || m.state != stateRun {
		m.detailsOpen = false
	}
	for range 2 {
		height := m.editor.Height()
		m.layout = m.generateLayout(m.width, m.height)
		m.editor.SetWidth(max(1, m.layout.editor.Dx()))
		if m.editor.Height() == height {
			break
		}
	}
	m.view.SetSize(max(0, m.layout.main.Dx()), max(0, m.layout.main.Dy()))
	if a := m.active(); a != nil {
		m.view.Refresh(a.Items())
	}
}

func (m *Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.BackgroundColor = m.sty.Background
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "hh " + prettyPath(m.opts.Cwd)
	if m.width <= 0 || m.height <= 0 {
		return v
	}

	canvas := uv.NewScreenBuffer(m.width, m.height)
	v.Cursor = m.Draw(canvas, canvas.Bounds())

	lines := strings.Split(strings.ReplaceAll(canvas.Render(), "\r\n", "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	v.Content = strings.Join(lines, "\n")
	return v
}

// Draw paints the whole ui into scr and returns where the cursor goes.
func (m *Model) Draw(scr uv.Screen, area uv.Rectangle) *tea.Cursor {
	l := m.layout
	screen.Clear(scr)

	switch m.state {
	case stateLanding:
		uv.NewStyledString(logo.Wide(m.sty, m.opts.Version, l.header.Dx())).Draw(scr, l.header)
		uv.NewStyledString(m.landingView(l.main.Dx(), l.main.Dy())).Draw(scr, l.main)
	case stateRun:
		if m.compact {
			uv.NewStyledString(m.compactHeader(l.header.Dx())).Draw(scr, l.header)
		} else {
			uv.NewStyledString(m.sidebarView(l.sidebar.Dx(), l.sidebar.Dy())).Draw(scr, l.sidebar)
		}
		uv.NewStyledString(m.view.Render()).Draw(scr, l.main)
		if m.focus == focusMain {
			if bar := m.view.Scrollbar(m.sty); bar != "" {
				col := image.Rect(l.main.Max.X, l.main.Min.Y, l.main.Max.X+1, l.main.Max.Y)
				uv.NewStyledString(bar).Draw(scr, col)
			}
		}
	}

	// the rows above and below the textarea hold a thin rule, like the section rules
	rule := m.sty.Section.Line.Render(strings.Repeat("─", max(0, l.editor.Dx())))
	uv.NewStyledString(rule+"\n"+m.editor.View()+"\n"+rule).Draw(scr, l.editor)

	if m.state == stateRun && m.compact && m.detailsOpen {
		uv.NewStyledString(m.detailsView(l.details.Dx(), l.details.Dy())).Draw(scr, l.details)
	}

	m.drawStatus(scr, l.status)

	if m.dialog.HasDialogs() {
		return m.dialog.Draw(scr, area)
	}
	if m.focus != focusEditor || m.detailsOpen {
		return nil
	}
	cur := m.editor.Cursor()
	if cur != nil {
		cur.X += l.editor.Min.X
		cur.Y += l.editor.Min.Y + 1
	}
	return cur
}

func prettyPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
