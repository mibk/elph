package main

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func cmdInit() {
	if _, err := os.Stat("Elphfile"); err == nil {
		log.Fatal("Elphfile already exists")
	}

	candidates := []string{"app/", "src/", "lib/"}
	var srcDirs []string
	for _, dir := range candidates {
		if containsPHP(dir) {
			srcDirs = append(srcDirs, dir)
		}
	}

	hasVendor := isDir("vendor/")

	var scan, analyze []string
	for _, d := range srcDirs {
		scan = append(scan, d)
		analyze = append(analyze, d)
	}
	if hasVendor {
		scan = append(scan, "vendor/")
	}

	var b strings.Builder
	b.WriteString("[Scan]\n")
	for _, s := range scan {
		b.WriteString(s)
		b.WriteByte('\n')
	}
	b.WriteString("\n[Analyze]\n")
	for _, a := range analyze {
		b.WriteString(a)
		b.WriteByte('\n')
	}
	b.WriteString("\n[Ignore]\n")

	if err := os.WriteFile("Elphfile", []byte(b.String()), 0644); err != nil {
		log.Fatal(err)
	}

	if len(srcDirs) == 0 {
		fmt.Println("no PHP directories found")
	}
	fmt.Println("created Elphfile")
}

func containsPHP(dir string) bool {
	if !isDir(dir) {
		return false
	}
	found := false
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(d.Name()) == ".php" {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
