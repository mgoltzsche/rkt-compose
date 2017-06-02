package model

import (
	"fmt"
	"io/ioutil"
	"math"
	"strings"
	"testing"
)

func TestRead(t *testing.T) {
	for _, prefix := range []string{"../../../../test-resources/reference-model", "../../../../test-resources/consul"} {
		dcFile := prefix + ".yml"
		expectedFile := prefix + ".json"
		expectedBytes, err := ioutil.ReadFile(expectedFile)
		if err != nil {
			t.Errorf("Failed to read assertion expectation file: %s", err)
			return
		}
		expected := strings.Trim(string(expectedBytes), "\n")
		models := NewDescriptors("./volumes")
		// TODO: also try parsing json version
		descr, err := models.Descriptor(dcFile)
		if err != nil {
			t.Errorf("models.Descriptor(%q) returned error: %s", dcFile, err)
			return
		}
		j := strings.Trim(descr.JSON(), "\n")
		if j != expected {
			t.Errorf("Unexpected %q state.\n\n%s", dcFile, diff(expected, j))
			return
		}
	}
}

func diff(expected, actual string) string {
	expectedSegs := strings.Split(expected, "\n")
	actualSegs := strings.Split(actual, "\n")
	pos := 0
	for i := 0; i < int(math.Max(float64(len(expectedSegs)), float64(len(actualSegs)))); i++ {
		if i >= len(expectedSegs) || i >= len(actualSegs) || expectedSegs[i] != actualSegs[i] {
			pos = i
			break
		}
	}
	fmt.Printf(actual)
	start := int(math.Max(0, float64(pos-5)))
	expectedEnd := int(math.Min(float64(len(expectedSegs)), float64(start+11)))
	actualEnd := int(math.Min(float64(len(actualSegs)), float64(start+11)))
	eDiff := strings.Join(expectedSegs[start:expectedEnd], "\n")
	aDiff := strings.Join(actualSegs[start:actualEnd], "\n")
	return fmt.Sprintf("Expected at line %d:\n%s\n\nBut was:\n%s\n", pos, eDiff, aDiff)
}
