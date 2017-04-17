package main

import (
	"encoding/json"
	"fmt"
	"github.com/mgoltzsche/launcher"
	"github.com/mgoltzsche/log"
	"github.com/mgoltzsche/model"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

type GlobalOptions struct {
	Debug string `opt:"debug,Enables debug log"`
}

type RunOptions struct {
	PodFile       string `param:"PODFILE"`
	Name          string `opt:"name,Sets the pod's name"`
	ConsulAddress string `opt:"consul-address,Specifies consul address to register the service"`
}

var globOpts GlobalOptions
var runOpts RunOptions

func main() {
	args := NewCmdArgs(&globOpts)
	args.AddCmd("run", "Runs a pod from the descriptor file. Both pod.json and docker-compose.yml descriptors are supported", &runOpts, runPod)
	err := args.Run()
	if err != nil {
		os.Stderr.WriteString(fmt.Sprintf("%s\n", err))
		os.Exit(1)
	}
}

func runPod() error {
	errorLog := log.NewStdLogger(os.Stderr)
	debugLog := log.NewNopLogger()
	if globOpts.Debug == "true" {
		debugLog = log.NewStdLogger(os.Stderr)
	}
	descrFile, err := filepath.Abs(runOpts.PodFile)
	if err != nil {
		return err
	}
	models := model.NewDescriptors(debugLog)
	descr, err := models.Descriptor(descrFile)
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
		checkTtl, e := time.ParseDuration("60s")
		if e != nil {
			panic(e)
		}
		listener, err = launcher.NewConsulLifecycleFactory(runOpts.ConsulAddress, checkTtl, debugLog)
		if err != nil {
			return err
		}
	}
	l := launcher.NewPodLauncher(descr, listener, debugLog, errorLog)
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

func dumpModel(pod *model.PodDescriptor) {
	j, err := json.MarshalIndent(pod, "", "  ")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(j))
}
