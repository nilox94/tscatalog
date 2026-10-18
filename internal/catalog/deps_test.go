package catalog

import "testing"

func TestExpandForRuntimeRequiresOrder(t *testing.T) {
	f := &File{
		Endpoint: DefaultEndpoint,
		Services: []Service{
			{Name: "app", Local: "http://127.0.0.1:8082", Requires: []string{"db", "cache"}, Launch: Launch{{Start: []string{"x"}}}},
			{Name: "db", Local: "http://127.0.0.1:8080", Launch: Launch{{Start: []string{"y"}}}},
			{Name: "cache", Local: "http://127.0.0.1:8081", Launch: Launch{{Start: []string{"z"}}}},
		},
	}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	got, err := f.ExpandForRuntime([]string{"app"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Name != "db" || got[1].Name != "cache" || got[2].Name != "app" {
		t.Fatalf("order = %v", names(got))
	}
}

func names(svcs []Service) []string {
	out := make([]string, len(svcs))
	for i, s := range svcs {
		out[i] = s.Name
	}
	return out
}
