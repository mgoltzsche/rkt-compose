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

//
// Permission info:
// - rkt fetch can be run by unprivileged user in rkt group.
// - rkt prepare|run-prepared can be run unprivileged user in rkt-admin group?

type GlobalOptions struct {
	Verbose bool   `opt:"verbose,false,Enables verbose logging"`
	Uid     string `opt:"fetch-uid,0,Sets the user used to fetch images"`
	Gid     string `opt:"fetch-gid,0,Sets the group used to fetch images"`
}

type RunOptions struct {
	PodFile        string        `param:"PODFILE,"`
	UuidFile       string        `opt:"uuid-file,,File to write pod UUID to. If provided last container is removed on container start"`
	Name           string        `opt:"name,,Sets the pod's name"`
	ConsulAddress  string        `opt:"consul-address,,Sets consul address to register the service"`
	ConsulCheckTtl time.Duration `opt:"consul-check-ttl,60s,Sets consul check TTL"` // TODO: encode default values in tag
}

type DumpOptions struct {
	PodFile string `param:"PODFILE,"`
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
var fetchAs model.UserGroup

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
	fetchAs.Uid = uint32(uid)
	fetchAs.Gid = uint32(gid)
	return nil
}

func runPod() error {
	if err := initContext(); err != nil {
		return err
	}
	models := model.NewDescriptors(&fetchAs, debugLog)
	descr, err := models.Descriptor(runOpts.PodFile)
	if err != nil {
		return err
	}
	err = models.Complete(descr, model.PULL_NEW)
	if err != nil {
		return err
	}
	if len(runOpts.Name) > 0 {
		descr.Name = runOpts.Name
	}
	var listener launcher.LifecycleListenerFactory
	//dumpModel(pod)
	// TODO: configure consul optionally
	// TODO: set health to critical on container stop
	if len(runOpts.ConsulAddress) > 0 {
		listener, err = launcher.NewConsulLifecycleFactory(runOpts.ConsulAddress, runOpts.ConsulCheckTtl, debugLog)
		if err != nil {
			return err
		}
	}
	l, err := launcher.NewPodLauncher(descr, runOpts.UuidFile, listener, debugLog, errorLog)
	if err != nil {
		return err
	}
	handleSignals(l)
	defer l.MarkGarbageContainersQuiet()
	err = l.Start()
	if err != nil {
		return err
	}
	l.Wait()
	return nil
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
	models := model.NewDescriptors(&fetchAs, debugLog)
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
