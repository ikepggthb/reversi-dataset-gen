// Package runui renders interactive progress for dataset generation.
package runui

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

type Event struct {
	Stage      string
	Title      string
	Mode       string
	Generation string
	Strategy   string
	Phase      int
	NoEmoji    bool
	NoColor    bool
	Input      string
	Output     string
	Detail     string
	Workers    int
	Window     WindowEvent

	Done         int64
	Total        int64
	OverallDone  int64
	OverallTotal int64
	PhaseDone    int64
	PhaseTotal   int64
	PhaseIndex   int
	PhaseCount   int
	Attempts     int64
	Duplicates   int64
	Failures     int64
	Timeouts     int64
	Errors       int64
	Train        int64
	Valid        int64
	Test         int64
}

type WindowEvent struct {
	Enabled            bool            `json:"enabled"`
	Anchor             int             `json:"anchor"`
	Start              int             `json:"start"`
	End                int             `json:"end"`
	Index              int             `json:"index"`
	Count              int             `json:"count"`
	Done               int64           `json:"done"`
	Total              int64           `json:"total"`
	Trajectories       int64           `json:"trajectories"`
	InFlight           int64           `json:"in_flight"`
	ActiveWorkers      int64           `json:"active_workers"`
	WorkerCapacity     int64           `json:"worker_capacity"`
	ExtractedSamples   int64           `json:"extracted_samples"`
	DuplicateSamples   int64           `json:"duplicate_samples"`
	FailedTrajectories int64           `json:"failed_trajectories"`
	Timeouts           int64           `json:"timeouts"`
	Errors             int64           `json:"errors"`
	Phases             []PhaseProgress `json:"phases"`
}

type PhaseProgress struct {
	Phase int   `json:"phase"`
	Done  int64 `json:"done"`
	Total int64 `json:"total"`
}

type State struct {
	mu            sync.Mutex
	startedAt     time.Time
	startTimeText string
	last          Event
	seen          bool
}

func NewState() *State {
	now := time.Now()
	return &State{
		startedAt:     now,
		startTimeText: now.Format("2006-01-02 15:04:05 -0700"),
	}
}

func (s *State) Apply(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.Generation == "window" || e.Window.Enabled {
		s.applyWindowLocked(e)
		s.seen = true
		return
	}
	s.applyPhaseLocked(e)
	s.seen = true
}

func (s *State) applyPhaseLocked(e Event) {
	newPhase := false
	if e.Phase != 0 && s.last.Phase != 0 && e.Phase != s.last.Phase {
		newPhase = true
	}
	if e.PhaseIndex != 0 && s.last.PhaseIndex != 0 && e.PhaseIndex != s.last.PhaseIndex {
		newPhase = true
	}
	if newPhase {
		s.last.PhaseDone = 0
		s.last.PhaseTotal = 0
		s.last.Attempts = 0
		s.last.Duplicates = 0
		s.last.Failures = 0
		s.last.Timeouts = 0
		s.last.Errors = 0
		s.last.Train = 0
		s.last.Valid = 0
		s.last.Test = 0
	}
	if e.Stage != "" {
		s.last.Stage = e.Stage
	}
	if e.Title != "" {
		s.last.Title = e.Title
	}
	if e.Mode != "" {
		s.last.Mode = e.Mode
	}
	if e.Phase != 0 {
		s.last.Phase = e.Phase
	}
	if e.NoEmoji {
		s.last.NoEmoji = true
	}
	if e.NoColor {
		s.last.NoColor = true
	}
	if e.Input != "" {
		s.last.Input = e.Input
	}
	if e.Output != "" {
		s.last.Output = e.Output
	}
	if e.Detail != "" {
		s.last.Detail = e.Detail
	}
	if e.Workers != 0 {
		s.last.Workers = e.Workers
	}
	if e.Total != 0 {
		s.last.Total = e.Total
	}
	if e.OverallTotal != 0 {
		s.last.OverallTotal = e.OverallTotal
	}
	if e.PhaseTotal != 0 {
		s.last.PhaseTotal = e.PhaseTotal
	}
	if e.PhaseIndex != 0 {
		s.last.PhaseIndex = e.PhaseIndex
	}
	if e.PhaseCount != 0 {
		s.last.PhaseCount = e.PhaseCount
	}
	s.last.Done = max64(s.last.Done, e.Done)
	s.last.OverallDone = max64(s.last.OverallDone, e.OverallDone)
	s.last.PhaseDone = max64(s.last.PhaseDone, e.PhaseDone)
	s.last.Attempts = max64(s.last.Attempts, e.Attempts)
	s.last.Duplicates = max64(s.last.Duplicates, e.Duplicates)
	s.last.Failures = max64(s.last.Failures, e.Failures)
	s.last.Timeouts = max64(s.last.Timeouts, e.Timeouts)
	s.last.Errors = max64(s.last.Errors, e.Errors)
	s.last.Train = max64(s.last.Train, e.Train)
	s.last.Valid = max64(s.last.Valid, e.Valid)
	s.last.Test = max64(s.last.Test, e.Test)
}

