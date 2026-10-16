package composeclient

import (
	"testing"

	composeapi "github.com/docker/compose/v5/pkg/api"
)

func TestContainerLines(t *testing.T) {
	roll, children := ContainerLines([]composeapi.ContainerSummary{
		{Service: "app", State: "running", Health: "healthy"},
		{Service: "db", State: "exited", ExitCode: 3},
	})
	if roll.State != "running" || roll.Detail != "1/2 running" {
		t.Fatalf("roll %+v", roll)
	}
	if len(children) != 2 || children[0].Detail != "healthy" || children[1].Detail != "exit 3" {
		t.Fatalf("children %+v", children)
	}
	if children[0].ID != "app" || children[0].State != "running" || children[1].State != "exited" {
		t.Fatalf("children %+v", children)
	}
}
