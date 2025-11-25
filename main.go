package main

import (
	"io/fs"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func main() {
	log.SetPrefix("phpqc: ")
	log.SetFlags(0)

	parseFile("stub/DateTime.php")



	toAnalyze := []string{
	}

	allParsed := slices.Sorted(maps.Keys(parsedFiles))
	for _, name := range allParsed {
		matched := false
		for _, pattern := range toAnalyze {
			if strings.HasPrefix(name, pattern) {
				matched = true
				break
			}
		}
		if matched {
			file := parsedFiles[name]
			Check(file)
		}
	}
}

var parsedFiles = make(map[string]*File)

func parseFile(filename string) {
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
		parseDir(filename)
		return
	}

	file, err := Parse(f, filename, false)
	if se, ok := err.(*SyntaxError); ok {
		log.Fatalf("%s:%d:%d: %v", filename, se.Line, se.Column, se.Err)
	} else if err != nil {
		log.Fatal(err)
	}
	file.Path = filename
	parsedFiles[filename] = file
}

func parseDir(filename string) {
	err := filepath.WalkDir(filename, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Fatal(err)
		}
		if d.IsDir() {
			return nil
		}
		switch filepath.Ext(d.Name()) {
		default:
			return nil
		case ".php":
		}

		parseFile(path)
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
}
