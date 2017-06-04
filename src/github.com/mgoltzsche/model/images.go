package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/appc/docker2aci/lib"
	"github.com/appc/docker2aci/lib/common"
	"github.com/mgoltzsche/log"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

type PullPolicy string

const (
	PULL_NEVER  PullPolicy = "never"
	PULL_NEW    PullPolicy = "new"
	PULL_UPDATE PullPolicy = "update"
)

type UserGroup struct {
	Uid uint32
	Gid uint32
}

type Images struct {
	images     map[string]*ImageMetadata
	pullPolicy PullPolicy
	fetchAs    *UserGroup
	debug      log.Logger
}

var toIdRegexp = regexp.MustCompile("[^a-z0-9]+")

func NewImages(pullPolicy PullPolicy, fetchAs *UserGroup, debug log.Logger) *Images {
	return &Images{map[string]*ImageMetadata{}, pullPolicy, fetchAs, debug}
}

func (self *Images) Image(name string) (*ImageMetadata, error) {
	return self.fetchImage(name, self.pullPolicy)
}

func (self *Images) fetchImage(name string, pullPolicy PullPolicy) (r *ImageMetadata, err error) {
	r = self.images[name]
	if r != nil {
		return
	}
	r = &ImageMetadata{"", []string{}, "", map[string]string{}, map[string]*ImagePort{}, map[string]string{}}
	self.debug.Printf("Fetching image %q...", name)
	insecOpt := ""
	if strings.Index(name, "docker://") == 0 {
		insecOpt = "image"
	}
	var stderr bytes.Buffer
	c := exec.Command("rkt", "fetch", "--pull-policy="+string(pullPolicy), "--insecure-options="+insecOpt, name)
	if self.fetchAs != nil {
		c.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: self.fetchAs.Uid, Gid: self.fetchAs.Gid}}
	}
	if pullPolicy == PULL_NEVER {
		c.Stderr = &stderr
	} else {
		c.Stderr = os.Stderr
	}
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("Cannot fetch image %q: %s. %s", name, err, stderr.String())
	}
	id := strings.TrimRight(string(out), "\n")
	c = exec.Command("rkt", "image", "cat-manifest", id)
	c.Stderr = os.Stderr // TODO: set log
	out, err = c.Output()
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
	self.images[name] = r
	return
}

func (self *Images) BuildImage(name, dockerFile, contextPath string) (img *ImageMetadata, err error) {
	img, err = self.fetchImage(name, PULL_NEVER)
	if err == nil {
		return
	}
	imgFile := filepath.FromSlash(dockerFile)
	dockerFileDir := filepath.Dir(imgFile)
	if contextPath == "" {
		contextPath = dockerFileDir
	}
	self.debug.Printf("Building docker image from %q...", imgFile)
	c := exec.Command("docker", "build", "-t", name, "--rm", dockerFileDir)
	c.Dir = contextPath
	c.Stdout = os.Stdout // TODO: write to log
	c.Stderr = os.Stderr
	if err = c.Run(); err != nil {
		return
	}
	if err = self.importLocalDockerImage(name); err != nil {
		return
	}
	img, err = self.fetchImage(name, PULL_NEVER)
	self.images[name] = img
	return
}

func (self *Images) importLocalDockerImage(imgName string) error {
	dockerImgFile, err := ioutil.TempFile("", "docker-image-")
	if err != nil {
		return fmt.Errorf("Cannot create temp file: %s", err)
	}
	defer removeFile(dockerImgFile.Name())
	self.debug.Println("Exporting docker image to file...")
	out, err := exec.Command("docker", "save", "--output", dockerImgFile.Name(), imgName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("Cannot export docker image %q: %s. %s", imgName, err, out)
	}
	self.debug.Println("Converting docker image to ACI...")
	tmpDir := os.TempDir()
	d2aCfg := docker2aci.FileConfig{
		CommonConfig: docker2aci.CommonConfig{
			Squash:      true,
			OutputDir:   tmpDir,
			TmpDir:      tmpDir,
			Compression: common.GzipCompression,
			Debug:       self.debug,
			Info:        self.debug,
		},
		DockerURL: "",
	}
	aciLayerPaths, err := docker2aci.ConvertSavedFile(dockerImgFile.Name(), d2aCfg)
	if err != nil {
		return fmt.Errorf("Cannot convert docker image to ACI: %s", err)
	}
	if len(aciLayerPaths) < 1 {
		return fmt.Errorf("No ACI files returned by docker2aci")
	}
	for _, f := range aciLayerPaths {
		defer removeFile(f)
	}
	self.debug.Println("Importing ACI file...")
	var stderr bytes.Buffer
	c := exec.Command("rkt", "prepare", "--quiet=true", "--insecure-options=image", aciLayerPaths[0])
	c.Stderr = &stderr
	out, err = c.Output()
	if err != nil {
		return fmt.Errorf("Cannot import converted docker image: %s. %s", err, stderr.String())
	}
	cId := strings.TrimRight(string(out), "\n")
	if e := exec.Command("rkt", "rm", cId).Run(); e != nil {
		return fmt.Errorf("Cannot remove rkt pod %q used to import converted docker image: %s", cId, e)
	}
	return nil
}

func toId(v string) string {
	return strings.Trim(toIdRegexp.ReplaceAllLiteralString(strings.ToLower(v), "-"), "-")
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
