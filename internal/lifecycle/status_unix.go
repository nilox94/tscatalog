//go:build unix

package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"tscatalog/internal/catalog"
)

func processStatus(s catalog.Service, index int, spec catalog.LaunchSpec) LaunchStatus {
	st := LaunchStatus{
		Service: s.Name,
		ID:      rowID(s, index, spec),
		Index:   index,
		Shape:   ShapeProcess,
	}
	rec, err := readPidfile(s.Name, index)
	if err == nil && sameProcess(rec.PID, rec.StartID) {
		st.State = LaunchRunning
		st.PID = rec.PID
		st.Command = strings.Join(spec.Start, " ")
		st.Detail = st.Command
		return st
	}
	exitfile, perr := exitPath(s.Name, spec.ID(index))
	if perr != nil {
		st.State = LaunchGone
		st.Detail = perr.Error()
		return st
	}
	got, rerr := readExit(exitfile)
	if rerr == nil {
		st.State, st.Detail = exitState(got)
		return st
	}
	if !os.IsNotExist(rerr) {
		st.State = LaunchGone
		st.Detail = rerr.Error()
		return st
	}
	if err == nil {
		st.State = LaunchGone
		st.Detail = "no exit status"
		return st
	}
	var missing noPidfileError
	if errors.As(err, &missing) {
		st.State = LaunchGone
		st.Detail = "not started"
		return st
	}
	st.State = LaunchGone
	st.Detail = err.Error()
	return st
}

func exitState(rec ExitRecord) (LaunchState, string) {
	switch rec.State {
	case string(LaunchExited):
		return LaunchExited, fmt.Sprintf("exit %d", rec.Code)
	case string(LaunchSignaled):
		return LaunchSignaled, fmt.Sprintf("signal %s", rec.Signal)
	case string(LaunchStopped):
		return LaunchStopped, "stopped"
	default:
		if rec.State == "" {
			return LaunchGone, "bad exit status"
		}
		return LaunchGone, rec.State
	}
}
