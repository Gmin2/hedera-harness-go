// Package anim is the scrambled gradient spinner shown next to running steps.
//
// An Anim never schedules itself. The owner drives every visible spinner
// from one clock by calling Advance each FrameInterval.
package anim

import (
	"hash/fnv"
	"image/color"
	"math/rand/v2"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/lucasb-eyer/go-colorful"
)

const (
	fps          = 20
	maxBirth     = 20 // frames before every column has faded in
	ellipsisRate = 8  // frames per ellipsis step
)

var (
	glyphs   = []rune("0123456789abcdefABCDEF~!@#$%^&*()+=_")
	ellipsis = []string{".", "..", "...", ""}
)

// FrameInterval is how often the owner should call Advance.
func FrameInterval() time.Duration { return time.Second / fps }

type Settings struct {
	Size       int
	Label      string
	From, To   color.Color
	LabelColor color.Color

	// Seed picks the scrambled glyphs so two spinners do not move in
	// lockstep. The same seed always renders the same frames.
	Seed string

	// Static freezes the spinner on a fully drawn first frame with no
	// ellipsis. Used for tests and --no-anim.
	Static bool
}

type Anim struct {
	size   int
	frames [][]string
	dots   []string
	births []int
	label  string
	static bool
	style  lipgloss.Style
	frame  int
	ticks  int
}

func New(s Settings) *Anim {
	if s.Size < 1 {
		s.Size = 10
	}
	h := fnv.New64a()
	h.Write([]byte(s.Seed + "|" + s.Label))
	seed := h.Sum64()
	rng := rand.New(rand.NewPCG(seed, ^seed))

	a := &Anim{
		size:   s.Size,
		static: s.Static,
		style:  lipgloss.NewStyle().Foreground(s.LabelColor),
	}
	if s.Label != "" {
		a.label = a.style.Render(s.Label)
	}

	ramp := blend(s.Size*3, s.From, s.To, s.From, s.To)
	a.frames = make([][]string, s.Size*2)
	for i := range a.frames {
		a.frames[i] = make([]string, s.Size)
		for j := range a.frames[i] {
			c := lipgloss.NewStyle().Foreground(ramp[(i+j)%len(ramp)])
			a.frames[i][j] = c.Render(string(glyphs[rng.IntN(len(glyphs))]))
		}
	}
	a.dots = make([]string, s.Size)
	for j := range a.dots {
		a.dots[j] = lipgloss.NewStyle().Foreground(ramp[j]).Render(".")
	}
	a.births = make([]int, s.Size)
	for j := range a.births {
		a.births[j] = rng.IntN(maxBirth)
	}
	return a
}

// Advance moves the spinner one frame forward.
func (a *Anim) Advance() {
	if a.static {
		return
	}
	a.frame = (a.frame + 1) % len(a.frames)
	a.ticks++
}

func (a *Anim) Render() string {
	var b strings.Builder
	for j := range a.size {
		if !a.static && a.ticks < a.births[j] {
			b.WriteString(a.dots[j])
			continue
		}
		b.WriteString(a.frames[a.frame][j])
	}
	if a.label == "" {
		return b.String()
	}
	b.WriteString(" ")
	b.WriteString(a.label)
	if !a.static && a.ticks >= maxBirth {
		b.WriteString(a.style.Render(ellipsis[(a.ticks/ellipsisRate)%len(ellipsis)]))
	}
	return b.String()
}

// blend builds a ramp of n colors through the given stops, mixed in HCL so
// the midpoints stay saturated.
func blend(n int, stops ...color.Color) []color.Color {
	points := make([]colorful.Color, len(stops))
	for i, c := range stops {
		points[i], _ = colorful.MakeColor(c)
	}
	segments := len(points) - 1
	out := make([]color.Color, 0, n)
	for i := range segments {
		size := n / segments
		if i < n%segments {
			size++
		}
		for j := range size {
			out = append(out, points[i].BlendHcl(points[i+1], float64(j)/float64(size)).Clamped())
		}
	}
	return out
}
