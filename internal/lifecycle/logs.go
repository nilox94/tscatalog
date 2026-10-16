package lifecycle

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"tscatalog/internal/catalog"
	"tscatalog/internal/composeclient"
)

// ReadTail returns the last n lines of path.
// n < 0 returns every line. n == 0 returns an empty slice.
// A missing file is "no log file".
func ReadTail(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no log file")
		}
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if n >= 0 && len(lines) > n {
			lines = lines[len(lines)-n:]
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if lines == nil {
		lines = []string{}
	}
	return lines, nil
}

// FollowFile seeks to the end of path and copies new bytes to w every 200ms.
// It returns nil when ctx is done. A missing file is an error before the loop.
func FollowFile(ctx context.Context, path string, w io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no log file")
		}
		return err
	}
	defer f.Close()
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	buf := make([]byte, 32*1024)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			for {
				n, err := f.Read(buf)
				if n > 0 {
					if _, werr := w.Write(buf[:n]); werr != nil {
						return werr
					}
				}
				if err == io.EOF || n == 0 {
					break
				}
				if err != nil {
					return err
				}
			}
		}
	}
}

// ReadSince reads bytes appended to path after offset.
func ReadSince(path string, offset int64) ([]byte, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, offset, err
	}
	if info.Size() < offset {
		offset = 0
	}
	if info.Size() == offset {
		return nil, offset, nil
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset, err
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, offset, err
	}
	return b, offset + int64(len(b)), nil
}

// LogFile is a process log the cat9s view can poll.
type LogFile struct {
	Path   string
	Offset int64
	Prefix string
}

type logJob struct {
	index     int
	spec      catalog.LaunchSpec
	kind      catalog.Kind
	id        string
	container string
}

// Logs writes the selected rows to w.
// which is the ps NAME. Empty which follows every row of the service.
// tailSet is true when the user passed --tail; external argv ignores it.
func Logs(ctx context.Context, s catalog.Service, which string, tail int, follow, tailSet bool, w io.Writer, comp *composeclient.Client) error {
	jobs, comp, err := prepareJobs(ctx, s, which, comp)
	if err != nil {
		return err
	}
	if tailSet {
		for _, job := range jobs {
			if job.kind == catalog.KindExternal {
				fmt.Fprintln(w, "external logs ignore --tail")
				break
			}
		}
	}
	return runJobs(ctx, s, jobs, tail, follow, w, comp)
}

// CaptureLogs is the non-following read used by cat9s.
// stream is true when a later follow should start an external argv or compose Logs.
func CaptureLogs(ctx context.Context, s catalog.Service, which string, comp *composeclient.Client) (lines []string, files []LogFile, stream bool, err error) {
	jobs, comp, err := prepareJobs(ctx, s, which, comp)
	if err != nil {
		return nil, nil, false, err
	}
	multi := len(jobs) > 1
	var first error
	for _, job := range jobs {
		switch job.kind {
		case catalog.KindProcess:
			path, lerr := logPath(s.Name, job.spec.ID(job.index))
			if lerr != nil {
				if first == nil {
					first = lerr
				}
				continue
			}
			got, lerr := ReadTail(path, 100)
			if lerr != nil {
				if first == nil {
					first = lerr
				}
				continue
			}
			prefix := ""
			if multi {
				prefix = job.id + " | "
			}
			lines = append(lines, prefixLines(prefix, got)...)
			info, serr := os.Stat(path)
			off := int64(0)
			if serr == nil {
				off = info.Size()
			}
			files = append(files, LogFile{Path: path, Offset: off, Prefix: prefix})
		case catalog.KindExternal:
			if len(job.spec.Logs) == 0 {
				if first == nil {
					first = fmt.Errorf("%s: no logs command", s.Name)
				}
				continue
			}
			got, lerr := captureExternal(ctx, job.spec)
			prefix := ""
			if multi {
				prefix = job.id + " | "
			}
			lines = append(lines, prefixLines(prefix, got)...)
			if lerr != nil && first == nil {
				first = lerr
			}
			stream = true
		case catalog.KindCompose:
			if comp == nil {
				if first == nil {
					first = fmt.Errorf("docker unavailable")
				}
				continue
			}
			got, lerr := captureCompose(ctx, job, comp, 100, false)
			lines = append(lines, got...)
			if lerr != nil && first == nil {
				first = lerr
			}
			stream = true
		}
	}
	return lines, files, stream, first
}

