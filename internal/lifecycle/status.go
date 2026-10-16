package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"tscatalog/internal/catalog"
	"tscatalog/internal/composeclient"
	"unicode/utf8"
)

// LaunchState is the state column of one ps row.
type LaunchState string

const (
	LaunchRunning   LaunchState = "running"
	LaunchExited    LaunchState = "exited"
	LaunchSignaled  LaunchState = "signaled"
	LaunchStopped   LaunchState = "stopped"
	LaunchGone      LaunchState = "gone"
	LaunchUnwatched LaunchState = "unwatched"
)

// Shape is the ps shape column. An external helper has an empty shape.
type Shape string

const (
	ShapeProcess   Shape = "process"
	ShapeContainer Shape = "container"
)

// LaunchStatus is one ps row.
type LaunchStatus struct {
	Service string
	ID      string
	Index   int
	Shape   Shape
	State   LaunchState
	PID     int
	Detail  string
}

// LaunchStatuses reports one row per process, one row per container, or one
// row for an external helper. A compose dial failure is a single gone row;
// it does not fail the caller. comp and dialErr are ignored for a service
// that has no compose spec.
func LaunchStatuses(ctx context.Context, s catalog.Service, comp *composeclient.Client, dialErr error) []LaunchStatus {
	if len(s.Launch) == 0 {
		return nil
	}
	var rows []LaunchStatus
	for i, spec := range s.Launch {
		kind, err := spec.ResolveKind()
		if err != nil {
			rows = append(rows, LaunchStatus{
				Service: s.Name,
				ID:      rowID(s, i, spec),
				Index:   i,
				State:   LaunchGone,
				Detail:  err.Error(),
			})
			continue
		}
		switch kind {
		case catalog.KindProcess:
			rows = append(rows, processStatus(s, i, spec))
		case catalog.KindExternal:
			rows = append(rows, externalStatus(ctx, s, i, spec))
		case catalog.KindCompose:
			rows = append(rows, composeStatus(ctx, s, i, spec, comp, dialErr)...)
		}
	}
	return rows
}

func rowID(s catalog.Service, index int, spec catalog.LaunchSpec) string {
	if spec.Name != "" || len(s.Launch) > 1 {
		return spec.ID(index)
	}
	return ""
}

func externalStatus(ctx context.Context, s catalog.Service, index int, spec catalog.LaunchSpec) LaunchStatus {
	st := LaunchStatus{
		Service: s.Name,
		ID:      rowID(s, index, spec),
		Index:   index,
	}
	if len(spec.Status) == 0 {
		st.State = LaunchUnwatched
		st.Detail = "no status command"
		return st
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, spec.Status[0], spec.Status[1:]...)
	if spec.Cwd != "" {
		cmd.Dir = expandHome(spec.Cwd)
	}
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	setPgroup(cmd, syscall.SIGKILL)
	err := cmd.Run()
	if cctx.Err() == context.DeadlineExceeded {
		st.State = LaunchGone
		st.Detail = "status timed out"
		return st
	}
	line := firstLine(stdout.String(), stderr.String())
	if err == nil {
		st.State = LaunchRunning
		if line == "" {
			line = "ok"
		}
		st.Detail = cutDetail(line)
		return st
	}
	st.State = LaunchExited
	if line == "" {
		code := 1
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		line = fmt.Sprintf("exit %d", code)
	}
	st.Detail = cutDetail(line)
	return st
}

func firstLine(stdout, stderr string) string {
	if line := firstNonEmpty(stdout); line != "" {
		return line
	}
	return firstNonEmpty(stderr)
}

func firstNonEmpty(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func cutDetail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 120 {
		return s
	}
	b := s[:120]
	for !utf8.ValidString(b) && len(b) > 0 {
		b = b[:len(b)-1]
	}
	return b + "…"
}

func composeStatus(ctx context.Context, s catalog.Service, index int, spec catalog.LaunchSpec, comp *composeclient.Client, dialErr error) []LaunchStatus {
	placeholder := func(state LaunchState, detail string) LaunchStatus {
		return LaunchStatus{
			Service: s.Name,
			ID:      rowID(s, index, spec),
			Index:   index,
			State:   state,
			Detail:  detail,
		}
	}
	if comp == nil {
		detail := "docker unavailable"
		if dialErr != nil {
			detail = dialErr.Error()
		}
		return []LaunchStatus{placeholder(LaunchGone, detail)}
	}
	project, err := composeProject(ctx, spec)
	if err != nil {
		return []LaunchStatus{placeholder(LaunchGone, composeErr(err))}
	}
	rows, err := comp.Containers(ctx, project)
	if err != nil {
		return []LaunchStatus{placeholder(LaunchGone, composeErr(err))}
	}
	roll, children := composeclient.ContainerLines(rows)
	if len(children) == 0 {
		return []LaunchStatus{placeholder(LaunchState(roll.State), roll.Detail)}
	}
	out := make([]LaunchStatus, 0, len(children))
	for _, child := range children {
		out = append(out, LaunchStatus{
			Service: s.Name,
			ID:      child.ID,
			Index:   index,
			Shape:   ShapeContainer,
			State:   LaunchState(child.State),
			Detail:  child.Detail,
		})
	}
	return out
}

func composeErr(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "docker timed out"
	}
	return err.Error()
}

// Describe is the describe/cat9s field for one row.
// The key is process, container, or external. The value omits an empty id.
func (st LaunchStatus) Describe() (key, value string) {
	switch st.Shape {
	case ShapeProcess:
		key = "process"
	case ShapeContainer:
		key = "container"
	default:
		key = "external"
	}
	var parts []string
	if st.ID != "" {
		parts = append(parts, st.ID)
	}
	parts = append(parts, string(st.State))
	if st.State == LaunchRunning && st.PID != 0 {
		parts = append(parts, fmt.Sprintf("pid %d", st.PID))
	}
	if st.Detail != "" {
		parts = append(parts, st.Detail)
	}
	return key, strings.Join(parts, "  ")
}

// FormatPS renders the ps table. Empty id and shape print as "-". PID 0 prints as "-".
func FormatPS(rows []LaunchStatus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-16s %-18s %-9s %-10s %-8s %s\n", "SERVICE", "NAME", "SHAPE", "STATE", "PID", "DETAIL")
	for _, row := range rows {
		fmt.Fprintf(
			&b, "%-16s %-18s %-9s %-10s %-8s %s\n",
			row.Service,
			dash(row.ID),
			dash(string(row.Shape)),
			row.State,
			pidCell(row.PID),
			row.Detail,
		)
	}
	return b.String()
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func pidCell(pid int) string {
	if pid == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", pid)
}
