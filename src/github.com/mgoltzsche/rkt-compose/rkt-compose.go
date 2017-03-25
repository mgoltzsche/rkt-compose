package main

import (
	"encoding/json"
	"fmt"
	"github.com/mgoltzsche/model"
	"os"
)

func main() {
	res, err := model.ReadDockerCompose(os.Args[1])
	if err == nil {
		/*for i := 0; i < len(res.Services); i++ {
			fmt.Println(res.Services[i].Value)
		}*/
		for k, v := range res.Services {
			fmt.Println(k)
			fmt.Println(v)
		}

		j, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		} else {
			fmt.Println(string(j))
		}

		m, err := model.LoadImages([]string{"sfs"})
		if err != nil {
			printErrorAndExit(err)
		}
		fmt.Println(m)
	} else {
		printErrorAndExit(err)
	}
}

func printErrorAndExit(e error) {
	fmt.Println(e)
	os.Exit(1)
}
