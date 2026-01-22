package main

import (
	"embed"
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

func main() {
	log.SetPrefix("elph: ")
	log.SetFlags(0)

	ignoreWarn := flag.Bool("W", false, "ignore warnings")
	flag.Parse()

	var warnOut io.Writer = os.Stderr
	if *ignoreWarn {
		warnOut = io.Discard
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
			if strings.HasPrefix(name, pattern) {
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
			fmt.Printf("[ERROR] pattern not matched: %s\n", p.def)
		}
	}
}

type rootFS struct{}

func (rootFS) Open(name string) (fs.File, error) { return os.Open(name) }

var parsedFiles = make(map[string]*File)

func parsePath(fsys fs.FS, filename string, ignored []string, warnOut io.Writer) {
	f, err := fsys.Open(filename)
	if err != nil {
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

	file, err := Parse(f, filename, false, warnOut)
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
