package main

import (
	"bytes"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

//go:embed stub/*.php
var stubs embed.FS

func usage() {
	fmt.Fprintf(os.Stderr, `usage: elph [-v]

Elph is a static analysis tool for checking your PHP files.
It performs basic checks. For advanced checks, see PHPStan.

Flags:
  -v	show warnings

Elph is configured using an Elphfile,
which is located in the root of the PHP project
(usually at the same level as, for example, composer.json).

The format is as follows:
  - The Elphfile is divided into three sections (denoted by brackets: [SECTION]):
    Scan, Analyze, and Ignore.
  - Lines beginning with ‘#’ or blank lines are ignored.
  - The Scan section includes paths that are parsed.
  - If a line begins with ‘!’, paths prefixed with that value are ignored.
  - The Analyze section includes paths that are analyzed.
  - The Ignore section includes patterns of errors to ignore.
  - If a line is in parentheses, the pattern is considered a regular expression;
    otherwise, simple glob matching is used (where * matches any characters).

To find out the type of a variable at any given time,
the special comment can be used (recognized by Elph).
To find out the type of an expression, one would type:

    $a = /* expr */;
    #debugType $a

Note: Only a subset of expressions is supported,
mainly function calls or accessing class members.
`)
	os.Exit(2)
}

func main() {
	log.SetPrefix("elph: ")
	log.SetFlags(0)

	showWarn := flag.Bool("v", false, "show warnings")
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() > 0 {
		log.Fatalf("unknown command %q\n", flag.Arg(0))
	}

	warnOut := io.Discard
	if *showWarn {
		warnOut = os.Stderr
	}

	cfg, err := loadElphfile(".")
	if err != nil {
		log.Fatal(err)
	}

	parsePath(stubs, ".", nil, warnOut)

	toScan, ignored := cfg.paths()
	root := new(rootFS)
	for _, path := range toScan {
		parsePath(root, path, ignored, warnOut)
	}

	arbiter, err := cfg.prepareArbiter()
	if err != nil {
		log.Fatal(err)
	}

	allParsed := slices.Sorted(maps.Keys(parsedFiles))
	for _, name := range allParsed {
		matched := false
		for _, pattern := range cfg.Analyze {
			if strings.HasPrefix(name, pattern.Value) {
				matched = true
				break
			}
		}
		if matched {
			file := parsedFiles[name]
			Check(file, arbiter, warnOut)
		}
	}

	for _, p := range arbiter.patterns {
		if !p.fired {
			fmt.Printf("%s:%d: pattern not matched: %s\n", configFileName, p.def.Number, p.def.Value)
			hasErrors = true
		}
	}
	if hasErrors {
		os.Exit(1)
	}
}

type rootFS struct{}

func (rootFS) Open(name string) (fs.File, error) { return os.Open(name) }

var parsedFiles = make(map[string]*File)

func parsePath(fsys fs.FS, filename string, ignored []string, warnOut io.Writer) {
	f, err := fsys.Open(filename)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(warnOut, "%s: [WARN] path not found\n", filename)
			return
		}
		log.Fatal(err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		log.Fatal(err)
	}

	if fi.IsDir() {
		parseDir(fsys, filename, ignored, warnOut)
		return
	}

	data, err := io.ReadAll(f)
	if err != nil {
		log.Fatal(err)
	}
	f.Close()

	file, err := Parse(bytes.NewReader(data), filename, false, warnOut)
	if se, ok := err.(*SyntaxError); ok {
		log.Fatalf("%s:%d:%d: %v", filename, se.Line, se.Column, se.Err)
	} else if err != nil {
		log.Fatal(err)
	}
	file.Path = filename
	parsedFiles[filename] = file
}

func parseDir(fsys fs.FS, filename string, ignored []string, warnOut io.Writer) {
	err := fs.WalkDir(fsys, filename, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Fatal(err)
		}
		if d.IsDir() {
			for _, p := range ignored {
				if strings.HasPrefix(path, p) {
					return fs.SkipDir
				}
			}
			return nil
		}
		switch filepath.Ext(d.Name()) {
		default:
			return nil
		case ".php":
		}

		parsePath(fsys, path, ignored, warnOut)
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
}
