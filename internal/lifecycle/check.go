package lifecycle

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"tscatalog/internal/catalog"
)

// Report is one service's local bind, local HTTP, and public HTTPS.
type Report struct {
	Name   string
	Bind   string
	Local  string
	Public string
	Host   string
	URL    string
	OK     bool
}

func Probe(ctx context.Context, s catalog.Service, publicBase, hostSummary string) Report {
	r := Report{
		Name: s.Name,
		Host: hostSummary,
		URL:  publicBase,
		OK:   true,
	}
	if r.Host == "" {
		r.Host = "-"
	}

	host, port, err := splitLocal(s.Local)
	if err != nil {
		r.Bind = "error"
		r.Local = err.Error()
		r.Public = "-"
		r.OK = false
		return r
	}
	if err := dialTCP(ctx, net.JoinHostPort(host, port)); err != nil {
		r.Bind = "down"
		r.OK = false
	} else {
		r.Bind = ":" + port
	}

	expect := s.Check.LocalExpect()
	if len(expect) == 0 {
		if r.Bind == "down" {
			r.Local = "down"
		} else {
			r.Local = "tcp"
		}
	} else {
		code, err := getStatus(ctx, catalog.JoinURL(s.Local, s.Check.LocalPath()), 2*time.Second)
		r.Local = formatHTTP(code, err)
		if !matchHTTP(code, err, expect) {
			r.OK = false
		}
	}

	if publicBase == "" {
		r.Public = "skip"
		return r
	}
	code, err := getStatus(ctx, catalog.JoinURL(publicBase, s.Check.LocalPath()), 8*time.Second)
	r.Public = formatHTTP(code, err)
	if !matchHTTP(code, err, s.Check.PublicExpect()) {
		r.OK = false
	}
	if strings.Contains(hostSummary, "not-approved") {
		r.OK = false
	}
	return r
}

func LocalReady(ctx context.Context, s catalog.Service) bool {
	r := Probe(ctx, s, "", "")
	if r.Bind == "down" || r.Bind == "error" {
		return false
	}
	expect := s.Check.LocalExpect()
	if len(expect) == 0 {
		return true
	}
	code, err := getStatus(ctx, catalog.JoinURL(s.Local, s.Check.LocalPath()), 2*time.Second)
	return matchHTTP(code, err, expect)
}

func splitLocal(local string) (string, string, error) {
	u, err := url.Parse(local)
	if err != nil {
		return "", "", err
	}
	return net.SplitHostPort(u.Host)
}

func dialTCP(ctx context.Context, addr string) error {
	d := net.Dialer{Timeout: time.Second}
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return c.Close()
}

func getStatus(ctx context.Context, raw string, timeout time.Duration) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return 0, err
	}
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			TLSHandshakeTimeout: timeout,
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	res.Body.Close()
	return res.StatusCode, nil
}

// ValueOK reports whether a probe cell is success.
// "tcp" is success. With an empty expect list, HTTP 2xx and 3xx are success.
func ValueOK(value string, expect []int) bool {
	switch value {
	case "down", "error", "timeout", "refused":
		return false
	case "tcp", "skip":
		return true
	}
	code, err := strconv.Atoi(value)
	if err != nil {
		return false
	}
	if len(expect) == 0 {
		return code >= 200 && code < 400
	}
	return matchHTTP(code, nil, expect)
}

// LocalHealthy reports whether the process is accepting the catalog's local check.
// Public HTTPS and host approval are ignored.
func LocalHealthy(s catalog.Service, rep Report) bool {
	switch rep.Bind {
	case "", "down", "error":
		return false
	}
	expect := s.Check.LocalExpect()
	if len(expect) == 0 {
		return rep.Local == "tcp"
	}
	return ValueOK(rep.Local, expect)
}

func matchHTTP(code int, err error, expect []int) bool {
	if err != nil || code == 0 {
		return false
	}
	return slices.Contains(expect, code)
}

func formatHTTP(code int, err error) string {
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "Timeout") || strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline"):
			return "timeout"
		case strings.Contains(msg, "connection refused"):
			return "refused"
		default:
			return "error"
		}
	}
	return strconv.Itoa(code)
}