// FollowStreams runs external argv and compose log streams until ctx is done.
// Process files are not included; the caller polls those.
func FollowStreams(ctx context.Context, s catalog.Service, which string, out chan<- string) error {
	comp, err := dialFor(ctx, s)
	if err != nil {
		return err
	}
	jobs, comp, err := prepareJobs(ctx, s, which, comp)
	if err != nil {
		return err
	}
	multi := len(jobs) > 1
	var wg sync.WaitGroup
	var first error
	var mu sync.Mutex
	for _, job := range jobs {
		switch job.kind {
		case catalog.KindExternal:
			if len(job.spec.Logs) == 0 {
				mu.Lock()
				if first == nil {
					first = fmt.Errorf("%s: no logs command", s.Name)
				}
				mu.Unlock()
				continue
			}
			wg.Add(1)
			go func(job logJob) {
				defer wg.Done()
				err := streamExternal(ctx, job, multi, out)
				mu.Lock()
				if err != nil && first == nil && ctx.Err() == nil {
					first = err
				}
				mu.Unlock()
			}(job)
		case catalog.KindCompose:
			wg.Add(1)
			go func(job logJob) {
				defer wg.Done()
				err := streamCompose(ctx, job, comp, out)
				mu.Lock()
				if err != nil && first == nil && ctx.Err() == nil {
					first = err
				}
				mu.Unlock()
			}(job)
		}
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil
	}
	return first
}

func dialFor(ctx context.Context, s catalog.Service) (*composeclient.Client, error) {
	if !NeedsCompose([]catalog.Service{s}) {
		return nil, nil
	}
	return composeclient.New(ctx)
}

func prepareJobs(ctx context.Context, s catalog.Service, which string, comp *composeclient.Client) ([]logJob, *composeclient.Client, error) {
	if which == "" {
		if len(s.Launch) == 0 {
			return nil, comp, fmt.Errorf("%s: no logs command", s.Name)
		}
		jobs := make([]logJob, 0, len(s.Launch))
		for i, spec := range s.Launch {
			kind, err := spec.ResolveKind()
			if err != nil {
				return nil, comp, fmt.Errorf("%s: %w", s.Name, err)
			}
			jobs = append(jobs, logJob{index: i, spec: spec, kind: kind, id: spec.ID(i)})
		}
		if comp == nil && hasCompose(jobs) {
			c, err := composeclient.New(ctx)
			if err != nil {
				return nil, nil, err
			}
			comp = c
		}
		return jobs, comp, nil
	}
	if i, spec, err := s.Launch.Find(which); err == nil {
		kind, kerr := spec.ResolveKind()
		if kerr != nil {
			return nil, comp, fmt.Errorf("%s: %w", s.Name, kerr)
		}
		job := logJob{index: i, spec: spec, kind: kind, id: spec.ID(i)}
		if comp == nil && kind == catalog.KindCompose {
			c, err := composeclient.New(ctx)
			if err != nil {
				return nil, nil, err
			}
			comp = c
		}
		return []logJob{job}, comp, nil
	}
	var composeJobs []logJob
	for i, spec := range s.Launch {
		kind, err := spec.ResolveKind()
		if err != nil {
			return nil, comp, fmt.Errorf("%s: %w", s.Name, err)
		}
		if kind == catalog.KindCompose {
			composeJobs = append(composeJobs, logJob{index: i, spec: spec, kind: kind, id: spec.ID(i)})
		}
	}
	if len(composeJobs) != 1 {
		return nil, comp, fmt.Errorf("%s: unknown %q", s.Name, which)
	}
	if comp == nil {
		c, err := composeclient.New(ctx)
		if err != nil {
			return nil, nil, err
		}
		comp = c
	}
	project, err := composeProject(ctx, composeJobs[0].spec)
	if err != nil {
		return nil, comp, err
	}
	if _, ok := project.Services[which]; !ok {
		return nil, comp, fmt.Errorf("%s: unknown %q", s.Name, which)
	}
	job := composeJobs[0]
	job.container = which
	return []logJob{job}, comp, nil
}

