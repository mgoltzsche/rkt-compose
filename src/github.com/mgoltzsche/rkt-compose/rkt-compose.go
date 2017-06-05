package main

import (
	"encoding/json"
	"fmt"
	"github.com/mgoltzsche/launcher"
	"github.com/mgoltzsche/log"
	"github.com/mgoltzsche/model"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

type GlobalOptions struct {
	Verbose bool   `opt:"verbose,false,Enables verbose logging"`
	Uid     string `opt:"fetch-uid,0,Sets the user used to fetch images"`
	Gid     string `opt:"fetch-gid,0,Sets the group used to fetch images"`
}

type RunOptions struct {
	PodFile                string        `param:"PODFILE,"`
	UUIDFile               string        `opt:"uuid-file,,Pod UUID file. If provided last container is removed on container start"`
	Name                   string        `opt:"name,,Pod name. Used for service discovery and as default hostname"`
	Net                    []string      `opt:"net,,List of networks"`
	Dns                    []string      `opt:"dns,,List of DNS server IPs"`
	DefaultVolumeDirectory string        `opt:"default-volume-dir,./volumes,Default volume base directory"`
	DefaultPublishIP       string        `opt:"default-publish-ip,,IP used to publish pod ports"`
	ConsulIP               string        `opt:"consul-ip,,Sets consul IP and enables service discovery"`
	ConsulApiPort          string        `opt:"consul-api-port,8500,Consul API port"`
	ConsulDatacenter       string        `opt:"consul-datacenter,dc1,Consul datacenter"`
	ConsulCheckTtl         time.Duration `opt:"consul-check-ttl,60s,Consul check TTL"`
}

type DumpOptions struct {
	PodFile                string `param:"PODFILE,"`
	DefaultVolumeDirectory string `opt:"default-volume-dir,./volumes,Default volume base directory"`
}

var globOpts GlobalOptions
var runOpts RunOptions
var dumpOpts DumpOptions

func main() {
	args := NewCmdArgs(&globOpts)
	args.AddCmd("run", "Runs a pod from the descriptor file. Both pod.json and docker-compose.yml descriptors are supported", &runOpts, runPod)
	args.AddCmd("dump", "Loads a pod model and dumps it as JSON", &dumpOpts, dumpPod)
	err := args.Run()
	if err != nil {
		errorLog.Println(err)
		os.Exit(1)
	}
}

var errorLog = log.NewStdLogger(os.Stderr)
var debugLog = log.NewNopLogger()
var fetchImagesAs model.UserGroup

func initContext() error {
	// Init logger
	if globOpts.Verbose {
		debugLog = log.NewStdLogger(os.Stderr)
	}
	// Init fetchAs
	u, err := user.LookupId(globOpts.Uid)
	if err != nil {
		u, err = user.Lookup(globOpts.Uid)
		if err != nil {
			return fmt.Errorf("Cannot find user %q", globOpts.Uid)
		}
	}
	g, err := user.LookupGroupId(globOpts.Gid)
	if err != nil {
		g, err = user.LookupGroup(globOpts.Gid)
		if err != nil {
			return fmt.Errorf("Cannot find group %q", globOpts.Gid)
		}
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		panic("Cannot parse user ID: " + u.Uid)
	}
	gid, err := strconv.ParseUint(g.Gid, 10, 32)
	if err != nil {
		panic("Cannot parse group ID: " + g.Gid)
	}
	gids, err := u.GroupIds()
	if err != nil {
		panic("Could not look up user's group IDs: " + err.Error())
	}
	hasGid := false
	for _, gid := range gids {
		if g.Gid == gid {
			hasGid = true
			break
		}
	}
	if !hasGid {
		return fmt.Errorf("User %s is not in group %s", globOpts.Uid, globOpts.Gid)
	}
	fetchImagesAs.Uid = uint32(uid)
	fetchImagesAs.Gid = uint32(gid)
	return nil
}

func runPod() (err error) {
	if err = initContext(); err != nil {
		return
	}
	models := model.NewDescriptors(runOpts.DefaultVolumeDirectory)
	imgs := model.NewImages(model.PULL_NEW, &fetchImagesAs, debugLog)
	loader := launcher.NewLoader(models, imgs, runOpts.DefaultVolumeDirectory, errorLog, debugLog)
	descr, err := models.Descriptor(runOpts.PodFile)
	if err != nil {
		return
	}
	if len(runOpts.Name) > 0 {
		descr.Name = runOpts.Name
	}
	pod, err := loader.LoadPod(descr)
	if err != nil {
		return
	}
	if len(runOpts.Net) > 0 {
		pod.Net = runOpts.Net
	}
	if len(runOpts.Dns) > 0 {
		pod.Dns = runOpts.Dns
	}
	var cfg = &launcher.Config{}
	cfg.Pod = pod
	cfg.UUIDFile = runOpts.UUIDFile
	cfg.DefaultPublishIP = runOpts.DefaultPublishIP
	cfg.Debug = debugLog
	cfg.Error = errorLog
	if len(runOpts.ConsulIP) > 0 {
		// Enable consul service discovery
		globalNS := "service." + runOpts.ConsulDatacenter + ".consul"
		localNS := descr.Name + "." + globalNS
		pod.Dns = []string{runOpts.ConsulIP}
		pod.DnsSearch = []string{localNS, globalNS}
		listener, err := launcher.NewConsulLifecycleFactory("http://"+runOpts.ConsulIP+":"+runOpts.ConsulApiPort, runOpts.ConsulCheckTtl, debugLog)
		if err != nil {
			return err
		}
		cfg.ListenerFactory = listener
	}
	l, err := launcher.NewPodLauncher(cfg)
	if err != nil {
		return
	}
	handleSignals(l)
	defer l.MarkGarbageContainersQuiet()
	err = l.Start()
	if err != nil {
		return err
	}
	return l.Wait()
}

func handleSignals(l *launcher.PodLauncher) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
	go func() {
		<-sigs
		err := l.Stop()
		if err != nil {
			os.Stderr.WriteString(fmt.Sprintf("Failed to stop: %s\n", err))
		}
	}()
}

func dumpPod() error {
	if err := initContext(); err != nil {
		return err
	}
	descrFile, err := filepath.Abs(filepath.FromSlash(dumpOpts.PodFile))
	if err != nil {
		return err
	}
	models := model.NewDescriptors(dumpOpts.DefaultVolumeDirectory)
	descr, err := models.Descriptor(descrFile)
	if err != nil {
		return err
	}
	fmt.Println(descr.JSON())
	return err
}

func dumpModel(pod *model.PodDescriptor) {
	j, err := json.MarshalIndent(pod, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(j))
}
