package tui

import (
	"strings"

	"tscatalog/internal/style"
)

const (
	keyBarPlain    = "<r> Restart  <R> Restart all  <x> Stop  <l> Logs  </> Filter  <enter> Describe  <q> Quit"
	detailBarPlain = "<esc> Back  <q> Quit"
	logBarPlain    = "<f> Follow  </> Search  <tab> Name  <esc> Back"
)

func bind(key, label string) string {
	return style.Key("<"+key+">") + " " + label
}

func renderKeyBar() string {
	return strings.Join([]string{
		bind("r", "Restart"),
		bind("R", "Restart all"),
		bind("x", "Stop"),
		bind("l", "Logs"),
		bind("/", "Filter"),
		bind("enter", "Describe"),
		bind("q", "Quit"),
	}, "  ")
}

func renderDetailBar() string {
	return strings.Join([]string{
		bind("esc", "Back"),
		bind("q", "Quit"),
	}, "  ")
}

func renderLogBar() string {
	return strings.Join([]string{
		bind("f", "Follow"),
		bind("/", "Search"),
		bind("tab", "Name"),
		bind("esc", "Back"),
	}, "  ")
}