func hasCompose(jobs []logJob) bool {
	for _, job := range jobs {
		if job.kind == catalog.KindCompose {
			return true
		}
	}
	return false
}

func runJobs(ctx context.Context, s catalog.Service, jobs []logJob, tail int, follow bool, w io.Writer, comp *composeclient.Client) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	multi := len(jobs) > 1
	errCh := make(chan error, len(jobs))
	var mu sync.Mutex
	for _, job := range jobs {
		go func(job logJob) {
			err := runJob(ctx, s, job, multi, tail, follow, w, &mu, comp)
			if err != nil {
				cancel()
			}
			errCh <- err
		}(job)
	}
	var first error
	for range jobs {
		if err := <-errCh; err != nil && first == nil {
			first = err
		}
	}
	if ctx.Err() != nil && first == nil {
		return nil
	}
	if ctx.Err() != nil && isCancel(first) {
		return nil
	}
	return first
}

func isCancel(err error) bool {
	return err == context.Canceled || err == context.DeadlineExceeded
}

func runJob(ctx context.Context, s catalog.Service, job logJob, multi bool, tail int, follow bool, w io.Writer, mu *sync.Mutex, comp *composeclient.Client) error {
	switch job.kind {
	case catalog.KindProcess:
		path, err := logPath(s.Name, job.spec.ID(job.index))
		if err != nil {
			return err
		}
		lines, err := ReadTail(path, tail)
		if err != nil {
			return err
		}
		prefix := ""
		if multi {
			prefix = job.id + " | "
		}
		mu.Lock()
		for _, line := range lines {
			fmt.Fprintln(w, prefix+line)
		}
		mu.Unlock()
		if !follow {
			return nil
		}
		var out io.Writer = w
		if prefix != "" {
			out = &prefixWriter{w: w, prefix: prefix, mu: mu}
		} else {
			out = &lockedWriter{w: w, mu: mu}
		}
		return FollowFile(ctx, path, out)
	case catalog.KindExternal:
		if len(job.spec.Logs) == 0 {
			return fmt.Errorf("%s: no logs command", s.Name)
		}
		return runExternalCmd(ctx, job, multi, w, mu)
	case catalog.KindCompose:
		if comp == nil {
			return fmt.Errorf("docker unavailable")
		}
		var out io.Writer = &lockedWriter{w: w, mu: mu}
		return streamComposeWriter(ctx, job, comp, tail, follow, out)
	default:
		return fmt.Errorf("%s: unknown kind", s.Name)
	}
}

func runExternalCmd(ctx context.Context, job logJob, multi bool, w io.Writer, mu *sync.Mutex) error {
	cmd := exec.Command(job.spec.Logs[0], job.spec.Logs[1:]...)
	if job.spec.Cwd != "" {
		cmd.Dir = expandHome(job.spec.Cwd)
	}
	cmd.Env = os.Environ()
	setPgroup(cmd, syscall.SIGTERM)
	if multi {
		pw := &prefixWriter{w: w, prefix: job.id + " | ", mu: mu}
		cmd.Stdout = pw
		cmd.Stderr = pw
	} else {
		cmd.Stdin = os.Stdin
		cmd.Stdout = w
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-ctx.Done():
		_ = signalGroup(cmd, syscall.SIGTERM)
		<-done
		return nil
	case err := <-done:
		if multi {
			if pw, ok := cmd.Stdout.(*prefixWriter); ok {
				_ = pw.Flush()
			}
		}
		return err
	}
}

