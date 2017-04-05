package main

import (
	"encoding/json"
	"fmt"
	"github.com/mgoltzsche/launcher"
	"github.com/mgoltzsche/model"
	"os"
	"path/filepath"
)

func main() {
	defer func() {
		if e := recover(); e != nil {
			os.Stderr.WriteString(fmt.Sprintf("Error: %s\n", e))
			os.Exit(1)
		}
	}()
	descrFile, err := filepath.Abs(os.Args[1])
	panicOnError(err)
	models := model.NewDescriptors()
	pod, err := models.Descriptor(descrFile)
	panicOnError(err)
	err = models.Complete(pod, model.PULL_NEW)
	panicOnError(err)
	if len(os.Args) > 1 {
		pod.Name = os.Args[2]
	}
	dumpModel(pod)
	err = launcher.Run(pod)
	panicOnError(err)
}

func dumpModel(pod *model.PodDescriptor) {
	j, err := json.MarshalIndent(pod, "", "  ")
	panicOnError(err)
	fmt.Println(string(j))
}

func panicOnError(e error) {
	if e != nil {
		panic(e)
	}
}
