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

type Config struct {
	Scan    []string
	Analyze []string
	Ignore  []string
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
			return nil, fmt.Errorf("no %[1]s found; run `elph help %[1]s`", configFileName)
		}
		if err != nil {
			return nil, err
		}
		break
	}

	var cfg Config
	var section *[]string
	for b := range bytes.Lines(cfgData) {
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
		*section = append(*section, line)
	}

	return &cfg, nil
}

func (c *Config) paths() (paths, ignored []string) {
	for _, path := range c.Scan {
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
		if !strings.HasPrefix(p, "(") {
			p = strings.ReplaceAll(p, "*", "\x1d")
			p = regexp.QuoteMeta(p)
			p = strings.ReplaceAll(p, "\x1d", ".*")
		}

		rx, err := regexp.Compile(p)
		if err != nil {
			return nil, err
		}
		a.patterns = append(a.patterns, rx)
	}
	return a, nil
}

type Arbiter struct {
	patterns []*regexp.Regexp
}

func (a *Arbiter) errorMatched(msg string) bool {
	// TODO: Support Windows paths.
	if a == nil {
		return false
	}
	for _, p := range a.patterns {
		if p.MatchString(msg) {
			return true
		}
	}
	return false
}
