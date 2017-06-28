package launcher

import (
	"github.com/mgoltzsche/rkt-compose/log"
	"regexp"
	"strings"
)

var substitutionRegex = regexp.MustCompile("\\$[a-zA-Z0-9_]+|\\$\\{[a-zA-Z0-9_]+(:?-.*?)?\\}")

type Substitutes struct {
	substitutes map[string]string
	warn        log.Logger
}

func NewSubstitutes(env map[string]string, warn log.Logger) *Substitutes {
	return &Substitutes{env, warn}
}

func (self *Substitutes) Substitute(v string) string {
	return substitutionRegex.ReplaceAllStringFunc(v, self.substituteExpression)
}

func (self *Substitutes) substituteExpression(v string) string {
	varName := ""
	defaultVal := ""
	hasDefault := false
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
			hasDefault = true
		}
	} else {
		// $VAR syntax
		varName = v[1:]
	}
	if s, ok := self.substitutes[varName]; ok {
		return s
	} else {
		if !hasDefault {
			self.warn.Printf("Warn: %s env var is not set. Defaulting to blank string.", varName)
		}
		return defaultVal
	}
}
