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
		images, err := model.NewImages(pod, descrFile)
		if err != nil {
			printErrorAndExit(err)
		}
		j, err := json.MarshalIndent(pod, "", "  ")
		if err != nil {
			printErrorAndExit(err)
		} else {
			fmt.Println(string(j))
		}

		img, err := images.Image("selfbuilt", pod.Services["selfbuilt"])
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
