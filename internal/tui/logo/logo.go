// Package logo renders the hh wordmark: block letters with a gradient,
// framed by fields of diagonals.
package logo

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Gmin2/hedera-harness-go/internal/tui/styles"
)

const (
	leftField     = 6
	minRightField = 15
)

// Height is how many rows Wide renders.
const Height = 4

// Wide renders the landing header: a diagonal field, the wordmark, and a
// field that steps in by one cell per row. It spells out the full name when
// there is room and falls back to HEDERA otherwise.
func Wide(sty *styles.Styles, version string, width int) string {
	name, meta := "HEDERA HARNESS", "hh"
	if blockWidth(name, 5)+leftField+2+minRightField > width {
		name, meta = "HEDERA", "harness"
	}
	block := render(sty, name, meta, version, 5)
	blockW := lipgloss.Width(block[0])

	rightW := max(minRightField, width-blockW-leftField-2)
	lines := make([]string, len(block))
	for i, b := range block {
		left := sty.Logo.Field.Render(strings.Repeat(styles.Diagonal, leftField))
		right := sty.Logo.Field.Render(strings.Repeat(styles.Diagonal, rightW-i))
		lines[i] = ansi.Truncate(left+" "+b+" "+right, width, "")
	}
	return strings.Join(lines, "\n")
}

// Compact renders the narrow wordmark for the sidebar: two field rows, the
// letters, the meta row and a closing field.
func Compact(sty *styles.Styles, version string, width int) string {
	block := render(sty, "HEDERA", "harness", version, 4)
	field := sty.Logo.Field.Render(strings.Repeat(styles.Diagonal, width))
	lines := []string{field, field}
	for _, b := range block {
		lines = append(lines, ansi.Truncate(b, width, ""))
	}
	lines = append(lines, field)
	return strings.Join(lines, "\n")
}

// Name is the one line wordmark used in the compact header.
func Name(sty *styles.Styles) string {
	return styles.BoldGradient(lipgloss.NewStyle(), "HEDERA HARNESS", sty.Logo.GradFrom, sty.Logo.GradTo)
}

// render returns the three letter rows followed by the meta row, all padded
// to the same width.
func render(sty *styles.Styles, name, meta, version string, letterWidth int) []string {
	rows := word(name, letterWidth)
	w := lipgloss.Width(rows[0])

	out := make([]string, 0, 4)
	for _, r := range rows {
		r += strings.Repeat(" ", max(0, w-lipgloss.Width(r)))
		out = append(out, styles.Gradient(lipgloss.NewStyle(), r, sty.Logo.GradFrom, sty.Logo.GradTo))
	}

	version = ansi.Truncate(version, max(0, w-lipgloss.Width(meta)-1), "…")
	gap := max(1, w-lipgloss.Width(meta)-lipgloss.Width(version))
	out = append(out, sty.Logo.Meta.Render(meta)+strings.Repeat(" ", gap)+sty.Logo.Version.Render(version))
	return out
}

func blockWidth(name string, letterWidth int) int {
	return lipgloss.Width(word(name, letterWidth)[0])
}
