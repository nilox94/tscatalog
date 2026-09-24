package catalog

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Launch is one process spec or several (Momoma UI + API).
type Launch []LaunchSpec

// LaunchSpec is how this host starts a process.
// start/stop are argv (not a shell). If Detach is set, start is backgrounded
// and Stop (or a SIGTERM to the pidfile) brings it down.
type LaunchSpec struct {
	Cwd    string   `yaml:"cwd"`
	Start  []string `yaml:"start"`
	Stop   []string `yaml:"stop"`
	Detach bool     `yaml:"detach"`
}

func (l *Launch) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || value.Tag == "!!null" {
		*l = nil
		return nil
	}
	switch value.Kind {
	case yaml.MappingNode:
		var spec LaunchSpec
		if err := value.Decode(&spec); err != nil {
			return err
		}
		*l = Launch{spec}
		return nil
	case yaml.SequenceNode:
		var specs []LaunchSpec
		if err := value.Decode(&specs); err != nil {
			return err
		}
		*l = specs
		return nil
	case yaml.ScalarNode:
		if value.Value == "" {
			*l = nil
			return nil
		}
	}
	return fmt.Errorf("launch: expected a mapping or a list of mappings")
}

func (l Launch) Validate() error {
	for i, spec := range l {
		if len(spec.Start) == 0 {
			return fmt.Errorf("launch[%d]: start argv is required", i)
		}
		for _, part := range spec.Start {
			if strings.TrimSpace(part) == "" {
				return fmt.Errorf("launch[%d]: empty start argument", i)
			}
		}
	}
	return nil
}

func (l Launch) Defined() bool {
	return len(l) > 0
}

// Check is optional HTTP probing on top of a TCP bind check.
type Check struct {
	Path   string `yaml:"path"`
	Expect []int  `yaml:"expect"`
	Public []int  `yaml:"public"`
}

func (c Check) LocalPath() string {
	if c.Path == "" {
		return "/"
	}
	if !strings.HasPrefix(c.Path, "/") {
		return "/" + c.Path
	}
	return c.Path
}

func (c Check) LocalExpect() []int {
	if len(c.Expect) > 0 {
		return c.Expect
	}
	if c.Path != "" {
		return []int{200}
	}
	return nil
}

func (c Check) PublicExpect() []int {
	if len(c.Public) > 0 {
		return c.Public
	}
	return []int{200, 204, 301, 302, 303, 307, 308}
}

func JoinURL(base, path string) string {
	return strings.TrimRight(base, "/") + path
}
