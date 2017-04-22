package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mgoltzsche/log"
	"github.com/mgoltzsche/model"
	"github.com/mgoltzsche/utils"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type LifecycleListenerFactory func(pod *model.PodDescriptor) LifecycleListener

type LifecycleListener interface {
	Start(podUUID, podIP string) error
	Terminate() error
}

type NilListener struct{}

func (l *NilListener) Start(podUUID, podIP string) error { return nil }
func (l *NilListener) Terminate() error                  { return nil }

type ContainerInfo struct {
	Name      string              `json:"name"`
	State     string              `json:"state"`
	Networks  []*ContainerNetwork `json:"networks"`
	AppNames  []string            `json:"app_names"`
	StartedAt uint64              `json:"started_at"`
}

type ContainerNetwork struct {
	NetworkName   string `json:"netName"`
	ConfigFile    string `json:"netConf"`
	PluginPath    string `json:"pluginPath"`
	InterfaceName string `json:"ifName"`
	IP            string `json:"ip"`
	Args          string `json:"args"`
	Mask          string `json:"mask"`
}

type PodLauncher struct {
	descriptor *model.PodDescriptor
	listener   LifecycleListener
	podUUID    string
	cmd        *exec.Cmd
	mutex      *sync.Mutex
	once       *sync.Once
	err        error
	wait       sync.WaitGroup
	debug      log.Logger
	info       log.Logger
	error      log.Logger
}

func NewPodLauncher(pod *model.PodDescriptor, listenerFactory LifecycleListenerFactory, debug log.Logger, error log.Logger) *PodLauncher {
	r := &PodLauncher{}
	r.debug = debug
	r.error = error
	r.descriptor = pod
	if listenerFactory == nil {
		r.listener = &NilListener{}
	} else {
		r.listener = listenerFactory(pod)
	}
	r.mutex = &sync.Mutex{}
	r.once = &sync.Once{}
	r.once.Do(func() {})
	return r
}

func (ctx *PodLauncher) Start() (err error) {
	defer func() {
		if e := recover(); e != nil {
			if terr := ctx.terminate(); terr != nil {
				ctx.error.Println(terr)
			}
			err = fmt.Errorf("launcher: %s", e)
		}
	}()
	ctx.mutex.Lock()
	defer ctx.mutex.Unlock()
	if len(ctx.podUUID) > 0 {
		return errors.New("launcher: pod already running")
	}
	ctx.err = nil
	prepareArgs, err := toRktPrepareArgs(ctx.descriptor)
	if err != nil {
		return
	}
	runArgsBuilder := toRktRunArgs(ctx.descriptor)
	ctx.createVolumeDirectories()
	ctx.debug.Println("Preparing pod...")
	out, err := utils.ExecCommand("rkt", prepareArgs...)
	if err != nil {
		return fmt.Errorf("Failed to prepare pod: %s", err)
	}
	ctx.podUUID = utils.ToTrimmedString(out)
	runArgs := runArgsBuilder.add(ctx.podUUID).toSlice()
	ctx.debug.Println("Starting pod...")
	ctx.wait.Add(1)
	ctx.cmd = exec.Command("rkt", runArgs...)
	go ctx.run()
	info, err := ctx.containerInfo()
	if err != nil {
		ctx.terminate()
		return fmt.Errorf("start status: %s", err)
	}
	if err = ctx.listener.Start(ctx.podUUID, info.Networks[0].IP); err != nil {
		ctx.terminate()
		return fmt.Errorf("start listener: %s", err)
	}
	ctx.once = &sync.Once{}
	return nil
}

func (ctx *PodLauncher) Stop() (err error) {
	ctx.debug.Println("Stopping pod...")
	ctx.mutex.Lock()
	defer ctx.mutex.Unlock()
	ctx.once.Do(ctx.invokeTerminationListener)
	err = ctx.terminate()
	ctx.podUUID = ""
	ctx.cmd = nil
	ctx.wait.Wait()
	if ctx.err != nil {
		if err == nil {
			err = ctx.err
		} else {
			err = fmt.Errorf("stop: %s. rkt: %s", err, ctx.err)
		}
		ctx.err = nil
	}
	return
}

