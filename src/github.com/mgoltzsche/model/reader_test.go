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

type expected struct {
	file     string
	contents string
}

func TestRead(t *testing.T) {
	inFile := "../../../../resources/reference-model.yml"
	expectedSimple := newExpected("../../../../resources/reference-model.json")
	expectedEnhanced := newExpected("../../../../resources/reference-model-effective.json")
	models := NewDescriptors("./volumes", nil, log.New(os.Stdout, "", 0))
	descr, err := models.Descriptor(inFile)
	if err != nil {
		t.Errorf("models.Descriptor(%q) returned error: %s", inFile, err)
		return
	}
	j := strings.Trim(descr.JSON(), "\n")
	if j != expectedSimple.contents {
		t.Errorf("Unexpected simple descriptor for file %q.\n\n%s", inFile, diff(expectedSimple.contents, j))
	}
	err = models.Complete(descr, PULL_NEW)
	if err != nil {
		t.Errorf("models.Complete(%q, PULL_NEW) returned error: %s", expectedSimple.file, err)
		return
	}
	j = strings.Trim(descr.JSON(), "\n")
	if j != expectedEnhanced.contents {
		t.Errorf("Unexpected enhanced descriptor for file %q.\n\n%s", expectedEnhanced.file, diff(expectedEnhanced.contents, j))
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

func newExpected(file string) *expected {
	b, err := ioutil.ReadFile(file)
	if err != nil {
		panic("Failed to read assertion expectation file: " + err.Error())
	}
	return &expected{file, strings.Trim(string(b), "\n")}
}
