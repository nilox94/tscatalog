// Package composeclient talks to a Compose project through the Compose SDK.
// It does not shell out to `docker compose`.
package composeclient

import (
	"context"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	composegocli "github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	composeapi "github.com/docker/compose/v5/pkg/api"
	sdk "github.com/docker/compose/v5/pkg/compose"
	mobyclient "github.com/moby/moby/client"
)

// Client is one Compose SDK session.
type Client struct {
	api    composeapi.Compose
	docker mobyclient.APIClient
}

// New connects a Compose service to the local Docker CLI defaults.
// A canceled ctx returns before Initialize finishes. The default prompt is
// left in place (it returns the suggested default).
func New(ctx context.Context) (*Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	type result struct {
		c   *Client
		err error
	}
	ch := make(chan result, 1)
	go func() {
		c, err := newClient()
		ch <- result{c, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		return r.c, r.err
	}
}

func newClient() (*Client, error) {
	dockerCli, err := command.NewDockerCli()
	if err != nil {
		return nil, err
	}
	opts := flags.NewClientOptions()
	opts.LogLevel = "error"
	if err := dockerCli.Initialize(opts); err != nil {
		return nil, err
	}
	svc, err := sdk.NewComposeService(dockerCli)
	if err != nil {
		return nil, err
	}
	return &Client{api: svc, docker: dockerCli.Client()}, nil
}

// Load reads a compose project. An empty dir is the current directory.
// An empty files list discovers compose.yaml, compose.yml, docker-compose.yml,
// and docker-compose.yaml in that directory (and a parent, which is compose-go's walk).
// projectName is applied only when it is non-empty; a file that sets `name:` is left alone.
func Load(ctx context.Context, dir string, files []string, projectName string) (*types.Project, error) {
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	opts := []composegocli.ProjectOptionsFn{
		composegocli.WithWorkingDirectory(abs),
		composegocli.WithOsEnv,
		composegocli.WithDotEnv,
		composegocli.WithConfigFileEnv,
		composegocli.WithDefaultConfigPath,
	}
	if projectName != "" {
		opts = append(opts, composegocli.WithName(projectName))
	}
	options, err := composegocli.NewProjectOptions(files, opts...)
	if err != nil {
		return nil, err
	}
	project, err := options.LoadProject(ctx)
	if err != nil {
		return nil, err
	}
	// The Compose CLI adds these after load. Without them, created containers
	// have no project label, so down cannot see them and the next up collides
	// on the container name.
	stampComposeLabels(project)
	return project, nil
}

// stampComposeLabels matches composeService.postProcessProject. The labels are
// copied onto each container when it is created.
func stampComposeLabels(p *types.Project) {
	if p == nil {
		return
	}
	files := strings.Join(p.ComposeFiles, ",")
	for name, svc := range p.Services {
		svc.CustomLabels = map[string]string{
			composeapi.ProjectLabel:     p.Name,
			composeapi.ServiceLabel:     name,
			composeapi.VersionLabel:     composeapi.ComposeVersion,
			composeapi.WorkingDirLabel:  p.WorkingDir,
			composeapi.ConfigFilesLabel: files,
			composeapi.OneoffLabel:      "False",
		}
		p.Services[name] = svc
	}
}

// Narrow limits the project to services. An empty list returns p unchanged.
// The default dependency set includes depends_on. An unknown service is the
// error from WithSelectedServices; the caller wraps it.
func Narrow(p *types.Project, services []string) (*types.Project, error) {
	if len(services) == 0 || p == nil {
		return p, nil
	}
	return p.WithSelectedServices(services)
}

// Up starts the project and returns after the containers are started.
// Build is set so an image-less service is built. An empty recreate policy
// recreates only when the config hash or image digest changed.
func (c *Client) Up(ctx context.Context, project *types.Project) error {
	return c.api.Up(ctx, project, composeapi.UpOptions{
		Create: composeapi.CreateOptions{
			Build:     &composeapi.BuildOptions{},
			QuietPull: false,
		},
	})
}

// Down stops the project. Volumes are kept. A narrowed project is the whole down set.
func (c *Client) Down(ctx context.Context, project *types.Project) error {
	return c.api.Down(ctx, project.Name, composeapi.DownOptions{Project: project})
}

// Containers lists running and exited containers.
func (c *Client) Containers(ctx context.Context, project *types.Project) ([]composeapi.ContainerSummary, error) {
	return c.api.Ps(ctx, project.Name, composeapi.PsOptions{Project: project, All: true})
}

// Logs writes container output to w. tail < 0 requests every line.
// A non-empty service limits the stream to that compose service.
func (c *Client) Logs(ctx context.Context, project *types.Project, service string, tail int, follow bool, w io.Writer) error {
	tailStr := strconv.Itoa(tail)
	if tail < 0 {
		tailStr = "all"
	}
	opts := composeapi.LogOptions{
		Project: project,
		Follow:  follow,
		Tail:    tailStr,
	}
	if service != "" {
		opts.Services = []string{service}
	}
	return c.api.Logs(ctx, project.Name, &lineConsumer{w: w}, opts)
}

type lineConsumer struct {
	w  io.Writer
	mu sync.Mutex
}

func (c *lineConsumer) Log(name, msg string)  { c.write(name, msg) }
func (c *lineConsumer) Err(name, msg string)  { c.write(name, msg) }
func (c *lineConsumer) Status(string, string) {}

func (c *lineConsumer) write(name, msg string) {
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _ = io.WriteString(c.w, name+" | "+msg)
}
