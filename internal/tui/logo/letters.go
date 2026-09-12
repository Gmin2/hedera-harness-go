package logo

import "strings"

// A letter is three rows of block glyphs, w cells wide. Every letter is
// built from a left column, a stretchable middle and a right column so the
// wordmark can be rendered at different sizes.
type letter func(w int) [3]string

func row(left, mid, right string, w int) string {
	return left + strings.Repeat(mid, max(0, w-2)) + right
}

// █   █
// █▀▀▀█
// ▀   ▀
func letterH(w int) [3]string {
	return [3]string{
		row("█", " ", "█", w),
		row("█", "▀", "█", w),
		row("▀", " ", "▀", w),
	}
}

// █▀▀▀▀
// █▀▀▀
// ▀▀▀▀▀
func letterE(w int) [3]string {
	return [3]string{
		row("█", "▀", "▀", w),
		row("█", "▀", " ", w),
		row("▀", "▀", "▀", w),
	}
}

// █▀▀▀▄
// █   █
// ▀▀▀▀
func letterD(w int) [3]string {
	return [3]string{
		row("█", "▀", "▄", w),
		row("█", " ", "█", w),
		row("▀", "▀", " ", w),
	}
}

// █▀▀▀▄
// █▀▀▀▄
// ▀   ▀
func letterR(w int) [3]string {
	return [3]string{
		row("█", "▀", "▄", w),
		row("█", "▀", "▄", w),
		row("▀", " ", "▀", w),
	}
}

// ▄▀▀▀▄
// █▀▀▀█
// ▀   ▀
func letterA(w int) [3]string {
	return [3]string{
		row("▄", "▀", "▄", w),
		row("█", "▀", "█", w),
		row("▀", " ", "▀", w),
	}
}

// █▀▀▀▄
// █   █
// ▀   ▀
func letterN(w int) [3]string {
	return [3]string{
		row("█", "▀", "▄", w),
		row("█", " ", "█", w),
		row("▀", " ", "▀", w),
	}
}

// S has a blank first cell on its middle row, so it is only described:
// a hooked top, a bar sliding right, and a flat base.
func letterS(w int) [3]string {
	return [3]string{
		row("▄", "▀", "▀", w),
		row(" ", "▀", "▄", w),
		row("▀", "▀", " ", w),
	}
}

var letters = map[rune]letter{
	'A': letterA,
	'D': letterD,
	'E': letterE,
	'H': letterH,
	'N': letterN,
	'R': letterR,
	'S': letterS,
}

// word renders text in block letters, each w cells wide with one blank
// column between letters. Spaces become a three cell gap. Letters without a
// letterform are skipped.
func word(text string, w int) [3]string {
	var rows [3]strings.Builder
	first := true
	for _, r := range text {
		if r == ' ' {
			for i := range rows {
				rows[i].WriteString("   ")
			}
			first = true
			continue
		}
		lf, ok := letters[r]
		if !ok {
			continue
		}
		glyph := lf(w)
		for i := range rows {
			if !first {
				rows[i].WriteByte(' ')
			}
			rows[i].WriteString(glyph[i])
		}
		first = false
	}
	return [3]string{rows[0].String(), rows[1].String(), rows[2].String()}
}
