package lifecycle

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"tscatalog/internal/catalog"
)

// Action is what a restart should do to the local process.
type Action int

const (
	ActionNone Action = iota
	ActionStart
	ActionRestart
)

// Decision is whether restarting the process can fix a row.
type Decision struct {
	Action Action
	// Note explains ActionNone. Empty when a restart will run.
	Note string
}

// CatalogOwns reports whether this catalog can stop the service.
// A process counts while its pidfile is still the process we started.
// An external helper counts when it has a stop argv.
// A compose project always counts. Anything else listening on the port is not ours.
func CatalogOwns(s catalog.Service) bool {
	if !s.Launch.Defined() {
		return false
	}
	for i, spec := range s.Launch {
		kind, err := spec.ResolveKind()
		if err != nil {
			continue
		}
		switch kind {
		case catalog.KindProcess:
			if detachedAlive(s.Name, i) {
				return true
			}
		case catalog.KindExternal:
			if len(spec.Stop) > 0 {
				return true
			}
		case catalog.KindCompose:
			return true
		}
	}
	return false
}

// Decide says whether restarting the process can fix this row.
// A healthy local process is not restarted, including when the host is
// only not-approved or draining.
func Decide(s catalog.Service, rep Report, owned bool) Decision {
	if LocalHealthy(s, rep) {
		switch {
		case strings.Contains(rep.Host, "not-approved"):
			return Decision{Note: "host is not-approved; restart will not fix it"}
		case strings.Contains(rep.Host, "draining"):
			return Decision{Note: "host is draining; restart will not fix it"}
		case !rep.OK && owned:
			return Decision{Action: ActionRestart}
		default:
			return Decision{Note: "local process is healthy"}
		}
	}
	if rep.Bind == "error" {
		if rep.Local != "" {
			return Decision{Note: rep.Local}
		}
		return Decision{Note: "bad local address"}
	}
	if !s.Launch.Defined() {
		return Decision{Note: "no launch in catalog"}
	}
	if rep.Bind == "down" {
		return Decision{Action: ActionStart}
	}
	if !owned {
		return Decision{Note: fmt.Sprintf("something else holds %s", s.Local)}
	}
	return Decision{Action: ActionRestart}
}

// Restart starts name when nothing is listening. When the port is open, it
// stops that service first, and only if this catalog owns the process.
// requires are started locally and are not published. Stop never follows requires.
func Restart(ctx context.Context, cat *catalog.File, name string, w io.Writer) error {
	s, ok := cat.Lookup(name)
	if !ok {
		return fmt.Errorf("unknown service %q", name)
	}
	if !s.Launch.Defined() {
		return fmt.Errorf("%s: no launch in catalog", s.Name)
	}
	open, err := portOpen(ctx, s)
	if err != nil {
		return err
	}
	if open {
		if !CatalogOwns(s) {
			return fmt.Errorf("%s: something else holds %s", s.Name, s.Local)
		}
		comp, err := DialCompose(ctx, []catalog.Service{s})
		if err != nil {
			return err
		}
		ctx = WithCompose(ctx, comp)
		if err := Stop(ctx, s, w); err != nil {
			return err
		}
		if err := waitClosed(ctx, s, 10*time.Second); err != nil {
			return err
		}
	}
	svcs, err := cat.ExpandForRuntime([]string{s.Name})
	if err != nil {
		return err
	}
	if composeFrom(ctx) == nil {
		comp, err := DialCompose(ctx, svcs)
		if err != nil {
			return err
		}
		ctx = WithCompose(ctx, comp)
	}
	for _, dep := range svcs {
		if err := Start(ctx, dep, w); err != nil {
			return err
		}
	}
	return nil
}

func portOpen(ctx context.Context, s catalog.Service) (bool, error) {
	host, port, err := splitLocal(s.Local)
	if err != nil {
		return false, fmt.Errorf("%s: %w", s.Name, err)
	}
	err = dialTCP(ctx, net.JoinHostPort(host, port))
	if err == nil {
		return true, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	return false, nil
}

func waitClosed(ctx context.Context, s catalog.Service, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		open, err := portOpen(ctx, s)
		if err != nil {
			return err
		}
		if !open {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s: still listening on %s after stop", s.Name, s.Local)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
