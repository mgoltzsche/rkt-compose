package launcher

import (
	"github.com/mgoltzsche/rkt-compose/log"
	"testing"
)

var testenv = map[string]string{
	"VAR1": "dyn1",
	"VAR2": "dyn2",
}

func TestRead(t *testing.T) {
	assertSubstitution(t, "static-dyn1", "static-$VAR1")
	assertSubstitution(t, "static-dyn1-XY", "static-$VAR1-XY")
	assertSubstitution(t, "static-dyn1-dyn2", "static-$VAR1-$VAR2")
	assertSubstitution(t, "static-dyn1-dyn2-", "static-$VAR1-$VAR2-$VAR3")
	assertSubstitution(t, "static-dyn1", "static-${VAR1}")
	assertSubstitution(t, "static-dyn1-XY", "static-${VAR1}-XY")
	assertSubstitution(t, "static-dyn1-dyn2", "static-${VAR1}-${VAR2}")
	assertSubstitution(t, "static-dyn1-dyn2-", "static-${VAR1}-${VAR2}-${VAR3}")
	assertSubstitution(t, "static-dyn1-dyn2-defaultval", "static-${VAR1}-${VAR2}-${VAR3-defaultval}")
	assertSubstitution(t, "static-dyn1-dyn2-defaultval", "static-${VAR1}-${VAR2}-${VAR3:-defaultval}")
}

func assertSubstitution(t *testing.T, expected string, input string) {
	testee := NewSubstitutes(testenv, log.NewNopLogger())
	actual := testee.Substitute(input)
	if actual != expected {
		t.Errorf("%q should be replaced with %q but was %q", input, expected, actual)
	}
}
