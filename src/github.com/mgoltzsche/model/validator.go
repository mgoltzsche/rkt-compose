package model

import (
	"regexp"
)

var idRegex = regexp.MustCompile("[a-z0-9\\-]+")

func Validate(p *PodDescriptor) []string {
	return nil
}
