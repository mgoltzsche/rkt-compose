package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
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
	Terminate()
}

type NilListener struct{}

func (l *NilListener) Start(podUUID, podIP string) error { return nil }
func (l *NilListener) Terminate()                        {}

type ConsulLifecycle struct {
	descriptor *model.PodDescriptor
	podUUID    string
	client     *ConsulClient
	checks     *HealthChecks
}

func NewConsulLifecycleFactory(consulAddress string) (LifecycleListenerFactory, error) {
	client := NewConsulClient(consulAddress)
	if !client.CheckAvailability(10) {
		return nil, errors.New("consul unavailable")
	}
	return func(pod *model.PodDescriptor) LifecycleListener {
		// Health checks done within the launcher to be able to run commands within the container
		return &ConsulLifecycle{pod, "", client, nil}
	}, nil
}

func (c *ConsulLifecycle) Start(podUUID, podIP string) error {
	c.podUUID = podUUID
	c.checks = toHealthChecks(c.descriptor, podUUID, c.reportHealth)
	tags := toTags(c.descriptor.Services)
	heartBeats := toHeartBeats(c.descriptor.Services)
	if err := c.client.RegisterService(&Service{c.descriptor.Name, podIP, tags, false, heartBeats}); err != nil {
		return err
	}
	// TODO: create health checks before container start to raise  possible errors early
	c.checks.Start()
	return nil
}

func (c *ConsulLifecycle) Terminate() {
	c.checks.Stop()
	if err := c.client.DeregisterService(c.descriptor.Name); err != nil {
		os.Stderr.WriteString(fmt.Sprintf("Error: launcher: failed to deregister service %q", c.descriptor.Name))
	}
}

func (c *ConsulLifecycle) reportHealth(r *HealthCheckResult) error {
	return c.client.ReportHealth(r.Name, &Health{r.Status, r.Output})
}

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
	err        error
	wait       sync.WaitGroup
}

func (ctx *PodLauncher) Start() (err error) {
	defer func() {
		if e := recover(); e != nil {
			if terr := ctx.terminate(); terr != nil {
				os.Stderr.WriteString(fmt.Sprintf("Error: launcher: termination: %s\n", terr))
			}
			err = errors.New(fmt.Sprintf("launcher: %s", e))
		}
	}()
	ctx.mutex.Lock()
	defer ctx.mutex.Unlock()
	if len(ctx.podUUID) > 0 {
		panic("pod already running")
	}
	ctx.err = nil
	prepareArgs := toRktPrepareArgs(ctx.descriptor).toSlice()
	runArgs := toRktRunArgs(ctx.descriptor)
	createVolumeDirectories(ctx.descriptor)
	ctx.podUUID = utils.ToTrimmedString(utils.ExecCommand("rkt", prepareArgs...))
	ctx.cmd = exec.Command("rkt", runArgs.add(ctx.podUUID).toSlice()...)
	ctx.cmd.Stdout = os.Stdout
	ctx.cmd.Stderr = os.Stderr
	err = ctx.cmd.Start()
	if err != nil {
		panic(err)
	}
	ctx.wait.Add(1)
	go ctx.handleTermination()
	info := ctx.containerInfo()
	if err = ctx.listener.Start(ctx.podUUID, info.Networks[0].IP); err != nil {
		panic(fmt.Sprintf("start listener: %s", err))
	}
	return nil
}

func (ctx *PodLauncher) Stop() (err error) {
	ctx.mutex.Lock()
	defer ctx.mutex.Unlock()
	if len(ctx.podUUID) == 0 {
		ctx.wait.Wait()
		return
	}
	ctx.listener.Terminate()
	err = ctx.terminate()
	ctx.podUUID = ""
	ctx.cmd = nil
	ctx.err = nil
	ctx.wait.Wait()
	if ctx.err != nil {
		if err == nil {
			err = ctx.err
		} else {
			err = errors.New(fmt.Sprintf("termination: %s\nrkt: %s", err, ctx.err))
		}
	}
	return
}

