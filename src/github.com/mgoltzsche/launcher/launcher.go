package launcher

import (
	"errors"
	"fmt"
	"github.com/mgoltzsche/model"
	"github.com/mgoltzsche/utils"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type args struct {
	values []string
}

func newArgs(a ...string) *args {
	return &args{a}
}

func (a *args) add(arg ...string) *args {
	a.values = append(a.values, arg...)
	return a
}

func (a *args) toStringSlice() []string {
	return a.values
}

func Run(pod *model.PodDescriptor) (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = errors.New(fmt.Sprintf("run: %s", e))
		}
	}()
	prepareArgs := toRktPrepareArgs(pod).toStringSlice()
	runArgs := toRktRunArgs(pod)
	createVolumeDirectories(pod)
	podUUID := utils.ToTrimmedString(utils.ExecCommand("rkt", prepareArgs...))
	defer exec.Command("rkt", "gc", "--mark-only").Run()
	//podUUID := utils.ToTrimmedString(utils.ExecCommand("rkt", "prepare", "--quiet=true", "--insecure-options=image", "docker://alpine:latest", "--exec=/bin/true"))
	cmd := exec.Command("rkt", runArgs.add(podUUID).toStringSlice()...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	return err
}

func createVolumeDirectories(pod *model.PodDescriptor) {
	for _, vol := range pod.Volumes {
		volFile := absFile(vol.Source, pod)
		os.MkdirAll(volFile, 0770)
	}
}

func toRktRunArgs(pod *model.PodDescriptor) *args {
	r := newArgs(
		"run-prepared",
		"--hostname="+strings.Trim(pod.Hostname+"."+pod.Domainname, "."),
		"--net="+pod.Net)
	for _, dnsIP := range pod.Dns {
		r.add("--dns=" + dnsIP)
	}
	for _, dnsSearch := range pod.DnsSearch {
		r.add("--dns-search=" + dnsSearch)
	}
	if pod.InjectHosts {
		for name := range pod.Services {
			r.add("--hosts-entry=127.0.0.1=" + name)
		}
	}
	return r
}

func toRktPrepareArgs(pod *model.PodDescriptor) *args {
	r := newArgs("prepare", "--quiet=true")
	if containsDockerImage(pod) {
		r.add("--insecure-options=image")
	}
	for k, v := range pod.Environment {
		r.add(fmt.Sprintf("--set-env=%s=%s", k, v))
	}
	for k, v := range pod.Volumes {
		readOnly := strconv.AppendBool([]byte{}, v.Readonly)
		r.add(fmt.Sprintf("--volume=%s,source=%s,kind=%s,readOnly=%s", k, absFile(v.Source, pod), v.Kind, readOnly))
	}
	// TODO: move ports to top level
	for _, s := range pod.Services {
		for portName, p := range s.Ports {
			portArg := "--port=" + portName
			if len(p) > 0 {
				portArg += p
			}
			r.add(portArg)
		}
	}
	for name, s := range pod.Services {
		r.add(s.Image)
		r.add("--name=" + name)
		for k, v := range s.Environment {
			r.add(fmt.Sprintf("--environment=%s=%s", k, v))
		}
		for target, volName := range s.Mounts {
			r.add(fmt.Sprintf("--mount=volume=%s,target=%s", volName, target))
		}
		if len(s.Entrypoint) == 0 {
			panic("undefined entrypoint: " + name)
		}
		r.add("--exec=" + s.Entrypoint[0])
		r.add("--")
		r.add(s.Entrypoint[1:]...)
		r.add(s.Command...)
		r.add("---")
	}
	return r
}

func absFile(path string, pod *model.PodDescriptor) string {
	return filepath.FromSlash(utils.AbsPath(path, pod.File))
}

func containsDockerImage(pod *model.PodDescriptor) bool {
	for _, s := range pod.Services {
		if strings.Index(s.Image, "docker://") == 0 {
			return true
		}
	}
	return false
}
