package main

import (
	"io"
	"testing"
)

func TestIconsAreOnUnlessTurnedOff(t *testing.T) {
	cases := map[string]bool{"": true, "--icons=false": false, "--icons=true": true}
	for arg, want := range cases {
		var args []string
		if arg != "" {
			args = []string{arg}
		}
		opts, err := parseOptions(args, io.Discard)
		if err != nil || opts.icons != want {
			t.Errorf("%q: icons = %v, err = %v, want %v", arg, opts.icons, err, want)
		}
	}
}
