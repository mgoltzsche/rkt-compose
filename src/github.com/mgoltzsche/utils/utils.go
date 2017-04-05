package utils

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
)

var toIdRegexp = regexp.MustCompile("[^a-z0-9]+")

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

func PanicOnError(e error) {
	if e != nil {
		panic(e)
	}
}

func ExecCommand(name string, args ...string) []byte {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	out, e := cmd.Output()
	if e != nil {
		cmd := ""
		if len(args) > 5 {
			cmd = name + "\n  " + strings.Join(args, "\n  ")
		} else {
			cmd = name + " " + strings.Join(args, " ")
		}
		panic(fmt.Sprintf("%s. cmd: %s", e, cmd))
	}
	return out
}

func ToTrimmedString(out []byte) string {
	return strings.TrimRight(string(out), "\n")
}
