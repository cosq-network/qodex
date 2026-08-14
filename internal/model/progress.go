package model

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// sample records a cumulative byte count at a point in time. Progress keeps a
// rolling window of samples to derive an accurate transfer speed.
type sample struct {
	at    time.Time
	bytes int64
}

// Progress tracks a download and renders a deterministic progress bar showing
// completion percentage, transfer speed, and estimated time to completion.
//
// Use it as a ProgressFunc via its Update method, or drive it directly. The
// zero value is not usable; construct with NewProgress.
type Progress struct {
	mu        sync.Mutex
	total     int64
	lastBytes int64
	speed     float64 // bytes/second over the rolling window
	samples   []sample
	now       func() time.Time
}

// speedWindow is the rolling window (in seconds) used to compute speed/ETA.
const speedWindow = 5 * time.Second

// NewProgress returns a Progress tracker for a download of total bytes. Pass a
// non-positive total when the remote size is unknown; the bar then shows the
// transferred amount instead of a percentage.
func NewProgress(total int64) *Progress {
	return &Progress{total: total, now: time.Now}
}

// Update implements ProgressFunc and must be called with the cumulative number
// of bytes written and the total size. The total is only applied when > 0 so a
// late-discovered remote size can be filled in.
func (p *Progress) Update(downloaded, total int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if total > 0 {
		p.total = total
	}
	p.lastBytes = downloaded
	now := p.now()
	p.samples = append(p.samples, sample{at: now, bytes: downloaded})
	for len(p.samples) > 2 && now.Sub(p.samples[0].at) > speedWindow {
		p.samples = p.samples[1:]
	}
	if n := len(p.samples); n >= 2 {
		first, last := p.samples[0], p.samples[n-1]
		if dt := last.at.Sub(first.at).Seconds(); dt > 0 {
			p.speed = float64(last.bytes-first.bytes) / dt
		} else {
			p.speed = 0
		}
	}
}

// Total returns the tracked total size in bytes (0 if unknown).
func (p *Progress) Total() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.total
}

// Downloaded returns the cumulative bytes transferred so far.
func (p *Progress) Downloaded() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastBytes
}

// Percent returns the completion percentage in [0, 100]; 0 when unknown.
func (p *Progress) Percent() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.total <= 0 {
		return 0
	}
	return float64(p.lastBytes) * 100 / float64(p.total)
}

// Speed returns the transfer rate in bytes/second over the rolling window.
func (p *Progress) Speed() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.speed
}

// ETA returns the estimated time remaining, or 0 when it cannot be computed
// (no speed yet, or the remote size is unknown).
func (p *Progress) ETA() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.speed <= 0 || p.total <= 0 {
		return 0
	}
	remaining := p.total - p.lastBytes
	if remaining <= 0 {
		return 0
	}
	return time.Duration(float64(remaining) / p.speed * float64(time.Second))
}

// humanBytes formats a byte count for display.
func humanBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func formatETA(d time.Duration) string {
	if d <= 0 {
		return "--:--"
	}
	total := int(d.Seconds())
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// View renders the deterministic progress bar as a single clean line without
// any control characters, so it can be embedded in a Bubble Tea view or any
// single-line layout. barCells is the width in cells of the filled bar body
// (the surrounding brackets add two).
//
//	[██████████░░░░░░░░░░]  50.0%  12.3 MB/s  ETA 00:01:02
func (p *Progress) View(barCells int) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if barCells < 1 {
		barCells = 1
	}
	var b strings.Builder
	if p.total > 0 {
		percent := float64(p.lastBytes) * 100 / float64(p.total)
		if percent > 100 {
			percent = 100
		}
		filled := int(percent / 100 * float64(barCells))
		b.WriteByte('[')
		b.WriteString(strings.Repeat("█", filled))
		b.WriteString(strings.Repeat("░", barCells-filled))
		b.WriteString("] ")
		fmt.Fprintf(&b, "%6.1f%%", percent)
	} else {
		b.WriteString(fmt.Sprintf("Downloaded %s", humanBytes(p.lastBytes)))
	}
	speed := p.speed
	var eta time.Duration
	if p.total > 0 && p.speed > 0 {
		remaining := p.total - p.lastBytes
		if remaining > 0 {
			eta = time.Duration(float64(remaining) / p.speed * float64(time.Second))
		}
	}
	fmt.Fprintf(&b, "  %8s/s", humanBytes(int64(speed)))
	fmt.Fprintf(&b, "  ETA %s", formatETA(eta))
	return b.String()
}

// String renders the bar with a default body width of 30 cells.
func (p *Progress) String() string {
	return p.View(30)
}

// WriteCLI redraws the progress bar in place on w using a carriage return and
// line erase, so successive calls update a single terminal line.
func (p *Progress) WriteCLI(w io.Writer, barCells int) {
	fmt.Fprintf(w, "\r\033[K%s", p.View(barCells))
}

// WriteLine writes the progress bar as a plain single line (no carriage
// return). Use this when the output is not a terminal.
func (p *Progress) WriteLine(w io.Writer, barCells int) {
	fmt.Fprintln(w, p.View(barCells))
}
