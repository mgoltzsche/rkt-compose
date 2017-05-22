package model

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var toIdRegexp = regexp.MustCompile("[^a-z0-9]+")

func assertTrue(e bool, msg, location string) {
	if !e {
		panic(fmt.Sprintf("%s: %s", location, msg))
	}
}

func fileExists(filePath string) bool {
	if _, err := os.Stat(filepath.FromSlash(filePath)); err != nil {
		if os.IsNotExist(err) {
			return false
		} else {
			panic(err)
		}
	}
	return true
}

func isDirectory(file string) bool {
	s, e := os.Stat(filepath.FromSlash(file))
	panicOnError(e)
	return s.IsDir()
}

func panicOnError(e error) {
	if e != nil {
		panic(e)
	}
}

func toId(v string) string {
	return strings.Trim(toIdRegexp.ReplaceAllLiteralString(strings.ToLower(v), "-"), "-")
}

func absPath(p, basePath string) string {
	if len(p) > 0 && p[0:1] == "/" {
		return path.Clean(p)
	} else {
		return path.Join(path.Dir(basePath), p)
	}
}

func relPath(p, basePath string) string {
	p = path.Clean(p)
	if len(p) == 0 || p[0:1] == "/" {
		baseDir := path.Clean(path.Dir(basePath))
		switch {
		case p == baseDir:
			p = "."
		case strings.Index(p, baseDir+"/") == 0:
			p = p[len(baseDir)+1:]
		}
	}
	if isPath(p) {
		return p
	} else {
		return "./" + p
	}
}

func isPath(v string) bool {
	return "." == v || (len(v) > 0 && v[0:1] == "/") || (len(v) > 1 && v[0:2] == "./") || (len(v) > 2 && v[0:3] == "../")
}
