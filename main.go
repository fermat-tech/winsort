package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var progName string

// version is the release this source is; release builds stamp the same value
// with -ldflags "-X main.version=vX.Y.Z".
var version = "v1.0.1"

func init() {
	progName = strings.TrimSuffix(filepath.Base(os.Args[0]), filepath.Ext(os.Args[0]))
	rand.Seed(time.Now().UnixNano()) //nolint:staticcheck
}

// ── multi-value flag ──────────────────────────────────────────────────────────

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, " ") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

// ── key definition ────────────────────────────────────────────────────────────

type keyDef struct {
	f1, c1                    int // start: field (1-based), char offset (1-based; 0=beginning)
	f2, c2                    int // end: field (0=end of line), char offset (0=end of field)
	blanks, dict, fold        bool
	general, ignore           bool
	month, human              bool
	numeric, random, version  bool
	reverse                   bool
}

// ── global config ─────────────────────────────────────────────────────────────

type config struct {
	blanks, dict, fold        bool
	general, ignore           bool
	month, human              bool
	numeric, random, version  bool
	reverse                   bool
	check, checkQ             bool
	merge                     bool
	output                    string
	stable                    bool
	sep                       rune // 0 = whitespace-delimited
	unique                    bool
	zero                      bool
	keys                      []keyDef
}

var cfg config

// ── argument preprocessing ────────────────────────────────────────────────────

// expandArgs splits combined single-char flags (e.g. -rn → -r -n).
// Flags that consume the next argument or an attached value: k, o, t, T, S.
func expandArgs(args []string) []string {
	valueFlag := map[byte]bool{'k': true, 'o': true, 't': true, 'T': true, 'S': true}
	var out []string
	for _, a := range args {
		if a == "--" || !strings.HasPrefix(a, "-") || strings.HasPrefix(a, "--") || len(a) <= 2 {
			out = append(out, a)
			continue
		}
		flags := a[1:]
		var expanded []string
		consumed := false
		for j := 0; j < len(flags); j++ {
			ch := flags[j]
			if valueFlag[ch] {
				// rest of the cluster is the value: -tX → -t X
				expanded = append(expanded, "-"+string(ch))
				if j+1 < len(flags) {
					expanded = append(expanded, flags[j+1:])
				}
				consumed = true
				break
			}
			expanded = append(expanded, "-"+string(ch))
		}
		if consumed || len(expanded) > 1 {
			out = append(out, expanded...)
		} else {
			out = append(out, a) // single flag, pass through unchanged
		}
	}
	return out
}

// ── key parsing ───────────────────────────────────────────────────────────────

// parseKey parses a KEYDEF: POS1[,POS2]
// POS = F[.C][OPTS]  where OPTS ⊆ {b,d,f,g,h,i,M,n,R,r,V}
func parseKey(s string) (keyDef, error) {
	var k keyDef
	comma := strings.IndexByte(s, ',')
	var p1, p2 string
	if comma < 0 {
		p1 = s
	} else {
		p1 = s[:comma]
		p2 = s[comma+1:]
	}
	var err error
	k.f1, k.c1, err = parsePosOpts(p1, &k)
	if err != nil {
		return k, fmt.Errorf("invalid key %q: %v", s, err)
	}
	if p2 != "" {
		k.f2, k.c2, err = parsePosOpts(p2, &k)
		if err != nil {
			return k, fmt.Errorf("invalid key %q: %v", s, err)
		}
	}
	return k, nil
}

// parsePosOpts parses F[.C][OPTS], applies flag chars to *k, returns (field, char).
func parsePosOpts(s string, k *keyDef) (field, char int, err error) {
	i := 0
	for i < len(s) && ((s[i] >= '0' && s[i] <= '9') || s[i] == '.') {
		i++
	}
	numPart := s[:i]
	for _, c := range s[i:] {
		switch c {
		case 'b':
			k.blanks = true
		case 'd':
			k.dict = true
		case 'f':
			k.fold = true
		case 'g':
			k.general = true
		case 'h':
			k.human = true
		case 'i':
			k.ignore = true
		case 'M':
			k.month = true
		case 'n':
			k.numeric = true
		case 'R':
			k.random = true
		case 'r':
			k.reverse = true
		case 'V':
			k.version = true
		default:
			return 0, 0, fmt.Errorf("unknown option %q", c)
		}
	}
	dot := strings.IndexByte(numPart, '.')
	if dot < 0 {
		if numPart == "" {
			return 0, 0, fmt.Errorf("missing field number")
		}
		field, err = strconv.Atoi(numPart)
		return field, 0, err
	}
	field, err = strconv.Atoi(numPart[:dot])
	if err != nil {
		return 0, 0, fmt.Errorf("bad field number")
	}
	if dot+1 < len(numPart) {
		char, err = strconv.Atoi(numPart[dot+1:])
		if err != nil {
			return 0, 0, fmt.Errorf("bad char offset")
		}
	}
	return field, char, nil
}

