package model

import (
	"strings"
	"testing"
	"time"
)

func TestProgressPercent(t *testing.T) {
	p := NewProgress(100)
	p.Update(0, 100)
	if got := p.Percent(); got != 0 {
		t.Fatalf("percent = %v, want 0", got)
	}
	p.Update(25, 100)
	if got := p.Percent(); got != 25 {
		t.Fatalf("percent = %v, want 25", got)
	}
	p.Update(100, 100)
	if got := p.Percent(); got != 100 {
		t.Fatalf("percent = %v, want 100", got)
	}
}

func TestProgressSpeedAndETA(t *testing.T) {
	p := NewProgress(100)
	fake := time.Unix(0, 0)
	p.now = func() time.Time { return fake }

	p.Update(0, 100)
	fake = fake.Add(2 * time.Second)
	p.Update(50, 100)

	// 50 bytes over 2s = 25 B/s, 50 remaining => 2s ETA.
	if got := p.Speed(); got < 24.9 || got > 25.1 {
		t.Fatalf("speed = %v, want ~25", got)
	}
	if got := p.ETA(); got != 2*time.Second {
		t.Fatalf("eta = %v, want 2s", got)
	}

	fake = fake.Add(2 * time.Second)
	p.Update(100, 100)
	if got := p.Percent(); got != 100 {
		t.Fatalf("percent = %v, want 100", got)
	}
}

func TestProgressETAUnknownWhenNoTotal(t *testing.T) {
	p := NewProgress(0)
	fake := time.Unix(0, 0)
	p.now = func() time.Time { return fake }
	p.Update(0, 0)
	fake = fake.Add(time.Second)
	p.Update(1024, 0)
	if p.ETA() != 0 {
		t.Fatalf("expected zero ETA for unknown total, got %v", p.ETA())
	}
	if p.Percent() != 0 {
		t.Fatalf("expected zero percent for unknown total, got %v", p.Percent())
	}
}

func TestProgressView(t *testing.T) {
	p := NewProgress(100)
	fake := time.Unix(0, 0)
	p.now = func() time.Time { return fake }
	p.Update(0, 100)
	fake = fake.Add(time.Second)
	p.Update(50, 100)

	view := p.View(20)
	if !strings.Contains(view, "[") || !strings.Contains(view, "]") {
		t.Fatalf("view missing bar brackets: %q", view)
	}
	if !strings.Contains(view, "50.0%") {
		t.Fatalf("view missing percentage: %q", view)
	}
	if !strings.Contains(view, "/s") {
		t.Fatalf("view missing speed: %q", view)
	}
	if !strings.Contains(view, "ETA") {
		t.Fatalf("view missing ETA: %q", view)
	}
	if strings.ContainsAny(view, "\r\n") {
		t.Fatalf("view must not contain control characters: %q", view)
	}
}

func TestProgressViewUnknownTotal(t *testing.T) {
	p := NewProgress(0)
	p.Update(2048, 0)
	view := p.View(20)
	if !strings.Contains(view, "Downloaded") || !strings.Contains(view, "2.0 KB") {
		t.Fatalf("unexpected unknown-total view: %q", view)
	}
}

func TestProgressSpeedWindowRolls(t *testing.T) {
	p := NewProgress(1000)
	fake := time.Unix(0, 0)
	p.now = func() time.Time { return fake }
	for i := 0; i < 50; i++ {
		fake = fake.Add(100 * time.Millisecond)
		p.Update(int64(i+1)*20, 1000)
	}
	// With a 5s window the oldest samples must have been dropped, otherwise a
	// single (0,0) sample would skew the average across 50 samples.
	if n := len(p.samples); n > 60 {
		t.Fatalf("sample buffer not bounded: %d samples retained", n)
	}
	if got := p.Speed(); got <= 0 {
		t.Fatalf("expected a positive rolling speed, got %v", got)
	}
}