func captureExternal(ctx context.Context, spec catalog.LaunchSpec) ([]string, error) {
	cmd := exec.Command(spec.Logs[0], spec.Logs[1:]...)
	if spec.Cwd != "" {
		cmd.Dir = expandHome(spec.Cwd)
	}
	cmd.Env = os.Environ()
	setPgroup(cmd, syscall.SIGTERM)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case <-ctx.Done():
		_ = signalGroup(cmd, syscall.SIGTERM)
		<-done
		return splitLines(stdout.String()), nil
	case err = <-done:
	}
	if err != nil {
		lines := splitLines(stderr.String())
		for i, line := range lines {
			lines[i] = "stderr: " + line
		}
		return lines, err
	}
	return splitLines(stdout.String()), nil
}

func captureCompose(ctx context.Context, job logJob, comp *composeclient.Client, tail int, follow bool) ([]string, error) {
	var buf strings.Builder
	err := streamComposeWriter(ctx, job, comp, tail, follow, &buf)
	return splitLines(buf.String()), err
}

func streamCompose(ctx context.Context, job logJob, comp *composeclient.Client, out chan<- string) error {
	if comp == nil {
		return fmt.Errorf("docker unavailable")
	}
	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		err := streamComposeWriter(ctx, job, comp, 0, true, pw)
		_ = pw.CloseWithError(err)
		errCh <- err
	}()
	sc := bufio.NewScanner(pr)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		select {
		case <-ctx.Done():
			_ = pr.Close()
			return nil
		case out <- sc.Text():
		}
	}
	_ = pr.Close()
	err := <-errCh
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return err
	}
	return sc.Err()
}

func streamExternal(ctx context.Context, job logJob, multi bool, out chan<- string) error {
	cmd := exec.Command(job.spec.Logs[0], job.spec.Logs[1:]...)
	if job.spec.Cwd != "" {
		cmd.Dir = expandHome(job.spec.Cwd)
	}
	cmd.Env = os.Environ()
	setPgroup(cmd, syscall.SIGTERM)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	prefix := ""
	if multi {
		prefix = job.id + " | "
	}
	var wg sync.WaitGroup
	wg.Add(2)
	scan := func(r io.Reader, failPrefix string) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			line := prefix + failPrefix + sc.Text()
			select {
			case <-ctx.Done():
				return
			case out <- line:
			}
		}
	}
	go scan(stdout, "")
	go scan(stderr, "")
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-ctx.Done():
		_ = signalGroup(cmd, syscall.SIGTERM)
		<-done
		wg.Wait()
		return nil
	case err := <-done:
		wg.Wait()
		return err
	}
}

func streamComposeWriter(ctx context.Context, job logJob, comp *composeclient.Client, tail int, follow bool, w io.Writer) error {
	project, err := composeProject(ctx, job.spec)
	if err != nil {
		return err
	}
	return comp.Logs(ctx, project, job.container, tail, follow, w)
}

func prefixLines(prefix string, lines []string) []string {
	if prefix == "" {
		return lines
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = prefix + line
	}
	return out
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

type lockedWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	if w.mu != nil {
		w.mu.Lock()
		defer w.mu.Unlock()
	}
	return w.w.Write(p)
}

type prefixWriter struct {
	w      io.Writer
	prefix string
	mu     *sync.Mutex
	buf    []byte
}

func (p *prefixWriter) Write(b []byte) (int, error) {
	if p.mu != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
	}
	p.buf = append(p.buf, b...)
	if err := p.flushLocked(false); err != nil {
		return 0, err
	}
	return len(b), nil
}

func (p *prefixWriter) Flush() error {
	if p.mu != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
	}
	return p.flushLocked(true)
}

func (p *prefixWriter) flushLocked(all bool) error {
	for {
		i := indexByte(p.buf, '\n')
		if i < 0 {
			break
		}
		line := p.buf[:i]
		p.buf = append([]byte(nil), p.buf[i+1:]...)
		if _, err := fmt.Fprintf(p.w, "%s%s\n", p.prefix, line); err != nil {
			return err
		}
	}
	if all && len(p.buf) > 0 {
		if _, err := fmt.Fprintf(p.w, "%s%s\n", p.prefix, p.buf); err != nil {
			return err
		}
		p.buf = nil
	}
	return nil
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}
