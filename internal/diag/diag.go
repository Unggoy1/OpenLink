// Package diag builds the diagnostics reports that hosts and players send
// when something goes wrong. Reports are plain text meant to be pasted in
// public channels, so Redact removes what should not be shared.
package diag

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	ipv4   = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	secret = regexp.MustCompile(`(?i)("?\b(?:token|register_key|admin_key|key|secret|password)"?\s*[:=]\s*"?)([^\s",}]+)`)
)

// Redact replaces public IPv4 addresses with "<public IP>" and the values of
// token/key/secret fields with "<redacted>". Private, loopback, link-local and
// unspecified addresses stay: they help debugging and identify no one.
func Redact(s string) string {
	s = ipv4.ReplaceAllStringFunc(s, func(m string) string {
		ip := net.ParseIP(m)
		if ip == nil || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() ||
			ip.Equal(net.IPv4bcast) {
			return m
		}
		return "<public IP>"
	})
	s = secret.ReplaceAllString(s, "${1}<redacted>")
	// The home folder's path names the Windows user.
	if home, err := os.UserHomeDir(); err == nil && len(home) > 3 {
		for _, h := range []string{home, filepath.ToSlash(home), strings.ReplaceAll(home, `\`, `\\`)} {
			s = replaceFold(s, h, "~")
		}
	}
	return s
}

// replaceFold replaces old in s ignoring case (Windows paths).
func replaceFold(s, old, new string) string {
	lower, target := strings.ToLower(s), strings.ToLower(old)
	if len(lower) != len(s) || len(target) != len(old) {
		return strings.ReplaceAll(s, old, new) // lowering changed byte lengths
	}
	var b strings.Builder
	for {
		i := strings.Index(lower, target)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i] + new)
		s, lower = s[i+len(old):], lower[i+len(old):]
	}
}

// Report is a diagnostics report: titled sections of text.
type Report struct {
	b strings.Builder
}

// Section adds a titled section; body is redacted.
func (r *Report) Section(title, body string) {
	fmt.Fprintf(&r.b, "== %s ==\n%s\n\n", title, strings.TrimRight(Redact(body), "\n"))
}

// Line adds "key: value" to the current section (redacted).
func (r *Report) Line(key string, value any) {
	fmt.Fprintf(&r.b, "%s: %s\n", key, Redact(fmt.Sprint(value)))
}

func (r *Report) String() string { return r.b.String() }

// Events keeps the most recent lines of a program's own event log in memory.
type Events struct {
	mu    sync.Mutex
	lines []string
	max   int
}

// NewEvents keeps up to max lines.
func NewEvents(max int) *Events { return &Events{max: max} }

// Add records one line with a timestamp.
func (e *Events) Add(format string, args ...any) {
	line := time.Now().Format("15:04:05 ") + fmt.Sprintf(format, args...)
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.lines) == e.max {
		copy(e.lines, e.lines[1:])
		e.lines = e.lines[:e.max-1]
	}
	e.lines = append(e.lines, line)
}

// String returns the recorded lines, oldest first.
func (e *Events) String() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return strings.Join(e.lines, "\n")
}

// Tail returns the last n lines of text.
func Tail(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
