package main

import "testing"

func TestLynisStateReportsOnlyTransitions(t *testing.T) {
	var state lynisState

	if !state.update(true, "/usr/sbin/lynis") {
		t.Fatal("the first detection must be reported")
	}
	if state.update(true, "/usr/sbin/lynis") {
		t.Fatal("an unchanged detection must not be reported")
	}
	if !state.update(false, "") {
		t.Fatal("losing Lynis must be reported")
	}
	if state.update(false, "") {
		t.Fatal("staying absent must not be reported")
	}
	if !state.update(true, "/usr/bin/lynis") {
		t.Fatal("Lynis appearing at a different path must be reported")
	}
}
