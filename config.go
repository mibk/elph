package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const configFileName = "Elphfile"

type Line struct {
	Number int
	Value  string
}

type Config struct {
	Scan    []Line
	Analyze []Line
	Ignore  []Line
}

func loadElphfile(dir string) (*Config, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	var cfgData []byte
	for dir != "" {
		cfg := filepath.Join(dir, configFileName)
		cfgData, err = os.ReadFile(cfg)
		if os.IsNotExist(err) {
			old := dir
			dir = filepath.Dir(dir)
			if dir != old {
				continue
			}
			// Root.
			return nil, fmt.Errorf("no %s found; run `elph -h`", configFileName)
		}
		if err != nil {
			return nil, err
		}
		break
	}

	var cfg Config
	var section *[]Line
	lineNumber := 0
	for b := range bytes.Lines(cfgData) {
		lineNumber++
		line := strings.TrimSpace(string(b))
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := strings.TrimSpace(line[1 : len(line)-1])
			name = strings.ToLower(name)
			switch name {
			default:
				return nil, fmt.Errorf("unexpected %s section %q", configFileName, name)
			case "scan":
				section = &cfg.Scan
			case "analyze":
				section = &cfg.Analyze
			case "ignore":
				section = &cfg.Ignore
			}
			continue
		}
		if section == nil {
			return nil, fmt.Errorf("no %s section; start with [scan]", configFileName)
		}
		*section = append(*section, Line{lineNumber, line})
	}

	return &cfg, nil
}

func (c *Config) paths() (paths, ignored []string) {
	for _, line := range c.Scan {
		path := line.Value
		if path, ok := strings.CutPrefix(path, "!"); ok {
			path = strings.TrimSpace(path)
			ignored = append(ignored, path)
		} else {
			paths = append(paths, path)
		}
	}
	return paths, ignored
}

func (c *Config) prepareArbiter() (*Arbiter, error) {
	a := new(Arbiter)
	for _, p := range c.Ignore {
		def := p
		p := p.Value
		if !strings.HasPrefix(p, "(") {
			p = strings.ReplaceAll(p, "*", "\x1d")
			p = regexp.QuoteMeta(p)
			p = strings.ReplaceAll(p, "\x1d", ".*")
			p = "^" + p + "$"
		}

		rx, err := regexp.Compile(p)
		if err != nil {
			return nil, err
		}
		a.patterns = append(a.patterns, &pattern{def: def, Regexp: rx})
	}
	return a, nil
}

type Arbiter struct {
	patterns []*pattern
}

type pattern struct {
	def Line
	*regexp.Regexp
	matched bool
}

func (a *Arbiter) errorMatched(msg string) bool {
	// Note: Windows paths are not supported.
	if a == nil {
		return false
	}
	for _, p := range a.patterns {
		if p.MatchString(msg) {
			p.matched = true
			return true
		}
	}
	return false
}
