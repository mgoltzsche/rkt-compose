package model

import (
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"os"
	"strings"
	"testing"
)

func TestRead(t *testing.T) {
	inFile := "../../../../resources/example-docker-compose.yml"
	expectedBytes, e := ioutil.ReadFile("../../../../resources/example-docker-compose.json")
	if e != nil {
		t.Errorf("Failed to read expected file contents: ", e)
		return
	}
	expected := strings.Trim(string(expectedBytes), "\n")
	models := NewDescriptors(&UserGroup{1000, 997}, log.New(os.Stdout, "", 0))
	descr, e := models.Descriptor(inFile)
	if e != nil {
		t.Errorf("models.Descriptor(%q) returned error: %s", inFile, e)
		return
	}
	j := strings.Trim(descr.JSON(), "\n")
	if j != expected {
		t.Errorf("Unexpected result for file %q.\n\n%s", inFile, diff(expected, j))
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
	start := int(math.Max(0, float64(pos-5)))
	expectedEnd := int(math.Min(float64(len(expectedSegs)), float64(start+11)))
	actualEnd := int(math.Min(float64(len(actualSegs)), float64(start+11)))
	eDiff := strings.Join(expectedSegs[start:expectedEnd], "\n")
	aDiff := strings.Join(actualSegs[start:actualEnd], "\n")
	return fmt.Sprintf("Expected at line %d:\n%s\n\nBut was:\n%s\n", pos, eDiff, aDiff)
}