func (ctx *PodLauncher) terminate() (err error) {
	if ctx.cmd != nil && ctx.cmd.Process != nil {
		err = ctx.cmd.Process.Signal(syscall.SIGTERM)
		if err == nil {
			quit := make(chan bool, 1)
			go func() {
				ctx.wait.Wait()
				quit <- true
			}()
			select {
			case <-time.After(time.Second * 10):
				os.Stderr.WriteString("Killing pod since timeout exceeded\n")
				err = ctx.cmd.Process.Kill()
				if err != nil {
					err = errors.New(fmt.Sprintf("Failed to kill rkt process: %s", err))
				}
				<-quit
			case <-quit:
			}
			close(quit)
		} else {
			err = errors.New(fmt.Sprintf("Failed to send SIGTERM to rkt process: %s\n", err))
		}
	}
	return
}

func (ctx *PodLauncher) Wait() {
	ctx.wait.Wait()
}

func (ctx *PodLauncher) handleTermination() {
	ctx.err = ctx.cmd.Wait()
	ctx.wait.Done()
}

func (ctx *PodLauncher) containerInfo() (r *ContainerInfo) {
	for i := 0; i < 4; i++ {
		<-time.After(time.Millisecond * 500)
		r = &ContainerInfo{}
		cmd := exec.Command("rkt", "status", "--format=json", "--wait-ready=5s", ctx.podUUID)
		cmd.Stderr = os.Stderr
		out, err := cmd.Output()
		if err != nil {
			panic(err)
		}
		err = json.Unmarshal(out, r)
		if err != nil {
			panic(err)
		}
		if len(r.Networks) > 0 {
			return
		}
	}
	panic("pod has no network")
}

func NewPodLauncher(pod *model.PodDescriptor, listenerFactory LifecycleListenerFactory) *PodLauncher {
	r := &PodLauncher{}
	r.descriptor = pod
	if listenerFactory == nil {
		r.listener = &NilListener{}
	} else {
		r.listener = listenerFactory(pod)
	}
	r.mutex = &sync.Mutex{}
	return r
}

func MarkGarbageContainers() error {
	return exec.Command("rkt", "gc", "--mark-only").Run()
}

func createVolumeDirectories(pod *model.PodDescriptor) {
	for _, vol := range pod.Volumes {
		volFile := absFile(vol.Source, pod)
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

func toHealthChecks(pod *model.PodDescriptor, podUUID string, reporter HealthReporter) *HealthChecks {
	checks := []*HealthCheck{}
	i := 1
	for k, s := range pod.Services {
		h := s.HealthCheck
		if h != nil && len(h.Command) > 0 {
			name := fmt.Sprintf("service:%s:%d", pod.Name, i)
			indicator := toHealthIndicator(pod, k, podUUID, h)
			check := NewHealthCheck(name, time.Duration(h.Interval), time.Duration(h.Timeout), indicator)
			checks = append(checks, check)
			i++
		}
	}
	return NewHealthChecks(reporter, checks...)
}

func toHealthIndicator(pod *model.PodDescriptor, app, podUUID string, h *model.HealthCheckDescriptor) HealthIndicator {
	switch {
	case len(h.Command) > 0:
		cmd := append([]string{"rkt", "enter", "--app=" + app, podUUID}, h.Command...)
		return CommandBasedHealthIndicator(cmd...)
	case len(h.Http) > 0:
		panic("HTTP health check unsupported")
	default:
		panic("no health check indicator defined")
	}
}

func toHeartBeats(m map[string]*model.ServiceDescriptor) []*HeartBeat {
	r := make([]*HeartBeat, 0, len(m))
	for k, s := range m {
		h := s.HealthCheck
		if h != nil {
			r = append(r, &HeartBeat{k + " check", (time.Duration(h.Interval) * 2).String()})
		}
	}
	return r
}

func toTags(m map[string]*model.ServiceDescriptor) []string {
	t := make([]string, len(m))
	i := 0
	for k := range m {
		t[i] = k
		i++
	}
	return t
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