func (s *State) applyWindowLocked(e Event) {
	if e.Stage != "" {
		s.last.Stage = e.Stage
	}
	if e.Title != "" {
		s.last.Title = e.Title
	}
	if e.Mode != "" {
		s.last.Mode = e.Mode
	}
	if e.Strategy != "" {
		s.last.Strategy = e.Strategy
	}
	s.last.Generation = "window"
	if e.NoEmoji {
		s.last.NoEmoji = true
	}
	if e.NoColor {
		s.last.NoColor = true
	}
	if e.Input != "" {
		s.last.Input = e.Input
	}
	if e.Output != "" {
		s.last.Output = e.Output
	}
	if e.Detail != "" {
		s.last.Detail = e.Detail
	}
	if e.Workers != 0 {
		s.last.Workers = e.Workers
	}
	if e.OverallTotal != 0 {
		s.last.OverallTotal = e.OverallTotal
		s.last.Total = e.OverallTotal
	}
	s.last.OverallDone = e.OverallDone
	s.last.Done = e.OverallDone
	if e.Window.Enabled {
		s.last.Window = e.Window
	}
}

func (s *State) snapshot() (Event, time.Time, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last, s.startedAt, s.startTimeText, s.seen
}

func Loop(ctx context.Context, state *State, events <-chan Event, interval time.Duration, w io.Writer) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if w == nil {
		w = os.Stderr
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	renderer := newRenderer(w)
	defer renderer.Close()
	last := ""
	render := func(force bool) {
		event, startedAt, startText, ok := state.snapshot()
		if !ok {
			return
		}
		out := Format(event, startedAt, startText)
		if !force && out == last {
			return
		}
		last = out
		renderer.Render(out)
	}
	for {
		select {
		case <-ctx.Done():
			render(true)
			return
		case event, ok := <-events:
			if !ok {
				render(true)
				return
			}
			state.Apply(event)
		case <-tick.C:
			render(false)
		}
	}
}

func LineLoop(ctx context.Context, events <-chan Event, interval time.Duration, w io.Writer) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if w == nil {
		w = os.Stderr
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	state := NewState()
	printLine := func(force bool) {
		event, startedAt, _, ok := state.snapshot()
		if !ok {
			return
		}
		if !force && event.Done <= 0 {
			return
		}
		fmt.Fprintln(w, FormatLine(event, time.Since(startedAt)))
	}
	for {
		select {
		case <-ctx.Done():
			printLine(true)
			return
		case event, ok := <-events:
			if !ok {
				printLine(true)
				return
			}
			state.Apply(event)
		case <-tick.C:
			printLine(false)
		}
	}
}

