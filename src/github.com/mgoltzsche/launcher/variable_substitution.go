package launcher

import (
	"regexp"
	"strings"
)

var substitutionRegex = regexp.MustCompile("\\$[a-zA-Z0-9_]+|\\$\\{[a-zA-Z0-9_]+(:?-.*?)?\\}")

type Substitutes struct {
	substitutes map[string]string
}

func NewSubstitutes(env map[string]string) *Substitutes {
	return &Substitutes{env}
}

func (self *Substitutes) Substitute(v string) string {
	return substitutionRegex.ReplaceAllStringFunc(v, self.substituteExpression)
}

func (self *Substitutes) substituteExpression(v string) string {
	varName := ""
	defaultVal := ""
	if v[1] == '{' {
		// ${VAR:-default} syntax
		exprEndPos := len(v) - 1
		minusPos := strings.Index(v, "-")
		if minusPos == -1 {
			varName = v[2:exprEndPos]
		} else {
			varEndPos := minusPos
			colonPos := strings.Index(v, ":")
			if colonPos == minusPos-1 {
				varEndPos = colonPos
			}
			varName = v[2:varEndPos]
			defaultVal = v[minusPos+1 : exprEndPos]
		}
	} else {
		// $VAR syntax
		varName = v[1:]
	}
	if s, ok := self.substitutes[varName]; ok {
		return s
	} else {
		// TODO: log undefined variable
		return defaultVal
	}
}