func (ctx *PodLauncher) run() {
	defer ctx.onPodTerminated()
	ctx.cmd.Stdout = os.Stdout
	ctx.cmd.Stderr = os.Stderr
	ctx.err = ctx.cmd.Run()
}

func (ctx *PodLauncher) onPodTerminated() {
	ctx.once.Do(ctx.invokeTerminationListener)
	ctx.wait.Done()
}

func (ctx *PodLauncher) terminate() (err error) {
	if ctx.cmd != nil && ctx.cmd.Process != nil {
		ctx.debug.Println("Terminating rkt process...")
		err = ctx.cmd.Process.Signal(syscall.SIGINT)
		if err == nil {
			quit := make(chan bool, 1)
			go func() {
				ctx.wait.Wait()
				quit <- true
			}()
			select {
			case <-time.After(time.Second * 10):
				ctx.error.Println("Killing pod since timeout exceeded")
				err = ctx.cmd.Process.Kill()
				if err != nil {
					err = fmt.Errorf("Failed to kill rkt process: %s", err)
				}
				<-quit
			case <-quit:
			}
			close(quit)
		} else if ctx.cmd.ProcessState.Exited() {
			err = nil
		} else {
			err = fmt.Errorf("Failed to terminate rkt process: %s", err)
		}
	}
	return
}

func (ctx *PodLauncher) invokeTerminationListener() {
	if err := ctx.listener.Terminate(); err != nil {
		ctx.error.Println(err)
	}
}

func (ctx *PodLauncher) Wait() {
	ctx.wait.Wait()
}

func (ctx *PodLauncher) containerInfo() (r *ContainerInfo, err error) {
	ctx.debug.Println("Awaiting pod start...")
	for i := 0; i < 40; i++ { // Loop is workaround since initial command call may list no networks
		r = &ContainerInfo{}
		cmd := exec.Command("rkt", "status", "--format=json", "--wait-ready=5s", ctx.podUUID)
		cmd.Stderr = os.Stderr
		out, e := cmd.Output()
		if err != nil {
			err = e
			return
		}
		err = json.Unmarshal(out, r)
		if err != nil {
			return
		}
		if r.State == "running" && len(r.Networks) > 0 {
			return
		}
		<-time.After(time.Millisecond * 50)
	}
	if r.State == "running" {
		err = errors.New("Pod has no network")
	} else {
		err = errors.New("Pod did not start")
	}
	return
}

func (ctx *PodLauncher) MarkGarbageContainersQuiet() {
	ctx.debug.Println("Marking garbage collectable pods")
	if err := exec.Command("rkt", "gc", "--mark-only").Run(); err != nil {
		ctx.error.Println(err)
	}
}

func (ctx *PodLauncher) createVolumeDirectories() {
	ctx.debug.Println("Creating volume directories...")
	for _, vol := range ctx.descriptor.Volumes {
		volFile := absFile(vol.Source, ctx.descriptor)
		os.MkdirAll(volFile, 0770)
	}
}

func toRktRunArgs(pod *model.PodDescriptor) *args {
	hostname := pod.Hostname
	if len(hostname) == 0 {
		hostname = pod.Name
	}
	r := newArgs(
		"run-prepared",
		"--hostname="+strings.Trim(hostname+"."+pod.Domainname, "."),
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

func toRktPrepareArgs(pod *model.PodDescriptor) ([]string, error) {
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
	// TODO: maybe move ports to top level
	for _, s := range pod.Services {
		for portName, p := range s.Ports {
			portArg := "--port=" + portName
			if len(p) > 0 {
				portArg += ":" + p
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
			return nil, fmt.Errorf("No entrypoint defined in service %q", name)
		}
		r.add("--exec=" + s.Entrypoint[0])
		r.add("--")
		r.add(s.Entrypoint[1:]...)
		r.add(s.Command...)
		r.add("---")
	}
	return r.toSlice(), nil
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

func (a *args) toSlice() []string {
	return a.values
}
