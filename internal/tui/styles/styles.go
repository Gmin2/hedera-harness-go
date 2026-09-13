package styles

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Styles is every style the ui uses, grouped by area. Build it with Default.
type Styles struct {
	Background color.Color

	Base   lipgloss.Style
	Muted  lipgloss.Style
	Subtle lipgloss.Style

	Logo struct {
		Field    lipgloss.Style
		Meta     lipgloss.Style
		Version  lipgloss.Style
		GradFrom color.Color
		GradTo   color.Color
	}

	Header struct {
		Diagonals lipgloss.Style
		Detail    lipgloss.Style
		Keystroke lipgloss.Style
		Tip       lipgloss.Style
		Separator lipgloss.Style
	}

	Section struct {
		Title lipgloss.Style
		Line  lipgloss.Style
	}

	Sidebar struct {
		Title lipgloss.Style
		Info  lipgloss.Style
	}

	Details lipgloss.Style

	Pill lipgloss.Style

	Run struct {
		Command lipgloss.Style

		IconPending lipgloss.Style
		IconSuccess lipgloss.Style
		IconError   lipgloss.Style
		IconIdle    lipgloss.Style

		Name    lipgloss.Style
		Params  lipgloss.Style
		Elapsed lipgloss.Style
		Waiting lipgloss.Style

		BodyLine  lipgloss.Style
		BodyKey   lipgloss.Style
		BodyValue lipgloss.Style
		BodyOK    lipgloss.Style
		BodyBad   lipgloss.Style
		BodyNote  lipgloss.Style
		Link      lipgloss.Style

		PassTag    lipgloss.Style
		FailTag    lipgloss.Style
		SkipTag    lipgloss.Style
		PendingTag lipgloss.Style

		ErrorTag     lipgloss.Style
		WarnTag      lipgloss.Style
		InfoTag      lipgloss.Style
		TagMessage   lipgloss.Style
		FooterText   lipgloss.Style
		FooterFailed lipgloss.Style
	}

	Editor struct {
		PromptFocused lipgloss.Style
		PromptBlurred lipgloss.Style
		Text          lipgloss.Style
		TextBlurred   lipgloss.Style
		Placeholder   lipgloss.Style
	}

	Help struct {
		Key       lipgloss.Style
		Desc      lipgloss.Style
		Separator lipgloss.Style
		View      lipgloss.Style
	}

	Status struct {
		SuccessTag lipgloss.Style
		WarnTag    lipgloss.Style
		ErrorTag   lipgloss.Style
		SuccessMsg lipgloss.Style
		WarnMsg    lipgloss.Style
		ErrorMsg   lipgloss.Style
	}

	Dialog struct {
		View         lipgloss.Style
		Title        lipgloss.Style
		TitleFrom    color.Color
		TitleTo      color.Color
		Input        lipgloss.Style
		InputPrompt  lipgloss.Style
		Placeholder  lipgloss.Style
		Item         lipgloss.Style
		SelectedItem lipgloss.Style
		Info         lipgloss.Style
		InfoSelected lipgloss.Style
		Empty        lipgloss.Style
		Help         lipgloss.Style
		Text         lipgloss.Style
		Hint         lipgloss.Style
		Frame        lipgloss.Style
		WarnView     lipgloss.Style
		WarnTitle    lipgloss.Style
	}

	Button struct {
		Focused lipgloss.Style
		Blurred lipgloss.Style
	}

	Scrollbar struct {
		Thumb lipgloss.Style
		Track lipgloss.Style
	}
}

