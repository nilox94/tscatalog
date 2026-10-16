package tui

import (
	"strings"

	"tscatalog/internal/catalog"
	"tscatalog/internal/lifecycle"
)

// Row is one catalog service plus the latest probe.
type Row struct {
	Service  catalog.Service
	Report   lifecycle.Report
	Serve    string
	URL      string
	Owned    bool
	Fresh    bool
	Launches []lifecycle.LaunchStatus
}

func order(rows []Row, filter string) []Row {
	var hot, rest []Row
	for _, row := range rows {
		if !matches(row, filter) {
			continue
		}
		if attention(row) {
			hot = append(hot, row)
		} else {
			rest = append(rest, row)
		}
	}
	return append(hot, rest...)
}

func matches(row Row, filter string) bool {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return true
	}
	f := strings.ToLower(filter)
	if strings.Contains(strings.ToLower(row.Service.Name), f) {
		return true
	}
	return strings.Contains(strings.ToLower(row.Service.TailscaleLabel()), f)
}

// attention is a published service that is down or unhealthy.
func attention(row Row) bool {
	return row.Fresh && row.Service.Published() && !row.Report.OK
}

// needsRestart is a published process failure that restart can fix.
// Host approval and a port held by something else are not in this set.
func needsRestart(row Row) bool {
	if !row.Fresh || !row.Service.Published() {
		return false
	}
	return lifecycle.Decide(row.Service, row.Report, row.Owned).Action != lifecycle.ActionNone
}
