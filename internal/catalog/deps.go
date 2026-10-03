package catalog

import "fmt"

// Published reports whether this row gets VIP, Serve, advertise, and ACL grants.
func (s Service) Published() bool {
	return !s.Unpublished
}

// ExpandForRuntime returns services to start or probe, with each requires chain
// before its dependent. When names is empty, every catalog row is included in
// dependency order. Stop does not use this — only explicitly named services stop.
func (f *File) ExpandForRuntime(names []string) ([]Service, error) {
	if len(names) == 0 {
		return f.topoSort(f.Services)
	}
	byName := f.byName()
	var out []Service
	seen := map[string]struct{}{}
	var visit func(string) error
	visit = func(name string) error {
		if _, ok := seen[name]; ok {
			return nil
		}
		s, ok := byName[name]
		if !ok {
			return fmt.Errorf("unknown service %q", name)
		}
		for _, req := range s.Requires {
			if err := visit(req); err != nil {
				return err
			}
		}
		seen[name] = struct{}{}
		out = append(out, s)
		return nil
	}
	for _, name := range names {
		s, ok := f.Lookup(name)
		if !ok {
			return nil, fmt.Errorf("unknown service %q", name)
		}
		if err := visit(s.Name); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (f *File) byName() map[string]Service {
	m := make(map[string]Service, len(f.Services))
	for _, s := range f.Services {
		m[s.Name] = s
	}
	return m
}

func (f *File) topoSort(svcs []Service) ([]Service, error) {
	byName := f.byName()
	seen := map[string]struct{}{}
	var out []Service
	var visit func(string) error
	visit = func(name string) error {
		if _, ok := seen[name]; ok {
			return nil
		}
		s, ok := byName[name]
		if !ok {
			return fmt.Errorf("requires unknown service %q", name)
		}
		for _, req := range s.Requires {
			if err := visit(req); err != nil {
				return err
			}
		}
		seen[name] = struct{}{}
		out = append(out, s)
		return nil
	}
	for _, s := range svcs {
		if err := visit(s.Name); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func validateRequires(f *File) error {
	byName := f.byName()
	for _, s := range f.Services {
		for _, req := range s.Requires {
			if req == s.Name {
				return fmt.Errorf("service %q: requires itself", s.Name)
			}
			if _, ok := byName[req]; !ok {
				return fmt.Errorf("service %q: requires unknown service %q", s.Name, req)
			}
		}
	}
	visiting := map[string]bool{}
	done := map[string]bool{}
	var dfs func(string) error
	dfs = func(name string) error {
		if done[name] {
			return nil
		}
		if visiting[name] {
			return fmt.Errorf("service %q: requires cycle involving %q", name, name)
		}
		visiting[name] = true
		for _, req := range byName[name].Requires {
			if err := dfs(req); err != nil {
				return err
			}
		}
		visiting[name] = false
		done[name] = true
		return nil
	}
	for name := range byName {
		if err := dfs(name); err != nil {
			return err
		}
	}
	return nil
}
