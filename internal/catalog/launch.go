package catalog

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Launch is one process spec or several (UI + API).
type Launch []LaunchSpec

// Kind is the catalog discriminator for one start slot.
type Kind string

const (
	KindProcess  Kind = "process"
	KindCompose  Kind = "compose"
	KindExternal Kind = "external"
)

// LaunchSpec is how this host starts one slot.
// start/stop/status/logs are argv (not a shell).
// Kind may be omitted: a strict `docker compose up` argv is compose,
// Detach selects a watched process, and anything else is an external helper.
type LaunchSpec struct {
	Kind     string   `yaml:"kind,omitempty"`
	Name     string   `yaml:"name,omitempty"`
	Cwd      string   `yaml:"cwd"`
	Start    []string `yaml:"start"`
	Stop     []string `yaml:"stop"`
	Detach   bool     `yaml:"detach"`
	Services []string `yaml:"services,omitempty"`
	Files    []string `yaml:"files,omitempty"`
	Status   []string `yaml:"status,omitempty"`
	Logs     []string `yaml:"logs,omitempty"`
}

var (
	launchNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`)
	composeSvcRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
)

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
	seen := map[string]struct{}{}
	for i, spec := range l {
		if err := argvElements(i, "start", spec.Start); err != nil {
			return err
		}
		if err := argvElements(i, "status", spec.Status); err != nil {
			return err
		}
		if err := argvElements(i, "logs", spec.Logs); err != nil {
			return err
		}
		kind, err := spec.ResolveKind()
		if err != nil {
			return fmt.Errorf("launch[%d]: %w", i, err)
		}
		if spec.Name != "" {
			if !launchNameRE.MatchString(spec.Name) || allDigits(spec.Name) {
				return fmt.Errorf("launch[%d]: name %q", i, spec.Name)
			}
			if _, ok := seen[spec.Name]; ok {
				return fmt.Errorf("launch[%d]: duplicate name %q", i, spec.Name)
			}
			seen[spec.Name] = struct{}{}
		}
		switch kind {
		case KindProcess:
			if len(spec.Start) == 0 {
				return fmt.Errorf("launch[%d]: start argv is required", i)
			}
			if err := rejectFields(i, "process", []fieldSet{
				{"services", len(spec.Services) > 0},
				{"files", len(spec.Files) > 0},
				{"status", len(spec.Status) > 0},
				{"logs", len(spec.Logs) > 0},
			}); err != nil {
				return err
			}
		case KindCompose:
			if spec.Kind == string(KindCompose) {
				if err := rejectFields(i, "compose", []fieldSet{
					{"start", len(spec.Start) > 0},
					{"stop", len(spec.Stop) > 0},
					{"status", len(spec.Status) > 0},
					{"logs", len(spec.Logs) > 0},
					{"detach", spec.Detach},
				}); err != nil {
					return err
				}
			} else if spec.Detach {
				return fmt.Errorf("launch[%d]: compose launch cannot set detach", i)
			}
			if err := validateComposeServices(i, spec.Services); err != nil {
				return err
			}
			if err := validateComposeFiles(i, spec.Files); err != nil {
				return err
			}
		case KindExternal:
			if len(spec.Start) == 0 {
				return fmt.Errorf("launch[%d]: start argv is required", i)
			}
			if spec.Detach {
				return fmt.Errorf("launch[%d]: external launch is not detached by tscatalog", i)
			}
			if err := rejectFields(i, "external", []fieldSet{
				{"services", len(spec.Services) > 0},
				{"files", len(spec.Files) > 0},
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

type fieldSet struct {
	name string
	set  bool
}

func rejectFields(i int, kind string, fields []fieldSet) error {
	for _, f := range fields {
		if f.set {
			return fmt.Errorf("launch[%d]: kind %s does not use %s", i, kind, f.name)
		}
	}
	return nil
}

func argvElements(i int, field string, argv []string) error {
	for _, part := range argv {
		if strings.TrimSpace(part) == "" {
			return fmt.Errorf("launch[%d]: empty %s argument", i, field)
		}
	}
	return nil
}

func validateComposeServices(i int, services []string) error {
	seen := map[string]struct{}{}
	for _, svc := range services {
		if svc == "" || !composeSvcRE.MatchString(svc) {
			return fmt.Errorf("launch[%d]: service %q", i, svc)
		}
		if _, ok := seen[svc]; ok {
			return fmt.Errorf("launch[%d]: duplicate service %q", i, svc)
		}
		seen[svc] = struct{}{}
	}
	return nil
}

func validateComposeFiles(i int, files []string) error {
	for _, f := range files {
		if strings.TrimSpace(f) == "" {
			return fmt.Errorf("launch[%d]: empty file", i)
		}
	}
	return nil
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ResolveKind returns the explicit kind, or infers one when Kind is empty.
// Compose is tested before detach, so a compose argv with detach is compose
// and then rejected by Validate.
func (s LaunchSpec) ResolveKind() (Kind, error) {
	switch Kind(s.Kind) {
	case "":
		if _, _, _, ok := composeUp(s.Start); ok {
			return KindCompose, nil
		}
		if s.Detach {
			return KindProcess, nil
		}
		return KindExternal, nil
	case KindProcess, KindCompose, KindExternal:
		return Kind(s.Kind), nil
	default:
		return "", fmt.Errorf("launch kind %q", s.Kind)
	}
}

// composeUp reports whether argv is a strict `docker compose up`.
// -d and --detach are ignored. -f/--file and -p/--project-name take the next
// token. Any other flag, a positional before up, a missing flag value, or a
// second project name is not compose. Positionals after up are service names.
func composeUp(argv []string) (services, files []string, project string, ok bool) {
	if len(argv) < 3 || argv[0] != "docker" || argv[1] != "compose" {
		return nil, nil, "", false
	}
	sawUp := false
	for i := 2; i < len(argv); i++ {
		tok := argv[i]
		switch tok {
		case "up":
			if sawUp {
				services = append(services, tok)
				continue
			}
			sawUp = true
		case "-d", "--detach":
		case "-f", "--file", "-p", "--project-name":
			if i+1 >= len(argv) {
				return nil, nil, "", false
			}
			i++
			val := argv[i]
			switch tok {
			case "-f", "--file":
				files = append(files, val)
			default:
				if project != "" {
					return nil, nil, "", false
				}
				project = val
			}
		default:
			if strings.HasPrefix(tok, "-") || !sawUp {
				return nil, nil, "", false
			}
			services = append(services, tok)
		}
	}
	if !sawUp {
		return nil, nil, "", false
	}
	return services, files, project, true
}

// ComposeSource is the compose working set for this spec.
// An explicit kind uses Files and Services and an empty project name.
// An inferred argv supplies files, services, and the project name.
func (s LaunchSpec) ComposeSource() (services, files []string, project string, err error) {
	kind, err := s.ResolveKind()
	if err != nil {
		return nil, nil, "", err
	}
	if kind != KindCompose {
		return nil, nil, "", fmt.Errorf("not compose")
	}
	if s.Kind == string(KindCompose) {
		return append([]string(nil), s.Services...), append([]string(nil), s.Files...), "", nil
	}
	services, files, project, ok := composeUp(s.Start)
	if !ok {
		return nil, nil, "", fmt.Errorf("not a compose argv")
	}
	return services, files, project, nil
}

// ID is the slot name, or the decimal index when Name is empty.
func (s LaunchSpec) ID(index int) string {
	if s.Name != "" {
		return s.Name
	}
	return strconv.Itoa(index)
}

// Find matches a slot by ID or by a decimal index.
// An empty id is the caller's error; Find still reports it as unknown.
func (l Launch) Find(id string) (int, LaunchSpec, error) {
	for i, spec := range l {
		if spec.ID(i) == id {
			return i, spec, nil
		}
	}
	if n, err := strconv.Atoi(id); err == nil && n >= 0 && n < len(l) && strconv.Itoa(n) == id {
		return n, l[n], nil
	}
	return 0, LaunchSpec{}, fmt.Errorf("unknown %q", id)
}

// Warnings is set only for an external helper missing status or logs.
// Validate does not call Warnings. Load still succeeds when those argv are omitted.
func (s LaunchSpec) Warnings() []string {
	kind, err := s.ResolveKind()
	if err != nil || kind != KindExternal {
		return nil
	}
	var out []string
	if len(s.Status) == 0 {
		out = append(out, "no status command; ps cannot tell if it is running")
	}
	if len(s.Logs) == 0 {
		out = append(out, "no logs command")
	}
	return out
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
