# winsort

A faithful Windows port of Unix `sort` — all the standard options, correct behavior, and a rename-friendly binary.

Part of the [fermat-tech](https://github.com/fermat-tech) suite of Unix-style command-line tools for Windows (`winegrep`, `winfind`, `winls`, `winwc`, `winwhich`, `winless`, `winheadtail`).

---

## About

`winsort` sorts lines of text from files or stdin and writes the result to stdout, matching the behavior of GNU `sort`. It supports numeric, human-readable, month, version, and general-numeric sort modes; multi-key (`-k`) sorting with per-key flags; field separators; duplicate removal; sorted-order checking; NUL-terminated lines; and stable sort. Rename the `.exe` — the tool adopts the new name in all prompts and error messages automatically.

---

## Installation

### Option 1 — Download the release binary

1. Go to the [Releases](https://github.com/fermat-tech/winsort/releases) page.
2. Download `winsort.exe` from the latest release.
3. Place it somewhere on your `PATH` (e.g. `C:\Windows\System32` or a personal `bin\` folder).

### Option 2 — Install with `go install`

Requires Go 1.21 or later.

```powershell
go install github.com/fermat-tech/winsort@latest
```

The binary is placed in `%GOPATH%\bin` (typically `%USERPROFILE%\go\bin`), which should already be on your `PATH` if you have Go installed.

---

## Usage

```
winsort [OPTION]... [FILE]...
```

Write sorted concatenation of all FILE(s) to standard output.  
With no FILE, or when FILE is `-`, read standard input.

### Ordering options

| Flag | Description |
|------|-------------|
| `-b` | Ignore leading blanks |
| `-d` | Dictionary order — consider only blanks and alphanumerics |
| `-f` | Fold lower case to upper case (case-insensitive sort) |
| `-g` | General numeric — handles scientific notation, `NaN`, `Inf` |
| `-h` | Human-readable numeric — `1K`, `2M`, `1G`, etc. (1024-based) |
| `-i` | Ignore non-printable characters |
| `-M` | Month sort — `(unknown) < JAN < FEB < … < DEC` |
| `-n` | Numeric sort |
| `-R` | Random shuffle (identical keys are grouped) |
| `-r` | Reverse the result of comparisons |
| `-V` | Version / natural sort — `v1.9 < v1.10 < v2.0` |

### Other options

| Flag | Description |
|------|-------------|
| `-c` | Check for sorted input; do not sort (exit 1 if disordered) |
| `-C` | Like `-c`, but do not report the first disordered line |
| `-k KEYDEF` | Sort via a key (see **Key definitions** below) |
| `-m` | Merge already-sorted files |
| `-o FILE` | Write result to FILE instead of stdout |
| `-s` | Stable sort — disable last-resort comparison |
| `-t SEP` | Use SEP as the field separator (default: whitespace) |
| `-u` | With `-c`: check strict ordering; otherwise: output only the first of each equal run |
| `-z` | Line delimiter is NUL, not newline |

### Key definitions

```
-k KEYDEF     KEYDEF = POS1[,POS2]
              POS    = F[.C][OPTS]
              F      = field number (1-based)
              C      = character offset within field (1-based)
              OPTS   = subset of b d f g h i M n R r V
```

A key takes the global ordering options only if it has none of its own: as in
GNU `sort`, a key with any ordering letter (`-k3,3n`) uses just its own, and
ignores every global one. So in `winsort -k1,1n -r` the key stays ascending.

When every key compares equal, lines are compared as wholes, byte by byte — the
last-resort comparison. A global `-r` reverses that too, so under
`winsort -r -k5,5n` lines with the same key come out in descending order; a
key's own `r` does not. `-s` turns the last-resort comparison off, keeping
equal-key lines in input order, and so does `-u`: lines are equal when their
keys are, and `-u` keeps the first of each equal run in input order —
`winsort -u -k2,2` leaves one line per distinct field 2. Before 1.0.1 the last
resort ignored `-r`, global options were added to every key, and `-u` compared
whole lines, so with a key it removed nothing.

**Examples:**

```powershell
# Sort by second field only
winsort -k2,2

# Sort by characters 2–4 of field 1
winsort -k1.2,1.4

# Numeric sort on field 3
winsort -k3n

# Reverse sort by field 1, then numeric sort by field 2
winsort -k1r -k2n

# Sort CSV by third column numerically
winsort -t, -k3,3n data.csv
```

### Examples

```powershell
# Basic alphabetic sort
winsort words.txt

# Numeric sort
echo "10`n9`n100`n2" | winsort -n

# Sort and remove duplicates
winsort -u names.txt

# Human-readable sizes (e.g. du output)
du -h * | winsort -h

# Version sort
echo "v1.10`nv1.9`nv2.0`nv1.2" | winsort -V

# Month sort
echo "DEC`nJAN`nMAR`nFEB" | winsort -M

# Check if a file is already sorted
winsort -c sorted.txt && echo "OK"

# Sort, write output to a file (safe even if input == output)
winsort -o output.txt input.txt

# NUL-delimited (e.g. find -print0 output)
winfind . -name "*.go" -print0 | winsort -z
```

---

## Building from source

```powershell
git clone https://github.com/fermat-tech/winsort.git
cd winsort
go build -o winsort.exe .
```

Requires Go 1.21+. No external dependencies.

---

## License

MIT
