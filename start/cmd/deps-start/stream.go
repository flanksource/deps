package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/api/icons"

	"github.com/flanksource/deps/start"
)

// waitReportInterval throttles the "waiting for ..." lines so a fast start
// stays quiet and a slow one keeps reporting.
const waitReportInterval = 2 * time.Second

// prefixWriter writes each complete line to w prefixed with the service name,
// buffering partial lines so output from several sources stays line-atomic.
type prefixWriter struct {
	mu     sync.Mutex
	w      io.Writer
	prefix string
	buf    []byte
}

func newPrefixWriter(w io.Writer, name string, ansi bool) *prefixWriter {
	prefix := name + " │ "
	if ansi {
		prefix = api.Text{}.Append(prefix, "text-blue-500").ANSI()
	}
	return &prefixWriter{w: w, prefix: prefix}
}

func (p *prefixWriter) Write(data []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buf = append(p.buf, data...)
	for {
		end := bytes.IndexByte(p.buf, '\n')
		if end < 0 {
			return len(data), nil
		}
		line := bytes.TrimRight(p.buf[:end], "\r")
		p.buf = p.buf[end+1:]
		if _, err := fmt.Fprintf(p.w, "%s%s\n", p.prefix, line); err != nil {
			return len(data), err
		}
	}
}

// Flush emits a trailing partial line, if any.
func (p *prefixWriter) Flush() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.buf) == 0 {
		return
	}
	fmt.Fprintf(p.w, "%s%s\n", p.prefix, p.buf)
	p.buf = nil
}

// serviceStream carries a service's own output to stderr while it starts, so
// stdout stays reserved for the structured connection output.
type serviceStream struct {
	writer   *prefixWriter
	reporter *waitReporter
}

// newServiceStream returns nil inside a detached supervisor, whose stderr is
// the supervisor log file: the parent process tails the service log instead,
// so streaming there would only duplicate it.
func newServiceStream(name string) *serviceStream {
	if os.Getenv(supervisorEnv) != "" {
		return nil
	}
	return &serviceStream{
		writer:   newPrefixWriter(os.Stderr, name, isTTY(os.Stderr)),
		reporter: &waitReporter{},
	}
}

// options wires the stream into a start call: service output is teed to
// stderr and every unmet readiness condition is reported.
func (s *serviceStream) options() []start.Option {
	if s == nil {
		return nil
	}
	return []start.Option{
		start.WithLogWriter(s.writer),
		start.WithOnWaiting(s.reporter.report),
	}
}

// logWriter is the prefixed stderr sink for the service's own output.
func (s *serviceStream) logWriter() io.Writer {
	if s == nil {
		return io.Discard
	}
	return s.writer
}

func (s *serviceStream) Close() {
	if s == nil {
		return
	}
	s.writer.Flush()
}

// waitReporter prints what a readiness wait is blocked on, repeating at most
// once per waitReportInterval per condition.
type waitReporter struct {
	mu   sync.Mutex
	last string
	next time.Time
}

func (r *waitReporter) report(readiness start.Readiness) {
	r.mu.Lock()
	if readiness.Waiting == r.last && time.Now().Before(r.next) {
		r.mu.Unlock()
		return
	}
	r.last, r.next = readiness.Waiting, time.Now().Add(waitReportInterval)
	r.mu.Unlock()

	message := fmt.Sprintf("waiting for %s (%s)", readiness.Waiting, readiness.Elapsed)
	if listening := portsLabel(readiness.Ports); listening != "" {
		message += ", listening on " + listening
	}
	printAction(icons.Pending, "muted", "%s", message)
}

// portsLabel renders ports as URLs so terminals turn them into links.
func portsLabel(ports []int) string {
	if len(ports) == 0 {
		return ""
	}
	urls := make([]string, len(ports))
	for i, port := range ports {
		urls[i] = fmt.Sprintf("http://localhost:%d", port)
	}
	return strings.Join(urls, " ")
}

// printReady reports a service as ready, pointing at the ports it published.
func printReady(name string, info start.ServiceInfo, elapsed time.Duration) {
	if listening := portsLabel(portValues(info.Ports)); listening != "" {
		printAction(icons.Success, "text-green-600", "%s ready on %s (%s)", name, listening, elapsed.Truncate(100*time.Millisecond))
		return
	}
	printAction(icons.Success, "text-green-600", "%s ready (%s)", name, elapsed.Truncate(100*time.Millisecond))
}

// logTail is a log file to follow from a known offset.
type logTail struct {
	path   string
	offset int64
}

// logTailFrom records a log file's current size, so following it shows only
// output written from now on rather than replaying earlier runs (service and
// supervisor logs are appended across starts).
func logTailFrom(path string) logTail {
	info, err := os.Stat(path)
	if err != nil {
		return logTail{path: path}
	}
	return logTail{path: path, offset: info.Size()}
}

// tailLogsUntil follows log files to w until ctx is cancelled. Files that do
// not exist yet are retried: a detached supervisor creates them only once it
// reaches the runtime.
func tailLogsUntil(ctx context.Context, w io.Writer, tails ...logTail) {
	for _, tail := range tails {
		go func(tail logTail) {
			for ctx.Err() == nil {
				if err := tail.follow(ctx, w); err == nil {
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(300 * time.Millisecond):
				}
			}
		}(tail)
	}
}

// follow copies appended output to w until ctx is cancelled, returning an
// error only when the file cannot be opened.
func (t logTail) follow(ctx context.Context, w io.Writer) error {
	f, err := os.Open(t.path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return nil
	}
	for {
		if _, err := io.Copy(w, f); err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(200 * time.Millisecond):
		}
	}
}