func FormatLine(e Event, elapsed time.Duration) string {
	if e.Stage == "fatal" {
		return fmt.Sprintf("[%s] fatal: %s", fmtDur(elapsed), e.Detail)
	}
	if e.Generation == "window" || e.Window.Enabled {
		od, ot := progressValues(e)
		w := e.Window
		strategy := e.Strategy
		if strategy == "" {
			strategy = "active_window"
		}
		return fmt.Sprintf("[%s] mode=%s generation=window strategy=%s active_window=%d/%d anchor=%d covers=%02d..%02d window_samples=%s/%s overall=%s/%s trajectories=%s active_workers=%s/%d in_flight=%s extracted=%s duplicate=%s failures=%s speed=%s",
			fmtDur(elapsed),
			e.Mode,
			strategy,
			w.Index,
			w.Count,
			w.Anchor,
			w.Start,
			w.End,
			fmtCount64(w.Done),
			fmtCount64(w.Total),
			fmtCount64(od),
			fmtCount64(ot),
			fmtCount64(w.Trajectories),
			fmtCount64(w.ActiveWorkers),
			e.Workers,
			fmtCount64(w.InFlight),
			fmtCount64(w.ExtractedSamples),
			fmtCount64(w.DuplicateSamples),
			fmtCount64(w.FailedTrajectories),
			speed(od, elapsed, "samples"),
		)
	}
	od, ot := progressValues(e)
	pd, pt := phaseProgressValues(e)
	return fmt.Sprintf("[%s] mode=%s phase=%02d/%02d phase_samples=%s/%s overall=%s/%s speed=%s duplicates=%s failures=%s timeouts=%s split=train:%s,valid:%s,test:%s",
		fmtDur(elapsed),
		e.Mode,
		e.PhaseIndex,
		e.PhaseCount,
		fmtCount64(pd),
		fmtCount64(pt),
		fmtCount64(od),
		fmtCount64(ot),
		speed(od, elapsed, "samples"),
		fmtCount64(e.Duplicates),
		fmtCount64(e.Failures),
		fmtCount64(e.Timeouts),
		fmtCount64(e.Train),
		fmtCount64(e.Valid),
		fmtCount64(e.Test),
	)
}

type renderer struct {
	w        io.Writer
	tty      bool
	lines    int
	rendered bool
}

func newRenderer(w io.Writer) *renderer {
	return &renderer{w: w, tty: isTerminal(w)}
}

func (r *renderer) Render(out string) {
	if !r.tty {
		fmt.Fprintln(r.w, out)
		return
	}
	if r.rendered {
		for i := 0; i < r.lines; i++ {
			fmt.Fprint(r.w, "\x1b[1A\x1b[2K\r")
		}
	} else {
		fmt.Fprint(r.w, "\x1b[?25l")
	}
	fmt.Fprintln(r.w, out)
	r.lines = strings.Count(out, "\n") + 1
	r.rendered = true
}

