package main

import (
	"bytes"
	"testing"
)

func TestLogsFollowDoesNotRedefineFileShorthand(t *testing.T) {
	root := newRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"logs", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	logs, _, err := root.Find([]string{"logs"})
	if err != nil {
		t.Fatal(err)
	}
	follow := logs.Flags().Lookup("follow")
	if follow == nil {
		t.Fatal("logs has no --follow flag")
	}
	if follow.Shorthand != "" {
		t.Fatalf("follow shorthand = %q, want none so -f stays the catalog file", follow.Shorthand)
	}
	if !bytes.Contains(buf.Bytes(), []byte("--follow")) {
		t.Fatalf("help missing --follow:\n%s", buf.String())
	}
	if bytes.Contains(buf.Bytes(), []byte("-f, --follow")) || bytes.Contains(buf.Bytes(), []byte("-f --follow")) {
		t.Fatalf("help still shows -f as follow:\n%s", buf.String())
	}
}
