package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/appc/docker2aci/lib"
	"github.com/appc/docker2aci/lib/common"
	"github.com/appc/docker2aci/pkg/log"
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
	images   map[string]*ImageMetadata
}

func NewImages(pod *PodDescriptor, filePath string) (r *Images, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = errors.New(fmt.Sprintf("image loader: %s", e))
		}
	}()
	r = &Images{pod, filePath, map[string]*ImageMetadata{}}
	for k, s := range pod.Services {
		var img *ImageMetadata
		if s.Build == nil {
			img = r.fetchImage(s.Image)
		} else {
			img = r.buildImage(k, s)
		}
		r.images[img.Name] = img
	}
	return
}

func (self *Images) Image(servName string, s *ServiceDescriptor) (img *ImageMetadata, err error) {
	imgName := self.toImageName(servName, s)
	img = self.images[imgName]
	if img == nil {
		err = errors.New(fmt.Sprintf("images: unknown %q", imgName))
	}
	return
}

func (self *Images) fetchImage(name string) *ImageMetadata {
	r := &ImageMetadata{"", []string{}, "", map[string]string{}, map[string]*ImagePort{}, map[string]string{}}
	fmt.Println("fetching " + name)
	id := toTrimmedString(execCommand("rkt", "fetch", "--pull-policy=new", "--insecure-options=image", name))
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
	return r
}

func (self *Images) buildImage(servName string, s *ServiceDescriptor) *ImageMetadata {
	imgName := self.toImageName(servName, s)
	// TODO: lookup (and build) image
	// TODO: lookup
	imgFile := filepath.FromSlash(self.toImageDescriptorFile(s.Build))
	fmt.Println("building docker image from " + imgFile)
	execCommand("docker", "build", "-t", imgName, "--rm", filepath.Dir(imgFile))
	importLocalDockerImage(imgName)
	return self.fetchImage(imgName)
}

func (self *Images) toImageName(servName string, s *ServiceDescriptor) string {
	if len(s.Image) > 0 {
		return s.Image
	} else {
		df := self.toImageDescriptorFile(s.Build)
		_, err := os.Stat(df)
		if err != nil {
			panic(fmt.Sprintf("%s: %s", df, err))
		}
		return "local/" + utils.ToId(self.pod.Name+"-"+servName+"-"+s.Build.Context) + ":latest" //+ st.ModTime().Format("yyMMddhhmmss")
	}
}

func (self *Images) toImageDescriptorFile(b *ServiceBuildDescriptor) string {
	df := b.Dockerfile
	if df == "" {
		df = "Dockerfile"
	}
	return utils.AbsPath(path.Join(b.Context, df), self.filePath)
}

func importLocalDockerImage(imgName string) {
	dockerImgFile, err := ioutil.TempFile("", "docker-image-")
	if err != nil {
		panic("cannot create temp file")
	}
	defer removeFile(dockerImgFile.Name())
	execCommand("docker", "save", "--output", dockerImgFile.Name(), imgName)
	d2aNopLogger := log.NewNopLogger()
	d2aCfg := docker2aci.FileConfig{
		CommonConfig: docker2aci.CommonConfig{
			Squash:      true,
			OutputDir:   os.TempDir(),
			TmpDir:      os.TempDir(),
			Compression: common.GzipCompression,
			Debug:       d2aNopLogger,
			Info:        d2aNopLogger,
		},
		DockerURL: "",
	}
	aciLayerPaths, err := docker2aci.ConvertSavedFile(dockerImgFile.Name(), d2aCfg)
	aciFile := filepath.Join(os.TempDir(), utils.ToId(imgName)+".aci")
	defer removeFile(aciFile)
	if len(aciLayerPaths) < 1 {
		panic(fmt.Sprintf("multiple ACI files returned by docker2aci: %s", err))
	}
	cId := execCommand("rkt", "prepare", "--quiet=true", "--insecure-options=image", aciLayerPaths[0])
	if e := exec.Command("rkt", "rm", toTrimmedString(cId)).Run(); e != nil {
		panic(fmt.Sprintf("cannot remove rkt container %q", cId))
	}
}

func execCommand(name string, args ...string) []byte {
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	out, e := cmd.Output()
	if e != nil {
		cmd := name + " " + strings.Join(args, " ")
		panic(fmt.Sprintf("%s: %s", cmd, e))
	}
	return out
}

func toTrimmedString(out []byte) string {
	return strings.TrimRight(string(out), "\n")
}

func removeFile(file string) {
	e := os.Remove(file)
	if e != nil {
		os.Stderr.WriteString(fmt.Sprintf("image loader: %s\n", e))
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
