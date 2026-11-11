//go:build !unix

package lifecycle

import "github.com/nilox94/tscatalog/internal/catalog"

func processStatus(s catalog.Service, index int, spec catalog.LaunchSpec) LaunchStatus {
	return LaunchStatus{
		Service: s.Name,
		ID:      rowID(s, index, spec),
		Index:   index,
		Shape:   ShapeProcess,
		State:   LaunchGone,
		Detail:  "detach unsupported",
	}
}
