package main

import (
	"reflect"
	"testing"
)

// sorted runs sortLines on a copy of lines under the configuration set by
// setup, starting from a clean config each time.
func sorted(t *testing.T, lines []string, keys []string, setup func()) []string {
	t.Helper()
	cfg = config{}
	if setup != nil {
		setup()
	}
	for _, ks := range keys {
		k, err := parseKey(ks)
		if err != nil {
			t.Fatalf("parseKey(%q): %v", ks, err)
		}
		cfg.keys = append(cfg.keys, k)
	}
	out := append([]string(nil), lines...)
	sortLines(out)
	return out
}

// -u keeps the first line of each run of equal keys, in input order: equality
// is the keys' alone, with no last-resort comparison. It used to compare whole
// lines, so with a key nothing was ever removed. Expectations are GNU sort
// 8.32's under LC_ALL=C.
func TestUniqueByKey(t *testing.T) {
	letters := []string{"b 1", "a 1", "c 2", "B 1", "A 2"}
	folded := []string{"a", "A", "b", "a", "B"}
	multi := []string{"x 1 p", "y 1 p", "z 1 q", "w 2 p"}
	unique := func(extra func()) func() {
		return func() {
			cfg.unique = true
			if extra != nil {
				extra()
			}
		}
	}

	cases := []struct {
		name  string
		lines []string
		keys  []string
		setup func()
		want  []string
	}{
		{"-u -k2,2", letters, []string{"2,2"}, unique(nil), []string{"b 1", "c 2"}},
		{"-u -r -k2,2", letters, []string{"2,2"}, unique(func() { cfg.reverse = true }), []string{"c 2", "b 1"}},
		{"-u -k2,2r", letters, []string{"2,2r"}, unique(nil), []string{"c 2", "b 1"}},
		{"-u -s -k2,2", letters, []string{"2,2"}, unique(func() { cfg.stable = true }), []string{"b 1", "c 2"}},
		{"-u", letters, nil, unique(nil), []string{"A 2", "B 1", "a 1", "b 1", "c 2"}},
		{"-u -f", folded, nil, unique(func() { cfg.fold = true }), []string{"a", "b"}},
		{"-u -f -r", folded, nil, unique(func() { cfg.fold = true; cfg.reverse = true }), []string{"b", "a"}},
		{"-u -k2,2 -k3,3", multi, []string{"2,2", "3,3"}, unique(nil), []string{"x 1 p", "z 1 q", "w 2 p"}},
		{"-u -k2,2", multi, []string{"2,2"}, unique(nil), []string{"x 1 p", "w 2 p"}},
	}
	for _, c := range cases {
		got := dedup(sorted(t, c.lines, c.keys, c.setup))
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got  %q\n want %q", c.name, got, c.want)
		}
	}
}

// Ordering when keys tie, and which -r applies where. Every expectation is
// GNU sort 8.32's under LC_ALL=C, the byte order winsort uses.
func TestReverseAndLastResort(t *testing.T) {
	letters := []string{"b 1", "a 1", "c 2", "B 1", "A 2"}
	numbers := []string{"3 x", "3 a", "1 z", "3 m", "2 q"}
	times := []string{"k 07:28 z", "k 07:04 a", "k 07:30 m"}
	reverse := func() { cfg.reverse = true }

	cases := []struct {
		name  string
		lines []string
		keys  []string
		setup func()
		want  []string
	}{
		{"-k2,2", letters, []string{"2,2"}, nil,
			[]string{"B 1", "a 1", "b 1", "A 2", "c 2"}},
		// A global -r reverses the key and the last-resort comparison.
		{"-r -k2,2", letters, []string{"2,2"}, reverse,
			[]string{"c 2", "A 2", "b 1", "a 1", "B 1"}},
		// A key's own r reverses the key only.
		{"-k2,2r", letters, []string{"2,2r"}, nil,
			[]string{"A 2", "c 2", "B 1", "a 1", "b 1"}},
		// -s has no last-resort comparison; equal keys keep input order.
		{"-s -r -k2,2", letters, []string{"2,2"}, func() { cfg.stable = true; cfg.reverse = true },
			[]string{"c 2", "A 2", "b 1", "a 1", "B 1"}},
		{"-n -k1,1", numbers, []string{"1,1"}, func() { cfg.numeric = true },
			[]string{"1 z", "2 q", "3 a", "3 m", "3 x"}},
		{"-r -n -k1,1", numbers, []string{"1,1"}, func() { cfg.numeric = true; cfg.reverse = true },
			[]string{"3 x", "3 m", "3 a", "2 q", "1 z"}},
		{"-k1,1nr", numbers, []string{"1,1nr"}, nil,
			[]string{"3 a", "3 m", "3 x", "2 q", "1 z"}},
		// A key with options of its own takes none of the global ones: the key
		// stays ascending, and the global -r reaches only the last resort.
		{"-k1,1n -r", numbers, []string{"1,1n"}, reverse,
			[]string{"1 z", "2 q", "3 x", "3 m", "3 a"}},
		{"-s -k1,1nr", numbers, []string{"1,1nr"}, func() { cfg.stable = true },
			[]string{"3 x", "3 a", "3 m", "2 q", "1 z"}},
		// HH:MM read numerically is its hour, so these all tie and the
		// reversed last resort decides.
		{"-r -k2 -n", times, []string{"2"}, func() { cfg.numeric = true; cfg.reverse = true },
			[]string{"k 07:30 m", "k 07:28 z", "k 07:04 a"}},
		{"-r -f -k2,2", letters, []string{"2,2"}, func() { cfg.fold = true; cfg.reverse = true },
			[]string{"c 2", "A 2", "b 1", "a 1", "B 1"}},
		{"-f -k2,2", letters, []string{"2,2"}, func() { cfg.fold = true },
			[]string{"B 1", "a 1", "b 1", "A 2", "c 2"}},
		{"-r", letters, nil, reverse,
			[]string{"c 2", "b 1", "a 1", "B 1", "A 2"}},
		{"-r -n", numbers, nil, func() { cfg.numeric = true; cfg.reverse = true },
			[]string{"3 x", "3 m", "3 a", "2 q", "1 z"}},
	}
	for _, c := range cases {
		if got := sorted(t, c.lines, c.keys, c.setup); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got  %q\n want %q", c.name, got, c.want)
		}
	}
}