func (r *renderer) Close() {
	if r.tty && r.rendered {
		fmt.Fprint(r.w, "\x1b[?25h")
	}
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

func Format(e Event, startedAt time.Time, startText string) string {
	if e.Stage == "fatal" {
		title := e.Title
		if title == "" {
			title = "rdg fatal"
		}
		lines := []string{
			fmt.Sprintf("Started  %s", startText),
			fmt.Sprintf("Updated  %s", time.Now().Format("2006-01-02 15:04:05 -0700")),
			"",
			"Fatal",
			"  " + e.Detail,
		}
		return frame(title, lines, e.NoColor || os.Getenv("NO_COLOR") != "", "\x1b[1;31m")
	}
	if e.Generation == "window" || e.Window.Enabled {
		return formatWindow(e, startedAt, startText)
	}
	now := time.Now()
	elapsed := now.Sub(startedAt)
	title := e.Title
	if title == "" {
		title = e.Stage
	}
	if title == "" {
		title = "rdg"
	}
	lines := []string{
		fmt.Sprintf("Started  %s", startText),
		fmt.Sprintf("Updated  %s", now.Format("2006-01-02 15:04:05 -0700")),
	}
	if e.Mode != "" || e.Phase != 0 {
		marker := "▶️ "
		if e.NoEmoji {
			marker = ""
		}
		lines = append(lines, fmt.Sprintf("Mode     %s%s   phase %02d   %d/%d", marker, e.Mode, e.Phase, e.PhaseIndex, e.PhaseCount))
	}
	od, ot := progressValues(e)
	lines = append(lines, "", "Overall")
	if ot > 0 {
		lines = append(lines,
			fmt.Sprintf("  %s", bar(od, ot, 48)),
			fmt.Sprintf("  progress %s / %s", fmtCount64(od), fmtCount64(ot)),
		)
	} else {
		lines = append(lines, fmt.Sprintf("  processed %s", fmtCount64(od)))
	}
	lines = append(lines,
		fmt.Sprintf("  speed    %s", speed(od, elapsed, "samples")),
		fmt.Sprintf("  time     elapsed %s   eta %s", fmtDur(elapsed), eta(od, ot, elapsed)),
		fmt.Sprintf("  finish   %s", finishText(od, ot, elapsed, now)),
	)
	pd, pt := phaseProgressValues(e)
	lines = append(lines,
		"",
		"Phase",
		fmt.Sprintf("  %s", bar(pd, pt, 48)),
		fmt.Sprintf("  progress %s / %s", fmtCount64(pd), fmtCount64(pt)),
		fmt.Sprintf("  attempts   %s", fmtCount64(e.Attempts)),
		fmt.Sprintf("  duplicate  %s   rate %s", fmtCount64(e.Duplicates), percent(e.Duplicates, max64(e.Attempts, 1))),
		fmt.Sprintf("  failures   %s   timeout %s   errors %s", fmtCount64(e.Failures), fmtCount64(e.Timeouts), fmtCount64(e.Errors)),
		fmt.Sprintf("  workers    %d", e.Workers),
		fmt.Sprintf("  train      %s", fmtCount64(e.Train)),
		fmt.Sprintf("  valid      %s", fmtCount64(e.Valid)),
		fmt.Sprintf("  test       %s", fmtCount64(e.Test)),
	)
	if e.Detail != "" {
		lines = append(lines, "", "Detail", "  "+e.Detail)
	}
	if e.Input != "" || e.Output != "" {
		lines = append(lines, "", "Paths")
		if e.Input != "" {
			lines = append(lines, "  input   "+e.Input)
		}
		if e.Output != "" {
			lines = append(lines, "  output  "+e.Output)
		}
	}
	return frame(title, lines, e.NoColor || os.Getenv("NO_COLOR") != "", "\x1b[1;36m")
}

func formatWindow(e Event, startedAt time.Time, startText string) string {
	now := time.Now()
	elapsed := now.Sub(startedAt)
	title := e.Title
	if title == "" {
		title = e.Stage
	}
	if title == "" {
		title = "rdg"
	}
	marker := "▶️ "
	if e.NoEmoji {
		marker = ""
	}
	w := e.Window
	lines := []string{
		fmt.Sprintf("Started  %s", startText),
		fmt.Sprintf("Updated  %s", now.Format("2006-01-02 15:04:05 -0700")),
		fmt.Sprintf("Mode     %s%s   generation window   strategy %s", marker, e.Mode, windowStrategy(e)),
	}
	if e.Output != "" {
		lines = append(lines, fmt.Sprintf("Output   %s", e.Output))
	}
	od, ot := progressValues(e)
	lines = append(lines,
		"",
		"Overall",
		fmt.Sprintf("  %s", bar(od, ot, 48)),
		fmt.Sprintf("  samples  %s / %s", fmtCount64(od), fmtCount64(ot)),
		fmt.Sprintf("  speed    %s", speed(od, elapsed, "samples")),
		fmt.Sprintf("  elapsed  %s", fmtDur(elapsed)),
		fmt.Sprintf("  eta      %s", eta(od, ot, elapsed)),
		fmt.Sprintf("  finish   %s", finishText(od, ot, elapsed, now)),
		"",
		"Active Window",
		fmt.Sprintf("  index    %d / %d", w.Index, w.Count),
		fmt.Sprintf("  anchor   %d", w.Anchor),
		fmt.Sprintf("  covers   %02d..%02d", w.Start, w.End),
		fmt.Sprintf("  samples  %s / %s", fmtCount64(w.Done), fmtCount64(w.Total)),
		fmt.Sprintf("  %s", bar(w.Done, w.Total, 48)),
		"",
		"Trajectory",
		fmt.Sprintf("  self-play games  %s", fmtCount64(w.Trajectories)),
		fmt.Sprintf("  active workers   %s / %d", fmtCount64(w.ActiveWorkers), e.Workers),
		fmt.Sprintf("  in-flight        %s", fmtCount64(w.InFlight)),
		fmt.Sprintf("  extracted        %s", fmtCount64(w.ExtractedSamples)),
		fmt.Sprintf("  duplicate        %s", fmtCount64(w.DuplicateSamples)),
		fmt.Sprintf("  failures         %s", fmtCount64(w.FailedTrajectories)),
		fmt.Sprintf("  timeout/errors   %s / %s", fmtCount64(w.Timeouts), fmtCount64(w.Errors)),
		"",
		"Phases",
	)
	for _, p := range visiblePhases(w.Phases) {
		lines = append(lines, fmt.Sprintf("  %02d  %s / %s  %s", p.Phase, fmtCount64(p.Done), fmtCount64(p.Total), bar(p.Done, p.Total, 28)))
	}
	return frame(title, lines, e.NoColor || os.Getenv("NO_COLOR") != "", "\x1b[1;36m")
}

func visiblePhases(phases []PhaseProgress) []PhaseProgress {
	if len(phases) <= 12 {
		return phases
	}
	out := make([]PhaseProgress, 0, 11)
	out = append(out, phases[:5]...)
	worst := phases[5]
	for _, p := range phases[5 : len(phases)-5] {
		if p.Total <= 0 {
			continue
		}
		if worst.Total <= 0 || float64(p.Done)/float64(p.Total) < float64(worst.Done)/float64(worst.Total) {
			worst = p
		}
	}
	out = append(out, worst)
	out = append(out, phases[len(phases)-5:]...)
	return out
}

func windowStrategy(e Event) string {
	if e.Strategy != "" {
		return e.Strategy
	}
	return "active_window"
}

func frame(title string, lines []string, noColor bool, color string) string {
	const width = 78
	var sb strings.Builder
	titleWidth := displayWidth(title)
	header := title
	if !noColor {
		header = color + title + "\x1b[0m"
	}
	fmt.Fprintf(&sb, "╭─ %s %s╮\n", header, strings.Repeat("─", max(0, width-titleWidth-5)))
	for _, line := range lines {
		line = truncateDisplay(line, width-4)
		padding := width - 4 - displayWidth(line)
		if padding < 0 {
			padding = 0
		}
		fmt.Fprintf(&sb, "│ %s%s │\n", line, strings.Repeat(" ", padding))
	}
	fmt.Fprintf(&sb, "╰%s╯", strings.Repeat("─", width-2))
	return sb.String()
}

func truncateDisplay(s string, maxWidth int) string {
	if displayWidth(s) <= maxWidth {
		return s
	}
	const suffix = "..."
	limit := maxWidth - displayWidth(suffix)
	if limit <= 0 {
		return suffix[:max(0, maxWidth)]
	}
	var b strings.Builder
	width := 0
	for _, r := range s {
		w := runeDisplayWidth(r)
		if width+w > limit {
			break
		}
		b.WriteRune(r)
		width += w
	}
	b.WriteString(suffix)
	return b.String()
}

func displayWidth(s string) int {
	width := 0
	inEscape := false
	for _, r := range s {
		if inEscape {
			if r >= '@' && r <= '~' {
				inEscape = false
			}
			continue
		}
		if r == '\x1b' {
			inEscape = true
			continue
		}
		width += runeDisplayWidth(r)
	}
	return width
}

func runeDisplayWidth(r rune) int {
	if r == 0 || r < 32 || (r >= 0x7f && r < 0xa0) {
		return 0
	}
	if r >= 0x300 && r <= 0x36f {
		return 0
	}
	if r == 0xfe0f {
		return 0
	}
	if isWideRune(r) {
		return 2
	}
	return 1
}

func isWideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115f:
		return true
	case r >= 0x2329 && r <= 0x232a:
		return true
	case r >= 0x2600 && r <= 0x27bf:
		return true
	case r >= 0x2e80 && r <= 0xa4cf:
		return true
	case r >= 0xac00 && r <= 0xd7a3:
		return true
	case r >= 0xf900 && r <= 0xfaff:
		return true
	case r >= 0xfe10 && r <= 0xfe19:
		return true
	case r >= 0xfe30 && r <= 0xfe6f:
		return true
	case r >= 0xff00 && r <= 0xff60:
		return true
	case r >= 0xffe0 && r <= 0xffe6:
		return true
	case r >= 0x1f000 && r <= 0x1faff:
		return true
	default:
		return false
	}
}

