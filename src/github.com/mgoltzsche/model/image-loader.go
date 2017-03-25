package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
)

func LoadImages(names []string) (r map[string]AciImageMetadata, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = errors.New(fmt.Sprintf("image loader: %s", e))
		}
	}()
	r = map[string]AciImageMetadata{}
	for _, name := range names {
		r[name] = loadImageMetadata(name)
	}
	return
}

func loadImageMetadata(name string) (r AciImageMetadata) {
	defer func() {
		if e := recover(); e != nil {
			panic(fmt.Sprintf("Cannot read metadata of image %q: %s", name, e))
		}
	}()
	id := fetchImageAndReturnId(name)
	out, e := exec.Command("cat", "src/github.com/mgoltzsche/model/example-aci-image-manifest.json").Output() // TODO: Call rkt
	panicOnError(e)
	fmt.Print(id + "  " + string(out))
	e = json.Unmarshal(out, &r)
	panicOnError(e)
	r.Name = name
	return
}

func fetchImageAndReturnId(name string) string {
	/*out, e := exec.Command("rkt", "fetch", "--insecure-options=image", name).Output()
	panicOnError(e)
	return string(out)*/
	return "addf"
}

type AciImageMetadata struct {
	Name string
	App  AciApp
}

type AciApp struct {
	Exec             []string
	WorkingDirectory string
	MountPoints      []AciMountPoint
	Ports            []AciPort
	Environment      []AciEnvVar
}

type AciMountPoint struct {
	Name string
	Path string
}

type AciPort struct {
	Name            string
	Protocol        string
	Port            uint32
	Count           uint32
	SocketActivated bool
}

type AciEnvVar struct {
	Name  string
	Value string
}
