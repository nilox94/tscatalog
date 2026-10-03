package catalog

import "testing"

func TestExpandForRuntimeRequiresOrder(t *testing.T) {
	f := &File{
		Endpoint: DefaultEndpoint,
		Services: []Service{
			{Name: "momoma", Local: "http://127.0.0.1:5175", Requires: []string{"clickstack", "langfuse"}, Launch: Launch{{Start: []string{"x"}}}},
			{Name: "clickstack", Local: "http://127.0.0.1:8080", Launch: Launch{{Start: []string{"y"}}}},
			{Name: "langfuse", Local: "http://127.0.0.1:3000", Launch: Launch{{Start: []string{"z"}}}},
		},
	}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	got, err := f.ExpandForRuntime([]string{"momoma"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Name != "clickstack" || got[1].Name != "langfuse" || got[2].Name != "momoma" {
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
