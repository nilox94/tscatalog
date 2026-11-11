package lifecycle

import (
	"context"
	"os"
	"path/filepath"

	"github.com/nilox94/tscatalog/internal/catalog"
	"github.com/nilox94/tscatalog/internal/composeclient"

	"github.com/compose-spec/compose-go/v2/types"
)

type composeCtxKey struct{}

// WithCompose attaches a client opened once for an up, stop, or restart.
func WithCompose(ctx context.Context, c *composeclient.Client) context.Context {
	if c == nil {
		return ctx
	}
	return context.WithValue(ctx, composeCtxKey{}, c)
}

func composeFrom(ctx context.Context) *composeclient.Client {
	c, _ := ctx.Value(composeCtxKey{}).(*composeclient.Client)
	return c
}

// NeedsCompose reports whether any spec resolves to compose.
func NeedsCompose(svcs []catalog.Service) bool {
	for _, s := range svcs {
		for _, spec := range s.Launch {
			kind, err := spec.ResolveKind()
			if err == nil && kind == catalog.KindCompose {
				return true
			}
		}
	}
	return false
}

// DialCompose opens one client when a selected service is compose.
// It returns (nil, nil) when none are. A dial failure is returned to the caller.
func DialCompose(ctx context.Context, svcs []catalog.Service) (*composeclient.Client, error) {
	if !NeedsCompose(svcs) {
		return nil, nil
	}
	return composeclient.New(ctx)
}

func composeProject(ctx context.Context, spec catalog.LaunchSpec) (*types.Project, error) {
	services, files, projectName, err := spec.ComposeSource()
	if err != nil {
		return nil, err
	}
	dir := expandHome(spec.Cwd)
	files, err = absComposeFiles(dir, files)
	if err != nil {
		return nil, err
	}
	p, err := composeclient.Load(ctx, dir, files, projectName)
	if err != nil {
		return nil, err
	}
	return composeclient.Narrow(p, services)
}

// absComposeFiles resolves relative compose files against the launch cwd.
// An empty dir is the process working directory. Absolute paths are unchanged.
func absComposeFiles(dir string, files []string) ([]string, error) {
	if len(files) == 0 {
		return nil, nil
	}
	base := dir
	if base == "" {
		var err error
		base, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	out := make([]string, len(files))
	for i, f := range files {
		if filepath.IsAbs(f) {
			out[i] = f
			continue
		}
		out[i] = filepath.Join(base, f)
	}
	return out, nil
}

func composeClient(ctx context.Context) (*composeclient.Client, error) {
	if c := composeFrom(ctx); c != nil {
		return c, nil
	}
	return composeclient.New(ctx)
}

func startCompose(ctx context.Context, spec catalog.LaunchSpec) error {
	comp, err := composeClient(ctx)
	if err != nil {
		return err
	}
	project, err := composeProject(ctx, spec)
	if err != nil {
		return err
	}
	return comp.Up(ctx, project)
}

func stopCompose(ctx context.Context, spec catalog.LaunchSpec) error {
	comp, err := composeClient(ctx)
	if err != nil {
		return err
	}
	project, err := composeProject(ctx, spec)
	if err != nil {
		return err
	}
	return comp.Down(ctx, project)
}
