package model

import (
	"fmt"
	"os"
	"path/filepath"
)

func assertTrue(e bool, msg, location string) {
	if !e {
		panic(fmt.Sprintf("%s: %s", location, msg))
	}
}

func fileExists(filePath string) bool {
	if _, err := os.Stat(filepath.FromSlash(filePath)); err != nil {
		if os.IsNotExist(err) {
			return false
		} else {
			panic(err)
		}
	}
	return true
}

func isDirectory(file string) bool {
	s, e := os.Stat(filepath.FromSlash(file))
	panicOnError(e)
	return s.IsDir()
}

func panicOnError(e error) {
	if e != nil {
		panic(e)
	}
}
