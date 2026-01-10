package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"
	"rsc.io/diff"
)

func Test(t *testing.T) {
	stubFiles, err := filepath.Glob("stub/*.php")
	if err != nil {
		t.Fatal(err)
	}

	var stubs []txtar.File
	for _, name := range stubFiles {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		stubs = append(stubs, txtar.File{Name: name, Data: b})
	}

	files, err := filepath.Glob("testdata/*.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, script := range files {
		name := strings.TrimSuffix(filepath.Base(script), ".txt")
		t.Run(name, func(t *testing.T) {
			defer func() {
				if t.Failed() {
					t.Log("\nfailed at", script, "\n")
				}
			}()

			a, err := txtar.ParseFile(script)
			if err != nil {
				t.Fatal(err)
			}

			clear(universe.types)

			var got, want strings.Builder

			parsed := make(map[string]*File)
			for _, f := range stubs {
				parsed[f.Name] = parseTestFile(t, f, &got)
			}
			for _, f := range a.Files {
				parsed[f.Name] = parseTestFile(t, f, &got)
			}

			l := linter{
				stdout:           &got,
				scope:            make(map[string]Ident),
				fileBeingChecked: "<test-line>",
			}

			for line := range strings.Lines(string(a.Comment)) {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "#") || line == "" {
					want.WriteString(line + "\n")
					got.WriteString(line + "\n")
					continue
				}

				want.WriteString(line + "\n")
				if arg, ok := strings.CutPrefix(line, "% check "); ok {
					got.WriteString(line + "\n")
					f := parsed[strings.TrimSpace(arg)]
					if f == nil {
						t.Fatalf("%s: no such file", arg)
					}
					l.check(f)
				}
			}
			if got, want := got.String(), want.String(); got != want {
				t.Errorf("lines don't match: (-extra +missing)\n%s", diff.Format(got, want))
			}
		})
	}
}

func parseTestFile(t *testing.T, f txtar.File, warnOut io.Writer) *File {
	file, err := parsePHP(bytes.NewReader(f.Data), f.Name, false, warnOut)
	if se, ok := err.(*SyntaxError); ok {
		t.Fatalf("%s:%d:%d: %v", f.Name, se.Line, se.Column, se.Err)
	} else if err != nil {
		t.Fatal(err)
	}
	file.Path = f.Name
	return file
}
