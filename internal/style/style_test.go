package style

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
)

func TestProbePadsBeforeColor(t *testing.T) {
	var buf bytes.Buffer
	w := &colorprofile.Writer{Forward: &buf, Profile: colorprofile.ANSI}
	fmt.Fprint(w, Probe(8, "down", false))
	if !strings.Contains(buf.String(), "down    ") {
		t.Fatalf("padded word should stay contiguous, got %q", buf.String())
	}
	if !strings.Contains(buf.String(), "\x1b") {
		t.Fatal("expected ANSI on an ANSI profile")
	}
}

func TestProbeEmphFaintWhite(t *testing.T) {
	var buf bytes.Buffer
	w := &colorprofile.Writer{Forward: &buf, Profile: colorprofile.ANSI}
	fmt.Fprint(w, ProbeEmph(8, "-", false))
	out := buf.String()
	if strings.Contains(out, "\x1b[2m") {
		t.Fatalf("selected faint cell should not stay dim, got %q", out)
	}
	if !strings.Contains(out, "\x1b[1;") {
		t.Fatalf("selected faint cell should be bold, got %q", out)
	}
}

func TestProbeEmphBold(t *testing.T) {
	var buf bytes.Buffer
	w := &colorprofile.Writer{Forward: &buf, Profile: colorprofile.ANSI}
	fmt.Fprint(w, ProbeEmph(8, "up", true))
	if !strings.Contains(buf.String(), "\x1b[1;") {
		t.Fatalf("emph probe should be bold, got %q", buf.String())
	}
}

func TestWriterStripsBuffer(t *testing.T) {
	var buf bytes.Buffer
	w := colorprofile.NewWriter(&buf, []string{"TERM=dumb"})
	fmt.Fprintln(w, Action("create"))
	fmt.Fprintln(w, Host("approved / ready; not-approved / draining"))
	out := buf.String()
	if strings.Contains(out, "\x1b") {
		t.Fatalf("buffer should be plain, got %q", out)
	}
	for _, want := range []string{"create", "approved / ready", "not-approved / draining"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}
