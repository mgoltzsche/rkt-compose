package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mgoltzsche/log"
	"github.com/mgoltzsche/model"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
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
	descriptor       *model.PodDescriptor
	listener         LifecycleListener
	podUUID          string
	podUUIDFile      string
	defaultPublishIP string
	cmd              *exec.Cmd
	mutex            *sync.Mutex
	once             *sync.Once
	err              error
	wait             sync.WaitGroup
	debug            log.Logger
	info             log.Logger
	error            log.Logger
}

type Config struct {
	Pod              *model.PodDescriptor
	UUIDFile         string
	DefaultPublishIP string
	ListenerFactory  LifecycleListenerFactory
	Debug            log.Logger
	Info             log.Logger
	Error            log.Logger
}

func NewPodLauncher(cfg *Config) (*PodLauncher, error) {
	r := &PodLauncher{}
	r.debug = cfg.Debug
	r.error = cfg.Error
	r.descriptor = cfg.Pod
	r.defaultPublishIP = cfg.DefaultPublishIP
	if cfg.UUIDFile != "" {
		uuidFile, err := filepath.Abs(cfg.UUIDFile)
		if err != nil {
			return nil, fmt.Errorf("Invalid pod UUID file: %s", err)
		}
		r.podUUIDFile = uuidFile
	}
	if cfg.ListenerFactory == nil {
		r.listener = &NilListener{}
	} else {
		r.listener = cfg.ListenerFactory(cfg.Pod)
	}
	r.mutex = &sync.Mutex{}
	r.once = &sync.Once{}
	r.once.Do(func() {})
	return r, nil
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
		return fmt.Errorf("launcher: pod already running: %s", ctx.podUUID)
	}
	ctx.err = nil
	runArgsBuilder := toRktRunArgs(ctx.descriptor)
	err = ctx.createVolumeDirectories()
	if err != nil {
		return
	}
	err = ctx.prepare()
	if err != nil {
		return
	}
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

func (ctx *PodLauncher) prepare() error {
	ctx.debug.Println("Preparing pod...")
	ctx.removeLastPod()
	prepareArgs, err := ctx.toRktPrepareArgs(ctx.descriptor)
	if err != nil {
		return err
	}
	c := exec.Command("rkt", prepareArgs...)
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // Run in separate process group to be able to shutdown health checks before container
	c.Stderr = os.Stderr
	out, err := c.Output()
	if err != nil {
		return fmt.Errorf("Failed to prepare pod: %s", err)
	}
	ctx.podUUID = strings.TrimRight(string(out), "\n")
	err = ctx.writeUuidFile()
	if err != nil {
		exec.Command("rkt", "rm", ctx.podUUID).Run()
	}
	return err
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
		err = exec.Command("rkt", "stop", ctx.podUUID).Run()
		if err != nil {
			ctx.error.Println("Killing pod since termination failed: ", err)
			err = ctx.cmd.Process.Kill()
			if err != nil && !ctx.cmd.ProcessState.Exited() {
				err = fmt.Errorf("Failed to kill rkt process: %s", err)
			}
			return
		}
		quit := make(chan bool, 1)
		go func() {
			ctx.wait.Wait()
			quit <- true
		}()
		select {
		case <-time.After(time.Duration(ctx.descriptor.StopGracePeriod)):
			ctx.error.Println("Killing pod since stop timeout exceeded")
			err = ctx.cmd.Process.Kill()
			if err != nil && !ctx.cmd.ProcessState.Exited() {
				err = fmt.Errorf("Failed to kill rkt process: %s", err)
			}
			<-quit
		case <-quit:
		}
		close(quit)
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
	interval := time.Millisecond * 50
	for i := 0; i < 40; i++ { // Loop is workaround since initial command call may list no networks
		r = &ContainerInfo{}
		cmd := exec.Command("rkt", "status", "--format=json", "--wait-ready=5s", ctx.podUUID)
		cmd.Stderr = os.Stderr
		out, e := cmd.Output()
		if err != nil {
			err = fmt.Errorf("Failed to request rkt pod status: %s", e)
			return
		}
		err = json.Unmarshal(out, r)
		if err != nil {
			err = fmt.Errorf("Failed to unmarshal rkt status. %s. Output: %s", err, string(out))
			return
		}
		if r.State == "running" && len(r.Networks) > 0 {
			return
		}
		<-time.After(interval)
	}
	if r.State == "running" {
		err = errors.New("Pod has no network")
	} else {
		err = fmt.Errorf("Pod start timed out after %s", time.Duration(interval*40))
	}
	return
}

func (ctx *PodLauncher) writeUuidFile() error {
	if ctx.podUUIDFile != "" {
		return ioutil.WriteFile(ctx.podUUIDFile, []byte(ctx.podUUID), 0644)
	}
	return nil
}

func (ctx *PodLauncher) removeLastPod() {
	if ctx.podUUIDFile != "" {
		ctx.debug.Println("Removing last pod...")
		err := exec.Command("rkt", "rm", "--uuid-file="+ctx.podUUIDFile).Run()
		if err != nil {
			ctx.debug.Printf("Warn: Could not remove last pod: %s", err)
		}
	}
}

func (ctx *PodLauncher) MarkGarbageContainersQuiet() {
	ctx.debug.Println("Marking garbage collectable pods")
	if err := exec.Command("rkt", "gc", "--mark-only").Run(); err != nil {
		ctx.error.Println(err)
	}
}

func (ctx *PodLauncher) createVolumeDirectories() error {
	ctx.debug.Println("Creating volume directories...")
	for _, vol := range ctx.descriptor.Volumes {
		volFile := absFile(vol.Source, ctx.descriptor)
		_, err := os.Stat(volFile)
		if os.IsNotExist(err) {
			if err := os.MkdirAll(volFile, 0770); err != nil {
				return fmt.Errorf("Failed to create volume directories: %s", err)
			}
		} else if err != nil {
			return fmt.Errorf("Cannot access volume: %s", err)
		}
	}
	return nil
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

func (ctx *PodLauncher) toRktPrepareArgs(pod *model.PodDescriptor) ([]string, error) {
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
	for _, s := range pod.Services {
		for portName, p := range s.Ports {
			portArg := portName
			if p.IP == "" {
				if ctx.defaultPublishIP != "" {
					portArg += ":" + ctx.defaultPublishIP
				}
			} else {
				portArg += ":" + p.IP
			}
			if p.Port > 0 {
				portArg += ":" + strconv.Itoa(int(p.Port))
			}
			r.add("--port=" + portArg)
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

func absFile(p string, pod *model.PodDescriptor) string {
	if len(p) > 0 && p[0:1] == "/" {
		p = path.Clean(p)
	} else {
		p = path.Join(path.Dir(pod.File), p)
	}
	return filepath.FromSlash(p)
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
