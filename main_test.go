package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
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

// TestMain lets a test run the real main() in a child process, for behavior
// that ends in os.Exit: WINSORT_TEST_ARGS holds the arguments, separated by
// U+001F (an environment variable cannot hold a NUL).
func TestMain(m *testing.M) {
	if args, ok := os.LookupEnv("WINSORT_TEST_ARGS"); ok {
		os.Args = append(os.Args[:1], strings.Split(args, "\x1f")...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// run executes winsort with args and stdin, returning stdout, stderr and the
// exit code.
func run(t *testing.T, stdin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "WINSORT_TEST_ARGS="+strings.Join(args, "\x1f"))
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), errb.String(), code
}

// -n reads what GNU sort -n reads: an optional '-', digits and a fraction. A
// '+' or an exponent is not part of the number, so "+1" sorts as zero and
// "1e3" as 1. It used to take both, as -g does. Expectations are GNU sort
// 8.32's under LC_ALL=C.
func TestNumeric(t *testing.T) {
	in := []string{"1e3", "2", "-.5", ".5", "-", "-0", "0", "1.", "-x", "--1", " \t7", "00.10", ".1", "+1", "+5", "-1"}
	want := []string{"-1", "-.5", "+1", "+5", "-", "--1", "-0", "-x", "0", ".1", "00.10", ".5", "1.", "1e3", "2", " \t7"}
	got := sorted(t, in, nil, func() { cfg.numeric = true })
	if !reflect.DeepEqual(got, want) {
		t.Errorf("-n:\n got  %q\n want %q", got, want)
	}
	got = dedup(sorted(t, []string{"+1", "1", "x", "1.0"}, nil, func() { cfg.numeric = true; cfg.unique = true }))
	if want := []string{"+1", "1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("-u -n:\n got  %q\n want %q", got, want)
	}
}

// -c and -C report as GNU sort does, naming the file, and check one file only.
func TestCheckReport(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "in")
	if err := os.WriteFile(f, []byte("b\na\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, stdin string
		args        []string
		stderr      string
		code        int
	}{
		{"stdin", "b\na\n", []string{"-c"}, "%s: -:2: disorder: a\n", 1},
		{"dash", "b\na\n", []string{"-c", "-"}, "%s: -:2: disorder: a\n", 1},
		{"file", "", []string{"-c", f}, "%s: " + f + ":2: disorder: a\n", 1},
		{"-z", "b\x00a\x00", []string{"-c", "-z"}, "%s: -:2: disorder: a\x00", 1},
		{"key", "a b\na a\n", []string{"-c", "-k2,2"}, "%s: -:2: disorder: a a\n", 1},
		{"-u", "a\na\n", []string{"-c", "-u"}, "%s: -:2: disorder: a\n", 1},
		{"sorted", "a\nb\n", []string{"-c"}, "", 0},
		{"-C", "b\na\n", []string{"-C"}, "", 1},
		{"extra -c", "", []string{"-c", f, f}, "%s: extra operand '" + f + "' not allowed with -c\n", 2},
		{"extra -C", "", []string{"-C", f, f}, "%s: extra operand '" + f + "' not allowed with -C\n", 2},
	}
	for _, c := range cases {
		_, stderr, code := run(t, c.stdin, c.args...)
		want := c.stderr
		if want != "" {
			want = fmt.Sprintf(c.stderr, progName)
		}
		if stderr != want || code != c.code {
			t.Errorf("%s: got %q rc=%d, want %q rc=%d", c.name, stderr, code, want, c.code)
		}
	}
}

// Options may follow operands, as GNU getopt permutes them: expandArgs moves
// every option ahead of a "--" and keeps the operands in order. It used to
// pass the command line through, and the flag package stopped at the first
// operand, so `winsort f -r` read a file named "-r".
func TestPermute(t *testing.T) {
	cases := []struct {
		name  string
		posix bool
		in    []string
		want  []string
	}{
		{"trailing flag", false, []string{"f", "-r"}, []string{"-r", "--", "f"}},
		{"cluster", false, []string{"f", "-rk2,2", "g"}, []string{"-r", "-k", "2,2", "--", "f", "g"}},
		{"separate value", false, []string{"f", "-k", "2,2", "-n"}, []string{"-k", "2,2", "-n", "--", "f"}},
		{"value looks like a flag", false, []string{"f", "-t", "-", "-o", "-r"}, []string{"-t", "-", "-o", "-r", "--", "f"}},
		{"attached value", false, []string{"f", "-oout", "-t,"}, []string{"-o", "out", "-t", ",", "--", "f"}},
		{"dash is an operand", false, []string{"f", "-", "-r"}, []string{"-r", "--", "f", "-"}},
		{"double dash ends options", false, []string{"f", "--", "-r", "g"}, []string{"--", "f", "-r", "g"}},
		{"long option", false, []string{"f", "--version"}, []string{"--version", "--", "f"}},
		{"missing value", false, []string{"f", "-t"}, []string{"-t"}},
		{"POSIXLY_CORRECT stops at an operand", true, []string{"-n", "f", "-r"}, []string{"-n", "--", "f", "-r"}},
		{"nothing", false, nil, []string{"--"}},
	}
	for _, c := range cases {
		if c.posix {
			t.Setenv("POSIXLY_CORRECT", "1")
		} else {
			os.Unsetenv("POSIXLY_CORRECT")
		}
		if got := expandArgs(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: expandArgs(%q)\n got  %q\n want %q", c.name, c.in, got, c.want)
		}
	}
}

// End to end, against GNU sort 8.32's output for the same command lines.
func TestOptionsAfterOperands(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	if err := os.WriteFile(f, []byte("b 2\na 3\nc 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	cases := []struct {
		args   []string
		stdout string
		stderr string
		code   int
	}{
		{[]string{f, "-r"}, "c 1\nb 2\na 3\n", "", 0},
		{[]string{f, "-k", "2,2", "-n"}, "c 1\nb 2\na 3\n", "", 0},
		{[]string{f, "-rk2,2"}, "a 3\nb 2\nc 1\n", "", 0},
		{[]string{"-c", f, "-k2,2"}, "", "%s: " + f + ":3: disorder: c 1\n", 1},
		{[]string{f, "-o", out, "-r"}, "", "", 0},
	}
	for _, c := range cases {
		stdout, stderr, code := run(t, "", c.args...)
		want := c.stderr
		if want != "" {
			want = fmt.Sprintf(want, progName)
		}
		if stdout != c.stdout || stderr != want || code != c.code {
			t.Errorf("%q: got %q %q rc=%d, want %q %q rc=%d", c.args, stdout, stderr, code, c.stdout, want, c.code)
		}
	}
	if b, err := os.ReadFile(out); err != nil || string(b) != "c 1\nb 2\na 3\n" {
		t.Errorf("-o after the operand: got %q, %v", b, err)
	}
}

func TestVersion(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"--version", "nonexistent"}} {
		out, stderr, code := run(t, "", args...)
		if want := progName + " " + version + "\n"; out != want || stderr != "" || code != 0 {
			t.Errorf("%q: got %q %q rc=%d, want %q", args, out, stderr, code, want)
		}
	}
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
