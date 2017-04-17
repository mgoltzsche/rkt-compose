package model

import (
	"encoding/json"
	"fmt"
	"github.com/appc/docker2aci/lib"
	"github.com/appc/docker2aci/lib/common"
	"github.com/mgoltzsche/log"
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
	debug            log.Logger
}

func NewImages(d *PodDescriptor, pullPolicy PullPolicy, debug log.Logger) *Images {
	return &Images{d.Name, d.File, map[string]*ImageMetadata{}, pullPolicy, debug}
}

func (self *Images) Image(s *ServiceDescriptor) (img *ImageMetadata, err error) {
	imgName, err := self.toImageName(s)
	if err != nil {
		return
	}
	img = self.images[imgName]
	if img == nil {
		if s.Build == nil {
			img, err = self.fetchImage(s.Image, self.pullPolicy)
		} else {
			img, err = self.buildImage(imgName, s.Build)
		}
		if err != nil {
			return
		}
		self.images[imgName] = img
	}
	return
}

func (self *Images) fetchImage(name string, pullPolicy PullPolicy) (r *ImageMetadata, err error) {
	r = &ImageMetadata{"", []string{}, "", map[string]string{}, map[string]*ImagePort{}, map[string]string{}}
	self.debug.Printf("Fetching image %q...", name)
	insecOpt := ""
	if strings.Index(name, "docker://") == 0 {
		insecOpt = "image"
	}
	out, err := utils.ExecCommand("rkt", "fetch", "--pull-policy="+string(pullPolicy), "--insecure-options="+insecOpt, name)
	if err != nil {
		return nil, fmt.Errorf("Cannot fetch image %q: %s", name, err)
	}
	id := utils.ToTrimmedString(out)
	out, err = utils.ExecCommand("rkt", "image", "cat-manifest", id)
	if err != nil {
		return nil, fmt.Errorf("Cannot load image manifest %q: %s", name, err)
	}
	aci := aciImageMetadata{}
	app := &aci.App
	if err := json.Unmarshal(out, &aci); err != nil {
		return nil, fmt.Errorf("Cannot unmarshal image manifest: %s", err)
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

func (self *Images) buildImage(imgName string, b *ServiceBuildDescriptor) (img *ImageMetadata, err error) {
	img, err = self.fetchImage(imgName, PULL_NEVER)
	if err == nil {
		return
	}
	imgFile := filepath.FromSlash(self.toImageDescriptorFile(b))
	self.debug.Printf("Building docker image from %q...", imgFile)
	utils.ExecCommand("docker", "build", "-t", imgName, "--rm", filepath.Dir(imgFile))
	self.importLocalDockerImage(imgName)
	img, err = self.fetchImage(imgName, PULL_NEVER)
	if err != nil {
		return
	}
	return
}

func (self *Images) toImageName(s *ServiceDescriptor) (string, error) {
	if len(s.Image) > 0 {
		return s.Image, nil
	} else {
		df := self.toImageDescriptorFile(s.Build)
		st, err := os.Stat(df)
		if err != nil {
			return "", fmt.Errorf("%s: %s", df, err)
		}
		return "local/" + utils.ToId(self.localImagePrefix+"-"+s.Build.Context) + ":" + st.ModTime().Format("20060102150405"), nil
	}
}

func (self *Images) toImageDescriptorFile(b *ServiceBuildDescriptor) string {
	df := b.Dockerfile
	if df == "" {
		df = "Dockerfile"
	}
	return utils.AbsPath(path.Join(b.Context, df), self.filePath)
}

func (self *Images) importLocalDockerImage(imgName string) error {
	dockerImgFile, err := ioutil.TempFile("", "docker-image-")
	if err != nil {
		return fmt.Errorf("Cannot create temp file: %s", err)
	}
	defer removeFile(dockerImgFile.Name())
	if _, err = utils.ExecCommand("docker", "save", "--output", dockerImgFile.Name(), imgName); err != nil {
		return fmt.Errorf("Cannot export docker image %q: %s", imgName, err)
	}
	d2aCfg := docker2aci.FileConfig{
		CommonConfig: docker2aci.CommonConfig{
			Squash:      true,
			OutputDir:   os.TempDir(),
			TmpDir:      os.TempDir(),
			Compression: common.GzipCompression,
			Debug:       self.debug,
			Info:        self.debug,
		},
		DockerURL: "",
	}
	aciLayerPaths, err := docker2aci.ConvertSavedFile(dockerImgFile.Name(), d2aCfg)
	aciFile := filepath.Join(os.TempDir(), utils.ToId(imgName)+".aci")
	defer removeFile(aciFile)
	if len(aciLayerPaths) < 1 {
		return fmt.Errorf("Multiple ACI files returned by docker2aci: %s", err)
	}
	out, err := utils.ExecCommand("rkt", "prepare", "--quiet=true", "--insecure-options=image", aciLayerPaths[0])
	if err != nil {
		return fmt.Errorf("Cannot prepare pod: %s", err)
	}
	cId := utils.ToTrimmedString(out)
	if e := exec.Command("rkt", "rm", cId).Run(); e != nil {
		return fmt.Errorf("Cannot remove rkt container %q", cId)
	}
	return nil
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