func bar(done, total int64, width int) string {
	if total <= 0 {
		return strings.Repeat("░", width) + " 100%"
	}
	pct := float64(done) / float64(total)
	if pct < 0 {
		pct = 0
	}
	if pct > 1 {
		pct = 1
	}
	filled := int(float64(width) * pct)
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + fmt.Sprintf(" %5.1f%%", pct*100)
}

func progressValues(e Event) (done, total int64) {
	done, total = e.OverallDone, e.OverallTotal
	if done == 0 && e.Done != 0 {
		done = e.Done
	}
	if total == 0 && e.Total != 0 {
		total = e.Total
	}
	return done, total
}

func phaseProgressValues(e Event) (done, total int64) {
	done, total = e.PhaseDone, e.PhaseTotal
	if done == 0 && e.Done != 0 {
		done = e.Done
	}
	if total == 0 && e.Total != 0 {
		total = e.Total
	}
	return done, total
}

func speed(n int64, elapsed time.Duration, unit string) string {
	if n <= 0 || elapsed <= 0 {
		return "unknown"
	}
	return fmt.Sprintf("%.1f %s/sec", float64(n)/elapsed.Seconds(), unit)
}

func eta(done, total int64, elapsed time.Duration) string {
	if done <= 0 || total <= 0 || done >= total || elapsed <= 0 {
		return "0s"
	}
	remaining := total - done
	avg := elapsed / time.Duration(done)
	return fmtDur(avg * time.Duration(remaining))
}

