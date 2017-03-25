package model

import (
	"model"
	"path"
)

var notIdCharRegexp = regexp.MustCompile("[^a-z0-9\\-]+")

func Run(p *model.PodDecl) error {

}

func toVolumeName(p string) string {
	pId := toId(path.Clean(p))
	if pId != "" {
		pId = "-" + pId
	}
	return "volume" + pId
}

func toId(s string) string {
	return strings.Trim(notIdCharRegexp.ReplaceAllLiteralString(s, "-"), "-")
}

func panicOnError(e error) {
	if e != nil {
		panic(e)
	}
}
