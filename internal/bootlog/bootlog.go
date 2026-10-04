// Package bootlog follows BootstrapLog.txt and reports the allocation state of
// one server process. Line format: "<RFC3339 time>\t0x<pid hex>\t<message>".
package bootlog

import (
	"bufio"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// Event is one parsed log line.
type Event struct {
	Time    time.Time
	PID     int
	Message string
	State   string // new state for "state transition" lines, else ""
}

// Parse reads one line; ok is false for lines that do not match the format.
func Parse(line string) (Event, bool) {
	parts := strings.SplitN(strings.TrimRight(line, "\r\n"), "\t", 3)
	if len(parts) != 3 || !strings.HasPrefix(parts[1], "0x") {
		return Event{}, false
	}
	pid, err := strconv.ParseInt(parts[1][2:], 16, 64)
	if err != nil {
		return Event{}, false
	}
	t, _ := time.Parse(time.RFC3339Nano, parts[0])
	ev := Event{Time: t, PID: int(pid), Message: parts[2]}
	const marker = "WaitForAllocation state transition: '"
	if i := strings.Index(ev.Message, marker); i >= 0 {
		// ... '<old>' -> '<new>'
		if j := strings.LastIndex(ev.Message, "-> '"); j >= 0 {
			ev.State = strings.TrimSuffix(ev.Message[j+4:], "'")
		}
	}
	return ev, true
}

// Follower tails the log from its current end and delivers events for one PID
// (0 = any PID).
type Follower struct {
	Path string
	PID  int
}

// Run polls the file until stop is closed. It tolerates the file being absent,
// truncated or recreated.
func (f Follower) Run(stop <-chan struct{}, out chan<- Event) {
	var offset int64 = -1 // -1: start at the current end
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		offset = f.poll(offset, out)
		select {
		case <-stop:
			return
		case <-tick.C:
		}
	}
}

func (f Follower) poll(offset int64, out chan<- Event) int64 {
	fh, err := os.Open(f.Path)
	if err != nil {
		return offset
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		return offset
	}
	if offset < 0 || offset > st.Size() {
		if offset < 0 {
			return st.Size()
		}
		offset = 0 // truncated or replaced
	}
	if _, err := fh.Seek(offset, io.SeekStart); err != nil {
		return offset
	}
	r := bufio.NewReader(fh)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return offset // keep a partial last line for the next poll
		}
		offset += int64(len(line))
		if ev, ok := Parse(line); ok && (f.PID == 0 || ev.PID == f.PID) {
			out <- ev
		}
	}
}
