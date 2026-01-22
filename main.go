package main

import (
	"flag"
	"io"
	"io/fs"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

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

	parsePath("stub/", nil, warnOut)
	toScan, ignored := cfg.paths()
	for _, path := range toScan {
		parsePath(path, ignored, warnOut)
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
			Check(file, warnOut)
		}
	}
}

var parsedFiles = make(map[string]*File)

func parsePath(filename string, ignored []string, warnOut io.Writer) {
	f, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		log.Fatal(err)
	}

	if fi.IsDir() {
		parseDir(filename, ignored, warnOut)
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

func parseDir(filename string, ignored []string, warnOut io.Writer) {
	err := filepath.WalkDir(filename, func(path string, d fs.DirEntry, err error) error {
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

		parsePath(path, ignored, warnOut)
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
}
