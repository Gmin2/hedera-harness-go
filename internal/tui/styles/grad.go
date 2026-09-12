package styles

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/rivo/uniseg"
)

// Gradient colors each grapheme of s along a blend from a to b.
func Gradient(base lipgloss.Style, s string, a, b color.Color) string {
	if s == "" {
		return ""
	}
	var clusters []string
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		clusters = append(clusters, g.Str())
	}
	if len(clusters) == 1 {
		return base.Foreground(a).Render(s)
	}

	var out strings.Builder
	for i, c := range lipgloss.Blend1D(len(clusters), a, b) {
		out.WriteString(base.Foreground(c).Render(clusters[i]))
	}
	return out.String()
}

// BoldGradient is Gradient with bold text.
func BoldGradient(base lipgloss.Style, s string, a, b color.Color) string {
	return Gradient(base.Bold(true), s, a, b)
}