func finishText(done, total int64, elapsed time.Duration, now time.Time) string {
	if done <= 0 || total <= 0 || done >= total || elapsed <= 0 {
		return "unknown"
	}
	remaining := total - done
	avg := elapsed / time.Duration(done)
	finish := now.Add(avg * time.Duration(remaining))
	days := int(finish.Sub(dayStart(now)) / (24 * time.Hour))
	label := "today"
	if days == 1 {
		label = "tomorrow"
	} else if days > 1 {
		label = fmt.Sprintf("in %dd", days)
	}
	return fmt.Sprintf("%s (%s)", finish.Format("2006-01-02 15:04:05 MST"), label)
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func percent(n, d int64) string {
	if d <= 0 {
		return "0.0%"
	}
	return fmt.Sprintf("%.1f%%", float64(n)*100/float64(d))
}

func fmtCount64(n int64) string {
	s := fmt.Sprintf("%d", n)
	out := make([]byte, 0, len(s)+len(s)/3)
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

func fmtDur(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	d = d.Round(time.Second)
	days := int64(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	h := int64(d / time.Hour)
	d -= time.Duration(h) * time.Hour
	m := int64(d / time.Minute)
	d -= time.Duration(m) * time.Minute
	s := int64(d / time.Second)
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh %dm", days, h, m)
	case h > 0:
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	case m > 0:
		return fmt.Sprintf("%dm %ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
