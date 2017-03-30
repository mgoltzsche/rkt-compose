package utils

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

var toIdRegexp = regexp.MustCompile("[^a-z0-9\\-]+")

func ToId(v string) string {
	return strings.Trim(toIdRegexp.ReplaceAllLiteralString(v, "-"), "-")
}

func IsPath(v string) bool {
	return "." == v || (len(v) > 0 && v[0:1] == "/") || (len(v) > 1 && v[0:2] == "./")
}

func RelPath(p, basePath string) string {
	p = path.Clean(p)
	if len(p) == 0 || p[0:1] == "/" {
		baseDir := path.Clean(path.Dir(basePath))
		fmt.Println(baseDir + " " + p)
		switch {
		case p == baseDir:
			p = "."
		case strings.Index(p, baseDir+"/") == 0:
			p = p[len(baseDir)+1:]
		}
	}
	if IsPath(p) {
		return p
	} else {
		return "./" + p
	}
}

func AbsPath(p, basePath string) string {
	if len(p) > 0 && p[0:1] == "/" {
		return path.Clean(p)
	} else {
		return path.Join(path.Dir(basePath), p)
	}
}
