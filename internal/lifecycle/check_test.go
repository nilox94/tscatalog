package lifecycle

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"tscatalog/internal/catalog"
)

func TestFormatHTTP(t *testing.T) {
	if formatHTTP(200, nil) != "200" {
		t.Fatal(formatHTTP(200, nil))
	}
	if formatHTTP(0, context.DeadlineExceeded) != "timeout" {
		t.Fatal(formatHTTP(0, context.DeadlineExceeded))
	}
}

func TestMatchHTTP(t *testing.T) {
	if !matchHTTP(401, nil, []int{401, 200}) {
		t.Fatal("401 should match gateway")
	}
	if matchHTTP(502, nil, []int{200}) {
		t.Fatal("502 is process-down")
	}
}

func TestProbeLocalHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	s := catalog.Service{
		Name:  "web",
		Local: "http://" + u.Host,
		Check: catalog.Check{Path: "/health", Expect: []int{200}, Public: []int{200}},
	}
	r := Probe(context.Background(), s, "", "")
	if !r.OK || r.Local != "200" {
		t.Fatalf("%+v", r)
	}
}

func TestProbeDown(t *testing.T) {
	s := catalog.Service{
		Name:  "web",
		Local: "http://127.0.0.1:1",
	}
	r := Probe(context.Background(), s, "", "")
	if r.OK || r.Bind != "down" {
		t.Fatalf("%+v", r)
	}
}
