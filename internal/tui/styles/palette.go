// Package styles holds the hh palette, glyphs and lipgloss styles. It only
// depends on lipgloss so the plain printer can share it with the tui.
package styles

import (
	"image/color"

	"github.com/charmbracelet/x/exp/charmtone"
)

// Palette roles. The names describe what a color is for, not what it looks
// like, so a theme swap only touches this file.
var (
	Primary   color.Color = charmtone.Charple
	Secondary color.Color = charmtone.Dolly
	Accent    color.Color = charmtone.Bok

	FgBase       color.Color = charmtone.Sash
	FgSubtle     color.Color = charmtone.Smoke
	FgMoreSubtle color.Color = charmtone.Squid
	FgMostSubtle color.Color = charmtone.Oyster
	OnPrimary    color.Color = charmtone.Butter

	BgBase         color.Color = charmtone.Pepper
	BgLeastVisible color.Color = charmtone.BBQ
	BgLessVisible  color.Color = charmtone.Char
	BgMostVisible  color.Color = charmtone.Iron
	Separator      color.Color = charmtone.Char

	Destructive   color.Color = charmtone.Coral
	Error         color.Color = charmtone.Sriracha
	Warning       color.Color = charmtone.Mustard
	WarningSubtle color.Color = charmtone.Zest
	Attention     color.Color = charmtone.Tang
	Busy          color.Color = charmtone.Citron

	Info           color.Color = charmtone.Malibu
	InfoMoreSubtle color.Color = charmtone.Sardine
	InfoMostSubtle color.Color = charmtone.Damson

	Success           color.Color = charmtone.Julep
	SuccessMoreSubtle color.Color = charmtone.Bok
	SuccessMostSubtle color.Color = charmtone.Guac

	Link color.Color = charmtone.Zinc
)

// NetworkColor is the pill background for a network name.
func NetworkColor(network string) color.Color {
	switch network {
	case "mock":
		return SuccessMostSubtle
	case "local":
		return Info
	case "testnet":
		return Attention
	case "mainnet":
		return Destructive
	}
	return BgMostVisible
}
