package composeclient

import (
	"fmt"
	"strconv"

	composeapi "github.com/docker/compose/v5/pkg/api"
)

// StateDetail is a rollup of one compose project.
type StateDetail struct {
	State  string
	Detail string
}

// ContainerLine is one container row. ID is the compose service, or service#n
// when that service appears more than once.
type ContainerLine struct {
	ID, State, Detail string
}

// ContainerLines rolls container rows into a project state and one line each.
// State strings are running, exited, and gone.
func ContainerLines(rows []composeapi.ContainerSummary) (roll StateDetail, children []ContainerLine) {
	if len(rows) == 0 {
		return StateDetail{State: "gone", Detail: "not started"}, nil
	}
	counts := map[string]int{}
	for _, row := range rows {
		counts[row.Service]++
	}
	nRunning, nExited := 0, 0
	seen := map[string]int{}
	for _, row := range rows {
		if row.State == "running" || row.State == "restarting" {
			nRunning++
		}
		if row.State == "exited" || row.State == "dead" {
			nExited++
		}
		seen[row.Service]++
		id := row.Service
		if counts[row.Service] > 1 {
			id = row.Service + "#" + strconv.Itoa(seen[row.Service])
		}
		children = append(children, ContainerLine{
			ID:     id,
			State:  childState(row),
			Detail: childDetail(row),
		})
	}
	switch {
	case nRunning > 0:
		roll = StateDetail{State: "running", Detail: fmt.Sprintf("%d/%d running", nRunning, len(rows))}
	case nExited > 0:
		roll = StateDetail{State: "exited", Detail: fmt.Sprintf("%d exited", nExited)}
	default:
		roll = StateDetail{State: "gone", Detail: string(rows[0].State)}
	}
	return roll, children
}

func childState(row composeapi.ContainerSummary) string {
	switch row.State {
	case "running", "restarting", "paused":
		return "running"
	case "exited", "dead":
		return "exited"
	default:
		return "gone"
	}
}

func childDetail(row composeapi.ContainerSummary) string {
	switch row.State {
	case "paused":
		return "paused"
	case "running", "restarting":
		if row.Health != "" {
			return string(row.Health)
		}
		return "running"
	case "exited", "dead":
		return fmt.Sprintf("exit %d", row.ExitCode)
	default:
		return string(row.State)
	}
}
