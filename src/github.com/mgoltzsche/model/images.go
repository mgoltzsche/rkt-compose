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

type PullPolicy string

const (
	PULL_NEVER  PullPolicy = "never"
	PULL_NEW    PullPolicy = "new"
	PULL_UPDATE PullPolicy = "update"
)

type Images struct {
	localImagePrefix string
	filePath         string
	images           map[string]*ImageMetadata
	pullPolicy       PullPolicy
}

func NewImages(d *PodDescriptor, pullPolicy PullPolicy) *Images {
	return &Images{d.Name, d.File, map[string]*ImageMetadata{}, pullPolicy}
}

func (self *Images) Image(s *ServiceDescriptor) (img *ImageMetadata, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = errors.New(fmt.Sprintf("loading: %s", e))
		}
	}()
	imgName := self.toImageName(s)
	img = self.images[imgName]
	if img == nil {
		if s.Build == nil {
			img, err = self.fetchImage(s.Image, self.pullPolicy)
			utils.PanicOnError(err)
		} else {
			img = self.buildImage(imgName, s.Build)
		}
		self.images[imgName] = img
	}
	return
}

func (self *Images) fetchImage(name string, pullPolicy PullPolicy) (r *ImageMetadata, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = errors.New(fmt.Sprintf("%q: %s", name, e))
		}
	}()
	r = &ImageMetadata{"", []string{}, "", map[string]string{}, map[string]*ImagePort{}, map[string]string{}}
	fmt.Println("fetching " + name)
	insecOpt := ""
	if strings.Index(name, "docker://") == 0 {
		insecOpt = "image"
	}
	id := utils.ToTrimmedString(utils.ExecCommand("rkt", "fetch", "--pull-policy="+string(pullPolicy), "--insecure-options="+insecOpt, name))
	out := utils.ExecCommand("rkt", "image", "cat-manifest", id)
	aci := aciImageMetadata{}
	app := &aci.App
	if e := json.Unmarshal(out, &aci); e != nil {
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
	return
}

func (self *Images) buildImage(imgName string, b *ServiceBuildDescriptor) *ImageMetadata {
	img, err := self.fetchImage(imgName, PULL_NEVER)
	if err == nil {
		return img
	}
	imgFile := filepath.FromSlash(self.toImageDescriptorFile(b))
	fmt.Println("building docker image from " + imgFile)
	utils.ExecCommand("docker", "build", "-t", imgName, "--rm", filepath.Dir(imgFile))
	importLocalDockerImage(imgName)
	img, err = self.fetchImage(imgName, PULL_NEVER)
	if err != nil {
		panic(err)
	}
	return img
}

func (self *Images) toImageName(s *ServiceDescriptor) string {
	if len(s.Image) > 0 {
		return s.Image
	} else {
		df := self.toImageDescriptorFile(s.Build)
		st, err := os.Stat(df)
		if err != nil {
			panic(fmt.Sprintf("%s: %s", df, err))
		}
		return "local/" + utils.ToId(self.localImagePrefix+"-"+s.Build.Context) + ":" + st.ModTime().Format("20060102150405")
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
	utils.ExecCommand("docker", "save", "--output", dockerImgFile.Name(), imgName)
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
	cId := utils.ToTrimmedString(utils.ExecCommand("rkt", "prepare", "--quiet=true", "--insecure-options=image", aciLayerPaths[0]))
	if e := exec.Command("rkt", "rm", cId).Run(); e != nil {
		panic(fmt.Sprintf("cannot remove rkt container %q", cId))
	}
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
