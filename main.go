package main

import (
	"flag"
	"fmt"
	"github.com/mgoltzsche/rkt-compose/launcher"
	"github.com/mgoltzsche/rkt-compose/log"
	"github.com/mgoltzsche/rkt-compose/model"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

/*type GlobalOptions struct {
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
}*/

var (
	/*var globOpts GlobalOptions
	var runOpts RunOptions
	var dumpOpts DumpOptions*/

	// global options
	verbose  bool
	fetchUid string
	fetchGid string

	// run options
	PodFile string

	uuidFile               string
	name                   string
	net                    StringSlice
	dns                    StringSlice
	defaultVolumeDirectory string
	defaultPublishIP       string
	consulIP               string
	consulApiPort          uint
	consulDatacenter       string
	consulCheckTtl         time.Duration

	// runtime vars
	errorLog      = log.NewStdLogger(os.Stderr)
	debugLog      = log.NewNopLogger()
	fetchImagesAs model.UserGroup
)

type StringSlice []string

var _ flag.Value = new(StringSlice)

func (s *StringSlice) Set(exp string) error {
	*s = append(*s, exp)
	return nil
}

func (s *StringSlice) String() string {
	return fmt.Sprintf("%v", *s)
}

func initFlags() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s OPTIONS ARGUMENTS\n", os.Args[0])
		fmt.Fprint(os.Stderr, "\nArguments:\n")
		fmt.Fprintf(os.Stderr, "  run PODFILE\n\tRuns pod from docker-compose.yml or pod.json file\n")
		fmt.Fprintf(os.Stderr, "  json PODFILE\n\tPrints pod model from file as JSON\n")
		fmt.Fprint(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
	}
	// global options
	flag.BoolVar(&verbose, "verbose", false, "enables verbose log output")
	flag.StringVar(&fetchUid, "fetch-uid", "0", "sets the user to fetch images with")
	flag.StringVar(&fetchGid, "fetch-gid", "0", "sets the group to fetch images with")
	// run options
	flag.StringVar(&uuidFile, "uuid-file", "", "file to save pod UUID to to remove last container on start")
	flag.StringVar(&name, "name", "", "pod name used for service discovery and as default hostname")
	flag.Var(&net, "net", "List of networks")
	flag.Var(&dns, "dns", "List of DNS server IPs")
	flag.StringVar(&defaultVolumeDirectory, "default-volume-dir", "./volumes", "Default volume base directory")
	flag.StringVar(&defaultPublishIP, "default-publish-ip", "", "IP used to publish pod ports")
	flag.StringVar(&consulIP, "consul-ip", "", "sets consul IP and enables service discovery")
	flag.UintVar(&consulApiPort, "consul-api-port", 8500, "sets consul API port")
	flag.StringVar(&consulDatacenter, "consul-datacenter", "dc1", "sets consul datacenter")
	flag.DurationVar(&consulCheckTtl, "consul-check-ttl", time.Duration(60000000000), "sets consul check TTL")
}

func main() {
	initFlags()
	flag.Parse()

	if err := validateFlags(); err != nil {
		errorLog.Println(err)
		os.Exit(1)
	}

	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(1)
	}

	var err error

	switch flag.Arg(0) {
	case "run":
		err = runPod(flag.Arg(1))
	case "json":
		err = dumpJSON(flag.Arg(1))
	default:
		errorLog.Printf("Invalid argument %q", flag.Arg(0))
		os.Exit(1)
	}

	if err != nil {
		errorLog.Println(err)
		os.Exit(2)
	}
}

func validateFlags() error {
	// Init logger
	if verbose {
		debugLog = log.NewStdLogger(os.Stderr)
	}
	// Init fetchAs
	u, err := user.LookupId(fetchUid)
	if err != nil {
		u, err = user.Lookup(fetchUid)
		if err != nil {
			return fmt.Errorf("Cannot find user %q", fetchUid)
		}
	}
	g, err := user.LookupGroupId(fetchGid)
	if err != nil {
		g, err = user.LookupGroup(fetchGid)
		if err != nil {
			return fmt.Errorf("Cannot find group %q", fetchGid)
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
		return fmt.Errorf("User %s is not in group %s", fetchUid, fetchGid)
	}
	fetchImagesAs.Uid = uint32(uid)
	fetchImagesAs.Gid = uint32(gid)
	return nil
}

func runPod(podFile string) (err error) {
	models := model.NewDescriptors(defaultVolumeDirectory)
	imgs := model.NewImages(model.PULL_NEW, &fetchImagesAs, debugLog)
	loader := launcher.NewLoader(models, imgs, defaultVolumeDirectory, errorLog, debugLog)
	descr, err := models.Descriptor(podFile)
	if err != nil {
		return
	}
	if len(name) > 0 {
		descr.Name = name
	}
	pod, err := loader.LoadPod(descr)
	if err != nil {
		return
	}
	if len(net) > 0 {
		pod.Net = net
	}
	if len(dns) > 0 {
		pod.Dns = dns
	}
	var cfg = &launcher.Config{}
	cfg.Pod = pod
	cfg.UUIDFile = uuidFile
	cfg.DefaultPublishIP = defaultPublishIP
	cfg.Debug = debugLog
	cfg.Error = errorLog
	if len(consulIP) > 0 {
		// Enable consul service discovery
		globalNS := "service." + consulDatacenter + ".consul"
		localNS := descr.Name + "." + globalNS
		pod.Dns = []string{consulIP}
		pod.DnsSearch = []string{localNS, globalNS}
		listener, err := launcher.NewConsulLifecycleFactory("http://"+consulIP+":"+strconv.Itoa(int(consulApiPort)), consulCheckTtl, debugLog)
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

func dumpJSON(podFile string) error {
	descrFile, err := filepath.Abs(filepath.FromSlash(podFile))
	if err != nil {
		return err
	}
	models := model.NewDescriptors(defaultVolumeDirectory)
	descr, err := models.Descriptor(descrFile)
	if err != nil {
		return err
	}
	fmt.Println(descr.JSON())
	return err
}