// Default builds the dark charmtone based theme.
func Default() *Styles {
	s := new(Styles)
	s.Background = BgBase

	base := lipgloss.NewStyle().Foreground(FgBase)
	muted := lipgloss.NewStyle().Foreground(FgMoreSubtle)
	subtle := lipgloss.NewStyle().Foreground(FgMostSubtle)
	s.Base, s.Muted, s.Subtle = base, muted, subtle

	s.Logo.Field = lipgloss.NewStyle().Foreground(Primary)
	s.Logo.Meta = lipgloss.NewStyle().Foreground(Secondary)
	s.Logo.Version = lipgloss.NewStyle().Foreground(Primary)
	s.Logo.GradFrom = Secondary
	s.Logo.GradTo = Primary

	s.Header.Diagonals = lipgloss.NewStyle().Foreground(Primary)
	s.Header.Detail = muted
	s.Header.Keystroke = muted
	s.Header.Tip = subtle
	s.Header.Separator = subtle

	s.Section.Title = subtle
	s.Section.Line = lipgloss.NewStyle().Foreground(Separator)

	s.Sidebar.Title = lipgloss.NewStyle().Foreground(FgSubtle)
	s.Sidebar.Info = muted

	s.Details = base.Padding(0, 1, 1, 1).Border(lipgloss.RoundedBorder()).BorderForeground(Primary)

	s.Pill = lipgloss.NewStyle().Padding(0, 1).Foreground(BgBase).Bold(true)

	r := &s.Run
	r.Command = base.PaddingLeft(1).BorderStyle(lipgloss.NormalBorder()).BorderLeft(true).BorderForeground(Primary)
	r.IconPending = lipgloss.NewStyle().Foreground(SuccessMostSubtle)
	r.IconSuccess = lipgloss.NewStyle().Foreground(Success)
	r.IconError = lipgloss.NewStyle().Foreground(Error)
	r.IconIdle = lipgloss.NewStyle().Foreground(BgMostVisible)
	r.Name = lipgloss.NewStyle().Foreground(Info)
	r.Params = subtle
	r.Elapsed = subtle
	r.Waiting = subtle
	r.BodyLine = muted.Background(BgLeastVisible)
	r.BodyKey = subtle.Background(BgLeastVisible)
	r.BodyValue = lipgloss.NewStyle().Foreground(FgSubtle).Background(BgLeastVisible)
	r.BodyOK = lipgloss.NewStyle().Foreground(SuccessMostSubtle).Background(BgLeastVisible)
	r.BodyBad = lipgloss.NewStyle().Foreground(Destructive).Background(BgLeastVisible)
	r.BodyNote = subtle.Background(BgLeastVisible)
	r.Link = lipgloss.NewStyle().Foreground(Link).Background(BgLeastVisible).Underline(true)

	tag := lipgloss.NewStyle().Padding(0, 1).Bold(true)
	r.PassTag = tag.Background(Success).Foreground(BgLessVisible).SetString("PASS")
	r.FailTag = tag.Background(Destructive).Foreground(OnPrimary).SetString("FAIL")
	r.SkipTag = tag.Background(Attention).Foreground(BgBase).SetString("SKIP")
	r.PendingTag = tag.Background(BgLessVisible).Foreground(FgMoreSubtle).SetString("····")
	r.ErrorTag = lipgloss.NewStyle().Padding(0, 1).Background(Destructive).Foreground(OnPrimary).SetString("ERROR")
	r.WarnTag = tag.Background(Attention).Foreground(BgBase).SetString("WARN")
	r.InfoTag = lipgloss.NewStyle().Padding(0, 1).Background(BgLessVisible).Foreground(FgMoreSubtle).SetString("INFO")
	r.TagMessage = lipgloss.NewStyle().Foreground(FgSubtle)
	r.FooterText = muted
	r.FooterFailed = lipgloss.NewStyle().Foreground(Destructive)

	s.Editor.PromptFocused = lipgloss.NewStyle().Foreground(SuccessMostSubtle)
	s.Editor.PromptBlurred = muted
	s.Editor.Text = base
	s.Editor.TextBlurred = muted
	s.Editor.Placeholder = subtle

	s.Help.Key = muted
	s.Help.Desc = subtle
	s.Help.Separator = lipgloss.NewStyle().Foreground(Separator)
	s.Help.View = lipgloss.NewStyle().Padding(0, 1)

	st := &s.Status
	st.SuccessTag = lipgloss.NewStyle().Padding(0, 1).Bold(true).Foreground(BgLessVisible).Background(Success).SetString("OKAY!")
	st.WarnTag = st.SuccessTag.Foreground(BgMostVisible).Background(Warning).SetString("WARNING")
	st.ErrorTag = st.SuccessTag.Foreground(BgBase).Background(Destructive).SetString("ERROR")
	st.SuccessMsg = lipgloss.NewStyle().Padding(0, 1).Foreground(BgLessVisible).Background(SuccessMostSubtle)
	st.WarnMsg = st.SuccessMsg.Foreground(BgMostVisible).Background(WarningSubtle)
	st.ErrorMsg = st.SuccessMsg.Foreground(OnPrimary).Background(Error)

	d := &s.Dialog
	d.View = base.Border(lipgloss.RoundedBorder()).BorderForeground(Primary)
	d.Title = base.Padding(0, 1).Foreground(Primary)
	d.TitleFrom = Primary
	d.TitleTo = Secondary
	d.Input = lipgloss.NewStyle().Padding(1, 1)
	d.InputPrompt = lipgloss.NewStyle().Foreground(Accent)
	d.Placeholder = subtle
	d.Item = base.Padding(0, 1)
	d.SelectedItem = lipgloss.NewStyle().Padding(0, 1).Background(Primary).Foreground(OnPrimary)
	d.Info = subtle
	d.InfoSelected = lipgloss.NewStyle().Foreground(OnPrimary)
	d.Empty = subtle.Padding(0, 1)
	d.Help = lipgloss.NewStyle().Padding(0, 1)
	d.Text = base
	d.Hint = subtle
	d.Frame = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(Primary).Padding(1, 2)
	d.WarnView = d.Frame.BorderForeground(Attention)
	d.WarnTitle = lipgloss.NewStyle().Foreground(Attention).Bold(true)

	s.Button.Focused = lipgloss.NewStyle().Padding(0, 3).Foreground(OnPrimary).Background(Secondary)
	s.Button.Blurred = lipgloss.NewStyle().Padding(0, 3).Foreground(FgBase).Background(BgLessVisible)

	s.Scrollbar.Thumb = lipgloss.NewStyle().Foreground(Secondary)
	s.Scrollbar.Track = lipgloss.NewStyle().Foreground(Separator)

	return s
}

// Rule renders "Title ─────" filling width.
func (s *Styles) Rule(title string, width int) string {
	rest := width - lipgloss.Width(title) - 1
	if rest <= 0 {
		return s.Section.Title.Render(title)
	}
	return s.Section.Title.Render(title) + " " + s.Section.Line.Render(strings.Repeat(RuleChar, rest))
}

// NetworkPill renders a network name as a small colored tag.
func (s *Styles) NetworkPill(network string) string {
	return s.Pill.Background(NetworkColor(network)).Render(network)
}

// Icon returns the colored status glyph for a step or stage.
func (s *Styles) Icon(ok, failed, running bool) string {
	switch {
	case failed:
		return s.Run.IconError.Render(IconError)
	case ok:
		return s.Run.IconSuccess.Render(IconSuccess)
	case running:
		return s.Run.IconPending.Render(IconPending)
	}
	return s.Run.IconIdle.Render(IconIdle)
}
