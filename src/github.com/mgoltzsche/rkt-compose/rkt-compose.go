package main

import (
	"encoding/json"
	"fmt"
	"github.com/mgoltzsche/model"
	"os"
	"path/filepath"
)

func main() {
	descrFile, err := filepath.Abs(os.Args[1])
	if err != nil {
		printErrorAndExit(err)
	}
	pod, err := model.LoadModel(descrFile)
	if len(os.Args) > 1 {
		pod.Name = os.Args[2]
	}
	if err == nil {
		imgLoader := model.NewImages(pod, descrFile)
		j, err := json.MarshalIndent(pod, "", "  ")
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		} else {
			fmt.Println(string(j))
		}

		img, err := imgLoader.LoadImage("docker://owncloud:latest")
		if err != nil {
			printErrorAndExit(err)
		}
		fmt.Println(img)
	} else {
		printErrorAndExit(err)
	}
}

func printErrorAndExit(e error) {
	os.Stderr.WriteString(fmt.Sprintf("Error: %s\n", e))
	os.Exit(1)
}
