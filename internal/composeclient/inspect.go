package composeclient

import (
	"context"
	"strings"

	mobyclient "github.com/moby/moby/client"
)

// EnrichCommands fills Command on running container lines from docker inspect Path.
func (c *Client) EnrichCommands(ctx context.Context, children []ContainerLine) {
	if c == nil || c.docker == nil {
		return
	}
	for i := range children {
		if children[i].State != "running" || children[i].ContainerID == "" {
			continue
		}
		insp, err := c.docker.ContainerInspect(ctx, children[i].ContainerID, mobyclient.ContainerInspectOptions{})
		if err != nil {
			continue
		}
		children[i].Command = commandFromInspect(insp.Container.Path, insp.Container.Args)
	}
}

func commandFromInspect(path string, args []string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if len(args) == 0 {
		return path
	}
	return path + " " + strings.Join(args, " ")
}
