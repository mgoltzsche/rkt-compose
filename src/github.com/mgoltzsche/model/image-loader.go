package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mgoltzsche/utils"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

type Images struct {
	pod      *PodDescriptor
	filePath string
	cache    map[string]*ImageMetadata
}

func NewImages(pod *PodDescriptor, filePath string) *Images {
	imgs := &Images{pod, filePath, map[string]*ImageMetadata{}}
	return imgs
}

func (self *Images) LoadImage(name string) (r *ImageMetadata, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = errors.New(strings.TrimRight(fmt.Sprintf("image loader: %s", e), "\n"))
		}
	}()
	cached := self.cache[name]
	if cached != nil {
		r = cached
		return
	}
	r = &ImageMetadata{"", []string{}, "", map[string]string{}, map[string]*ImagePort{}, map[string]string{}}
	id := strings.TrimRight(string(execCommand("rkt", "fetch", "--pull-policy=new", "--insecure-options=image", name)), "\n")
	out := execCommand("rkt", "image", "cat-manifest", id)
	aci := aciImageMetadata{}
	app := &aci.App
	e := json.Unmarshal(out, &aci)
	if e != nil {
		panic(e)
	}
	r.Name = name
	r.Exec = app.Exec
	r.WorkingDirectory = app.WorkingDirectory
	for _, mp := range app.MountPoints {
		r.MountPoints[mp.Name] = mp.Path
	}
	for _, p := range app.Ports {
		r.Ports[p.Name] = &ImagePort{p.Protocol, p.Port}
	}
	for _, env := range app.Environment {
		r.Environment[env.Name] = env.Value
	}
	self.cache[name] = r
	return
}

func (self *Images) buildImage(servName string, s *ServiceDescriptor) (r *ImageMetadata, err error) {
	imgName := self.toImageName(servName, s)
	cached := self.cache[imgName]
	if cached != nil {
		r = cached
		return
	}
	// TODO: lookup (and build) image
	// TODO: lookup
	dockerImgFile, err := ioutil.TempFile("", "docker-image")
	if err != nil {

	}
	defer removeFile(dockerImgFile.Name())
	// aciImgFileName := utils.ToId(imgName) + ".aci"
	execCommand("docker", "build", "-t", imgName, "--rm", filepath.FromSlash(self.toImageDescriptorFile(s.Build)))
	execCommand("docker", "save", "--output", dockerImgFile.Name(), imgName)
	self.cache[imgName] = r
	return
}

func (self *Images) toImageName(servName string, s *ServiceDescriptor) string {
	if len(s.Image) > 0 {
		return s.Image
	} else {
		df := self.toImageDescriptorFile(s.Build)
		st, err := os.Stat(df)
		if err != nil {
			panic(fmt.Sprintf("image loader: %s: %s", df, err))
		}
		return "local/" + utils.ToId(self.pod.Name+"-"+servName+"-"+s.Build.Context) + ":" + st.ModTime().Format("yyMMddhhmmss")
	}
}

func (self *Images) toImageDescriptorFile(b *ServiceBuildDescriptor) string {
	df := b.Dockerfile
	if df == "" {
		df = "Dockerfile"
	}
	return utils.AbsPath(path.Join(b.Context, df), self.filePath)
}

func execCommand(name string, args ...string) []byte {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	out, e := cmd.Output()
	if e != nil {
		panic(fmt.Sprintf("%s: %s", name, e))
	}
	return out
}

func removeFile(file string) {
	e := os.Remove(file)
	if e != nil {
		os.Stderr.WriteString(fmt.Sprintf("image loader: cannot remove file %q: %s", file, e))
	}
}

type ImageMetadata struct {
	Name             string
	Exec             []string
	WorkingDirectory string
	MountPoints      map[string]string
	Ports            map[string]*ImagePort
	Environment      map[string]string
}

type ImagePort struct {
	Protocol string `json:"protocol"`
	Port     uint16 `json:"port"`
}

type aciImageMetadata struct {
	Name string `json:"name"`
	App  aciApp `json:"app"`
}

type aciApp struct {
	Exec             []string         `json:"exec"`
	WorkingDirectory string           `json:"workingDirectory"`
	MountPoints      []*aciMountPoint `json:"mountPoints"`
	Ports            []*aciImagePort  `json:"ports"`
	Environment      []*aciEnvVar     `json:"environment"`
}

type aciImagePort struct {
	Name            string `json:"name"`
	Protocol        string `json:"protocol"`
	Port            uint16 `json:"port"`
	Count           uint16 `json:"count"`
	SocketActivated bool   `json:"socketActivated"`
}

type aciMountPoint struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type aciEnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