// ── field splitting ───────────────────────────────────────────────────────────

// fieldSpans returns [start, end) byte offsets for each field in line.
// sep==0: fields are runs of non-whitespace (GNU sort default).
// sep!=0: fields split by the separator character.
func fieldSpans(line string, sep rune) [][2]int {
	if sep == 0 {
		var spans [][2]int
		inField := false
		start := 0
		for i := 0; i < len(line); {
			r, size := utf8.DecodeRuneInString(line[i:])
			blank := r == ' ' || r == '\t'
			if !inField && !blank {
				start = i
				inField = true
			} else if inField && blank {
				spans = append(spans, [2]int{start, i})
				inField = false
			}
			i += size
		}
		if inField {
			spans = append(spans, [2]int{start, len(line)})
		}
		return spans
	}
	sepStr := string(sep)
	var spans [][2]int
	base := 0
	for {
		idx := strings.Index(line[base:], sepStr)
		if idx < 0 {
			spans = append(spans, [2]int{base, len(line)})
			break
		}
		spans = append(spans, [2]int{base, base + idx})
		base += idx + len(sepStr)
	}
	return spans
}

// byteOffset returns the byte offset of the n-th rune (0-based) in s,
// clamped to len(s).
func byteOffset(s string, n int) int {
	i := 0
	for j := 0; j < n && i < len(s); j++ {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return i
}

// extractKeyStr returns the key substring from line for key k.
func extractKeyStr(line string, k keyDef) string {
	spans := fieldSpans(line, cfg.sep)
	n := len(spans)

	// Resolve start byte
	var startByte int
	if k.f1 < 1 || k.f1 > n {
		startByte = len(line)
	} else {
		sp := spans[k.f1-1]
		c1 := k.c1
		if c1 < 1 {
			c1 = 1
		}
		fieldStr := line[sp[0]:sp[1]]
		startByte = sp[0] + byteOffset(fieldStr, c1-1)
	}

	// Resolve end byte
	var endByte int
	if k.f2 == 0 {
		endByte = len(line)
	} else if k.f2 > n {
		endByte = len(line)
	} else {
		sp := spans[k.f2-1]
		if k.c2 == 0 {
			endByte = sp[1]
		} else {
			fieldStr := line[sp[0]:sp[1]]
			endByte = sp[0] + byteOffset(fieldStr, k.c2)
			if endByte > sp[1] {
				endByte = sp[1]
			}
		}
	}

	if startByte >= endByte || startByte < 0 {
		return ""
	}
	if endByte > len(line) {
		endByte = len(line)
	}
	return line[startByte:endByte]
}

// ── key transformation ────────────────────────────────────────────────────────

func applyTextFlags(s string, blanks, dict, fold, ignore bool) string {
	if blanks {
		s = strings.TrimLeft(s, " \t")
	}
	if !dict && !fold && !ignore {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if ignore && !unicode.IsPrint(r) {
			continue
		}
		if dict && !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != ' ' && r != '\t' {
			continue
		}
		if fold {
			r = unicode.ToUpper(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ── numeric / special comparisons ────────────────────────────────────────────

var monthMap = map[string]int{
	"JAN": 1, "FEB": 2, "MAR": 3, "APR": 4,
	"MAY": 5, "JUN": 6, "JUL": 7, "AUG": 8,
	"SEP": 9, "OCT": 10, "NOV": 11, "DEC": 12,
}

func monthValue(s string) int {
	s = strings.TrimLeft(s, " \t")
	if len(s) >= 3 {
		if v, ok := monthMap[strings.ToUpper(s[:3])]; ok {
			return v
		}
	}
	return 0
}

// parseLeadingFloat extracts a float from the leading numeric portion of s.
func parseLeadingFloat(s string) (float64, bool) {
	s = strings.TrimLeft(s, " \t")
	if s == "" {
		return 0, false
	}
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	}
	// allow exponent for -g
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j > i+1 {
			i = j
		}
	}
	if i == 0 {
		return 0, false
	}
	f, err := strconv.ParseFloat(s[:i], 64)
	return f, err == nil
}

// parseNumeric reads the leading number of s the way GNU sort -n does: blanks,
// an optional '-', digits, and an optional fraction. No '+' and no exponent --
// those are -g's -- so "+1" and "x" both count as zero, and "1e3" as 1.
func parseNumeric(s string) float64 {
	s = strings.TrimLeft(s, " \t")
	i := 0
	if i < len(s) && s[i] == '-' {
		i++
	}
	digits := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		digits++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			digits++
		}
	}
	if digits == 0 {
		return 0
	}
	f, _ := strconv.ParseFloat(s[:i], 64)
	return f
}

func parseGeneral(s string) float64 {
	s = strings.TrimLeft(s, " \t")
	up := strings.ToUpper(s)
	switch up {
	case "NAN", "+NAN", "-NAN":
		return math.NaN()
	case "INF", "+INF", "INFINITY", "+INFINITY":
		return math.Inf(1)
	case "-INF", "-INFINITY":
		return math.Inf(-1)
	}
	f, _ := parseLeadingFloat(s)
	return f
}

func parseHuman(s string) float64 {
	s = strings.TrimLeft(s, " \t")
	if s == "" {
		return 0
	}
	neg := false
	if s[0] == '-' {
		neg = true
		s = s[1:]
	} else if s[0] == '+' {
		s = s[1:]
	}
	mult := 1.0
	if len(s) > 0 {
		switch s[len(s)-1] {
		case 'K', 'k':
			mult = 1024
			s = s[:len(s)-1]
		case 'M':
			mult = 1024 * 1024
			s = s[:len(s)-1]
		case 'G':
			mult = 1024 * 1024 * 1024
			s = s[:len(s)-1]
		case 'T':
			mult = 1024 * 1024 * 1024 * 1024
			s = s[:len(s)-1]
		case 'P':
			mult = 1024 * 1024 * 1024 * 1024 * 1024
			s = s[:len(s)-1]
		case 'E':
			mult = 1024 * 1024 * 1024 * 1024 * 1024 * 1024
			s = s[:len(s)-1]
		}
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	if neg {
		f = -f
	}
	return f * mult
}

// parseVersion splits s into alternating string/int chunks for version comparison.
func versionChunks(s string) []any {
	var parts []any
	i := 0
	for i < len(s) {
		if s[i] >= '0' && s[i] <= '9' {
			j := i
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			n, _ := strconv.Atoi(s[i:j])
			parts = append(parts, n)
			i = j
		} else {
			j := i
			for j < len(s) && !(s[j] >= '0' && s[j] <= '9') {
				j++
			}
			parts = append(parts, s[i:j])
			i = j
		}
	}
	return parts
}

func cmpVersion(a, b string) int {
	ap := versionChunks(a)
	bp := versionChunks(b)
	lim := len(ap)
	if len(bp) < lim {
		lim = len(bp)
	}
	for i := 0; i < lim; i++ {
		ai, aInt := ap[i].(int)
		bi, bInt := bp[i].(int)
		if aInt && bInt {
			if ai != bi {
				return cmpInt(ai, bi)
			}
			continue
		}
		as, _ := ap[i].(string)
		bs, _ := bp[i].(string)
		if as != bs {
			if as < bs {
				return -1
			}
			return 1
		}
	}
	return cmpInt(len(ap), len(bp))
}

func cmpFloat(a, b float64) int {
	aNaN, bNaN := math.IsNaN(a), math.IsNaN(b)
	if aNaN && bNaN {
		return 0
	}
	if aNaN {
		return -1
	}
	if bNaN {
		return 1
	}
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// ── random sort support ───────────────────────────────────────────────────────

// randomValues maps each unique key to a stable random int64 for this run.
var randomValues = map[string]int64{}

func randomValue(s string) int64 {
	if v, ok := randomValues[s]; ok {
		return v
	}
	v := rand.Int63()
	randomValues[s] = v
	return v
}

// ── comparison engine ─────────────────────────────────────────────────────────

// hasOrdering reports whether a key carries any ordering option of its own.
func hasOrdering(k keyDef) bool {
	return k.blanks || k.dict || k.fold || k.general || k.ignore || k.month ||
		k.human || k.numeric || k.random || k.version || k.reverse
}

// mergeGlobal gives a key the global ordering options, GNU's way: all or
// nothing. A key with no ordering letters of its own takes every global one; a
// key with any takes none. So in `-k1,1n -r` the key stays ascending and only
// the last-resort comparison is reversed. ORing each global option into every
// key made that key descending too.
func mergeGlobal(k keyDef) keyDef {
	if hasOrdering(k) {
		return k
	}
	g := globalKey()
	g.f1, g.c1, g.f2, g.c2 = k.f1, k.c1, k.f2, k.c2
	return g
}

func globalKey() keyDef {
	return keyDef{
		blanks: cfg.blanks, dict: cfg.dict, fold: cfg.fold,
		general: cfg.general, ignore: cfg.ignore,
		month: cfg.month, human: cfg.human,
		numeric: cfg.numeric, random: cfg.random,
		version: cfg.version, reverse: cfg.reverse,
	}
}

// compareByKey compares two key strings (ka, kb extracted from lines a, b)
// according to the modifiers in k.
func compareByKey(ka, kb string, k keyDef) int {
	var c int
	switch {
	case k.random:
		if ka == kb {
			return 0
		}
		c = cmpInt64(randomValue(ka), randomValue(kb))
	case k.month:
		c = cmpInt(monthValue(ka), monthValue(kb))
	case k.human:
		c = cmpFloat(parseHuman(ka), parseHuman(kb))
	case k.general:
		c = cmpFloat(parseGeneral(ka), parseGeneral(kb))
	case k.numeric:
		c = cmpFloat(parseNumeric(ka), parseNumeric(kb))
	case k.version:
		c = cmpVersion(ka, kb)
	default:
		ta := applyTextFlags(ka, k.blanks, k.dict, k.fold, k.ignore)
		tb := applyTextFlags(kb, k.blanks, k.dict, k.fold, k.ignore)
		if ta < tb {
			c = -1
		} else if ta > tb {
			c = 1
		}
	}
	if k.reverse {
		return -c
	}
	return c
}

func cmpInt64(a, b int64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// compareLines is the top-level comparator used by sort.
func compareLines(a, b string) int {
	if len(cfg.keys) == 0 {
		// No -k: compare whole line with global flags.
		if c := compareByKey(a, b, globalKey()); c != 0 {
			return c
		}
	} else {
		for _, k := range cfg.keys {
			mk := mergeGlobal(k)
			ka := extractKeyStr(a, mk)
			kb := extractKeyStr(b, mk)
			if c := compareByKey(ka, kb, mk); c != 0 {
				return c
			}
		}
	}
	// Last resort, when every key ties: the whole lines byte by byte (unless
	// -s or -u). A global -r reverses it, as in GNU sort, so equal-key lines
	// come out descending under -r; a key's own r does not. It used to ignore
	// -r, so `sort -r -k5,5n` listed the lines of each equal key ascending.
	//
	// -u turns it off as -s does: "equal" then means equal keys, which is what
	// both the sort and the removal of duplicates go by -- the first line of
	// each equal run, in input order, is the one kept. With it on, lines whose
	// keys matched still differed as wholes, and `-u -k2,2` removed nothing.
	if !cfg.stable && !cfg.unique {
		c := strings.Compare(a, b)
		if cfg.reverse {
			return -c
		}
		return c
	}
	return 0
}

// ── sort / check / dedup ──────────────────────────────────────────────────────

func sortLines(lines []string) {
	sort.SliceStable(lines, func(i, j int) bool {
		return compareLines(lines[i], lines[j]) < 0
	})
}

func dedup(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	out := lines[:1]
	for _, l := range lines[1:] {
		if compareLines(out[len(out)-1], l) != 0 {
			out = append(out, l)
		}
	}
	return out
}

// checkSorted exits non-zero if lines are not in sorted order. The report is
// GNU's, "NAME: FILE:N: disorder: LINE", with FILE "-" for standard input and
// the line ended by the -z terminator as GNU ends it.
func checkSorted(lines []string, file string, term byte) {
	for i := 1; i < len(lines); i++ {
		c := compareLines(lines[i-1], lines[i])
		ok := c <= 0
		if cfg.unique {
			ok = c < 0
		}
		if !ok {
			if !cfg.checkQ {
				fmt.Fprintf(os.Stderr, "%s: %s:%d: disorder: %s%c",
					progName, file, i+1, lines[i], term)
			}
			os.Exit(1)
		}
	}
}

// ── I/O ───────────────────────────────────────────────────────────────────────

func readAllLines(files []string, term byte) ([]string, error) {
	if len(files) == 0 {
		return scanLines(os.Stdin, term)
	}
	var all []string
	for _, f := range files {
		var r io.Reader
		if f == "-" {
			r = os.Stdin
		} else {
			fh, err := os.Open(f)
			if err != nil {
				return nil, err
			}
			ls, err := scanLines(fh, term)
			fh.Close()
			if err != nil {
				return nil, err
			}
			all = append(all, ls...)
			continue
		}
		ls, err := scanLines(r, term)
		if err != nil {
			return nil, err
		}
		all = append(all, ls...)
	}
	return all, nil
}

func scanLines(r io.Reader, term byte) ([]string, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	if term == 0 {
		sc.Split(splitNul)
	}
	// default ScanLines handles \n and \r\n, stripping the terminator.
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, sc.Err()
}

func splitNul(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i, b := range data {
		if b == 0 {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// ── main ──────────────────────────────────────────────────────────────────────

func main() {
	os.Args = append(os.Args[:1], expandArgs(os.Args[1:])...)

	var keyFlags multiFlag
	var sepStr string

	flag.BoolVar(&cfg.blanks, "b", false, "ignore leading blanks")
	flag.BoolVar(&cfg.dict, "d", false, "consider only blanks and alphanumeric characters")
	flag.BoolVar(&cfg.fold, "f", false, "fold lower case to upper case characters")
	flag.BoolVar(&cfg.general, "g", false, "compare according to general numerical value")
	flag.BoolVar(&cfg.human, "h", false, "compare human readable numbers (e.g., 2K 1G)")
	flag.BoolVar(&cfg.ignore, "i", false, "consider only printable characters")
	flag.BoolVar(&cfg.month, "M", false, "compare (unknown) < 'JAN' < ... < 'DEC'")
	flag.BoolVar(&cfg.numeric, "n", false, "compare according to string numerical value")
	flag.BoolVar(&cfg.random, "R", false, "shuffle, but group identical keys")
	flag.BoolVar(&cfg.reverse, "r", false, "reverse the result of comparisons")
	flag.BoolVar(&cfg.version, "V", false, "natural sort of (version) numbers within text")
	flag.BoolVar(&cfg.check, "c", false, "check for sorted input; do not sort")
	flag.BoolVar(&cfg.checkQ, "C", false, "like -c, but do not report first bad line")
	flag.BoolVar(&cfg.merge, "m", false, "merge already sorted files")
	flag.StringVar(&cfg.output, "o", "", "write result to FILE instead of standard output")
	flag.BoolVar(&cfg.stable, "s", false, "stabilize sort by disabling last-resort comparison")
	flag.StringVar(&sepStr, "t", "", "use SEP instead of non-blank to blank transition")
	flag.BoolVar(&cfg.unique, "u", false, "output only the first of an equal run")
	flag.BoolVar(&cfg.zero, "z", false, "line delimiter is NUL, not newline")
	flag.Var(&keyFlags, "k", "sort via a key; KEYDEF gives location and type")
	var showVersion bool
	flag.BoolVar(&showVersion, "version", false, "output version information and exit")

	flag.Usage = func() {
		w := os.Stderr
		fmt.Fprintf(w, "Usage: %s [OPTION]... [FILE]...\n", progName)
		fmt.Fprintf(w, "Write sorted concatenation of all FILE(s) to standard output.\n")
		fmt.Fprintf(w, "With no FILE, or when FILE is -, read standard input.\n\n")
		fmt.Fprintf(w, "Ordering options (override for specific key with the same letter in KEYDEF):\n\n")
		fmt.Fprintf(w, "  -b    ignore leading blanks\n")
		fmt.Fprintf(w, "  -d    dictionary order: consider only blanks and alphanumerics\n")
		fmt.Fprintf(w, "  -f    fold lower case to upper case\n")
		fmt.Fprintf(w, "  -g    general numeric sort (handles scientific notation, NaN, Inf)\n")
		fmt.Fprintf(w, "  -h    human-readable numeric sort (e.g., 1K 2M 3G)\n")
		fmt.Fprintf(w, "  -i    ignore non-printable characters\n")
		fmt.Fprintf(w, "  -M    month sort: (unknown) < JAN < FEB < ... < DEC\n")
		fmt.Fprintf(w, "  -n    numeric sort\n")
		fmt.Fprintf(w, "  -R    random sort (shuffle, grouping identical keys)\n")
		fmt.Fprintf(w, "  -r    reverse the result of comparisons\n")
		fmt.Fprintf(w, "  -V    version (natural) sort\n\n")
		fmt.Fprintf(w, "Other options:\n\n")
		fmt.Fprintf(w, "  -c          check for sorted input; do not sort\n")
		fmt.Fprintf(w, "  -C          like -c, but do not report first bad line\n")
		fmt.Fprintf(w, "  -k KEYDEF   sort via a key; KEYDEF = POS1[,POS2], POS = F[.C][OPTS]\n")
		fmt.Fprintf(w, "  -m          merge already sorted files\n")
		fmt.Fprintf(w, "  -o FILE     write result to FILE instead of stdout\n")
		fmt.Fprintf(w, "  -s          stable sort (disable last-resort comparison)\n")
		fmt.Fprintf(w, "  -t SEP      use SEP as field separator (default: whitespace)\n")
		fmt.Fprintf(w, "  -u          with -c: check strict ordering; otherwise: deduplicate\n")
		fmt.Fprintf(w, "  -z          line delimiter is NUL, not newline\n")
		fmt.Fprintf(w, "  --version   output version information and exit\n\n")
		fmt.Fprintf(w, "KEYDEF examples:\n")
		fmt.Fprintf(w, "  -k2,2       sort by second field only\n")
		fmt.Fprintf(w, "  -k1.2,1.4   sort by characters 2-4 of field 1\n")
		fmt.Fprintf(w, "  -k3n        sort by field 3 numerically\n")
		fmt.Fprintf(w, "  -k1r -k2n   reverse sort by field 1, then numeric sort by field 2\n")
	}

	flag.Parse()

	if showVersion {
		fmt.Printf("%s %s\n", progName, version)
		return
	}

	if sepStr != "" {
		r, _ := utf8.DecodeRuneInString(sepStr)
		cfg.sep = r
	}

	for _, ks := range keyFlags {
		k, err := parseKey(ks)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", progName, err)
			os.Exit(2)
		}
		cfg.keys = append(cfg.keys, k)
	}

	term := byte('\n')
	if cfg.zero {
		term = 0
	}

	// -c and -C check one input, as in GNU sort.
	checking := cfg.check || cfg.checkQ
	if checking && flag.NArg() > 1 {
		opt := "c"
		if !cfg.check {
			opt = "C"
		}
		fmt.Fprintf(os.Stderr, "%s: extra operand '%s' not allowed with -%s\n", progName, flag.Arg(1), opt)
		os.Exit(2)
	}

	lines, err := readAllLines(flag.Args(), term)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", progName, err)
		os.Exit(1)
	}

	if checking {
		file := "-"
		if flag.NArg() == 1 {
			file = flag.Arg(0)
		}
		checkSorted(lines, file, term)
		return
	}

	sortLines(lines)

	if cfg.unique {
		lines = dedup(lines)
	}

	out := os.Stdout
	if cfg.output != "" {
		f, err := os.Create(cfg.output)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %s: %v\n", progName, cfg.output, err)
			os.Exit(1)
		}
		defer f.Close()
		out = f
	}

	w := bufio.NewWriter(out)
	for _, l := range lines {
		w.WriteString(l)
		w.WriteByte(term)
	}
	w.Flush()
}
