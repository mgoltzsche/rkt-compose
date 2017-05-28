package model

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/mgoltzsche/log"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var idRegexp = regexp.MustCompile("^[a-z0-9\\-]+$")

type Descriptors struct {
	descriptors          map[string]*PodDescriptor
	defaultVolumeBaseDir string
	fetchAs              *UserGroup
	debug                log.Logger
}

func NewDescriptors(defaultVolumeBaseDir string, fetchAs *UserGroup, debug log.Logger) *Descriptors {
	return &Descriptors{map[string]*PodDescriptor{}, defaultVolumeBaseDir, fetchAs, debug}
}

func (self *Descriptors) Descriptor(file string) (r *PodDescriptor, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("model: %s", e)
		}
	}()
	file, err = filepath.Abs(file)
	if err != nil {
		err = fmt.Errorf("Invalid descriptor file path: %s", err)
		return
	}
	file = path.Clean(filepath.ToSlash(file))
	r = self.loadDescriptor(file)
	return
}

func (self *Descriptors) Complete(pod *PodDescriptor, pullPolicy PullPolicy) (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("extend: %q: %s", pod.File, e)
		}
	}()
	self.resolveExtensions(pod, map[string]bool{})
	/*if len(pod.Net) == 0 {
		pod.Net = []string{"compose-bridge"}
	}*/
	resolveEnvFiles(pod)
	fileMountsToVolumes(pod)
	self.addFetchedImages(pod, pullPolicy)
	self.addImageVolumes(pod)
	for _, s := range pod.Services {
		if len(s.Entrypoint) == 0 && len(s.FetchedImage.Exec) > 0 {
			s.Entrypoint = []string{s.FetchedImage.Exec[0]}
			if len(s.Command) == 0 {
				s.Command = s.FetchedImage.Exec[1:]
			}
		}
		if len(s.Image) == 0 {
			s.Image = s.FetchedImage.Name
		}
	}
	return
}

func (self *Descriptors) loadDescriptor(filePath string) (r *PodDescriptor) {
	defer func() {
		if e := recover(); e != nil {
			panic(fmt.Sprintf("%q: %s", filePath, e))
		}
	}()
	r = self.descriptors[filePath]
	if r == nil {
		filePath = resolveDescriptorFile(filePath)
		fileExt := filepath.Ext(filePath)
		r = NewPodDescriptor()
		if fileExt == ".yml" || fileExt == ".yaml" {
			self.readDockerCompose(filePath, r)
		} else {
			readPodJson(filePath, r)
		}
		r.File = filePath
		// Set defaults

		for _, v := range r.Services {
			if v.Entrypoint == nil {
				v.Entrypoint = []string{}
			}
			if v.Command == nil {
				v.Command = []string{}
			}
			if v.EnvFile == nil {
				v.EnvFile = []string{}
			}
			if v.Environment == nil {
				v.Environment = map[string]string{}
			}
			if v.Ports == nil {
				v.Ports = []*PortBindingDescriptor{}
			}
			if v.Mounts == nil {
				v.Mounts = map[string]string{}
			}
			if v.HealthCheck != nil {
				if v.HealthCheck.Interval == 0 {
					v.HealthCheck.Interval = Duration(10 * time.Second)
				}
				if v.HealthCheck.Timeout == 0 {
					v.HealthCheck.Timeout = v.HealthCheck.Interval
				}
			}
		}
		for _, v := range r.Volumes {
			if len(v.Kind) == 0 {
				v.Kind = "host"
			}
		}
		if r.SharedKeys == nil {
			r.SharedKeys = map[string]string{}
		}
		if r.StopGracePeriod == 0 {
			r.StopGracePeriod = Duration(10 * time.Second)
		}
		validate(r)
		self.descriptors[filePath] = r
	}
	return r
}

func (self *Descriptors) addFetchedImages(pod *PodDescriptor, pullPolicy PullPolicy) {
	imgs := NewImages(pod, pullPolicy, self.fetchAs, self.debug)
	for _, s := range pod.Services {
		img, err := imgs.Image(s)
		panicOnError(err)
		s.FetchedImage = img
	}
}

func resolveEnvFiles(d *PodDescriptor) {
	for _, s := range d.Services {
		env := map[string]string{}
		for _, f := range s.EnvFile {
			for k, v := range readEnvFile(absPath(f, d.File)) {
				env[k] = v
			}
		}
		for k, v := range s.Environment {
			env[k] = v
		}
		s.EnvFile = nil
		s.Environment = env
	}
}

func readEnvFile(file string) map[string]string {
	r := map[string]string{}
	f, err := os.Open(filepath.FromSlash(file))
	if err != nil {
		panic(fmt.Sprintf("cannot open %q: %s", file, err))
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	i := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" && strings.Index(line, "#") != 0 {
			kv := strings.SplitN(line, "=", 2)
			assertTrue(len(kv) == 2, fmt.Sprintf("invalid env file line %d: %q", i, kv), file)
			r[kv[0]] = kv[1]
		}
		i++
	}
	panicOnError(scanner.Err())
	return r
}

func fileMountsToVolumes(pod *PodDescriptor) {
	for _, s := range pod.Services {
		for k, v := range s.Mounts {
			if isPath(v) {
				volName := toId(v)
				s.Mounts[k] = volName
				if _, ok := pod.Volumes[volName]; !ok {
					pod.Volumes[volName] = &VolumeDescriptor{v, "host", false}
				}
			}
		}
	}
}

func (self *Descriptors) addImageVolumes(pod *PodDescriptor) {
	for _, s := range pod.Services {
		for volName, _ := range s.FetchedImage.MountPoints {
			if _, ok := pod.Volumes[volName]; !ok {
				src := self.defaultVolumeBaseDir + "/" + volName
				pod.Volumes[volName] = &VolumeDescriptor{src, "host", false}
			}
		}
	}
}

func (self *Descriptors) resolveExtensions(d *PodDescriptor, visited map[string]bool) {
	for k, s := range d.Services {
		if s.Extends != nil {
			kPath := ".services." + k
			assertTrue(len(s.Extends.File) > 0, "empty", kPath+".extends.file")
			extPod := d
			if len(s.Extends.File) > 0 {
				f := absPath(s.Extends.File, d.File)
				assertTrue(fileExists(f), "file does not exist: "+f, kPath+".extends.file")
				extPod = self.loadDescriptor(f)
			}
			extServ := extPod.Services[s.Extends.Service]
			extKey := extPod.File + "/" + s.Extends.Service
			assertTrue(extServ != nil, "unresolvable: "+s.Extends.Service, kPath+".extends.service")
			if _, ok := visited[extKey]; ok {
				i := 0
				keys := make([]string, len(visited))
				for k := range visited {
					keys[i] = k
					i++
				}
				panic(fmt.Sprintf("circular extension: %s", keys))
			}
			visited[extKey] = true
			self.resolveExtensions(extPod, visited)
			if extServ.Build != nil {
				if s.Build == nil {
					s.Build = &ServiceBuildDescriptor{}
				}
				if len(s.Build.Context) == 0 {
					s.Build.Context = relPath(absPath(extServ.Build.Context, extPod.File), d.File)
				}
				if len(s.Build.Dockerfile) == 0 {
					s.Build.Dockerfile = extServ.Build.Dockerfile
				}
				if s.Build.Args == nil {
					s.Build.Args = extServ.Build.Args
				}
			}
			if len(s.Image) == 0 {
				s.Image = extServ.Image
			}
			if len(d.Hostname) == 0 {
				d.Hostname = extPod.Hostname
			}
			if len(d.Domainname) > 0 {
				d.Domainname = extPod.Domainname
			}
			if s.Entrypoint == nil {
				s.Entrypoint = extServ.Entrypoint
			}
			if s.Command == nil {
				s.Command = extServ.Command
			}
			envFiles := []string{}
			for _, ef := range extServ.EnvFile {
				envFiles = append(envFiles, relPath(absPath(ef, extPod.File), d.File))
			}
			for _, ef := range s.EnvFile {
				envFiles = append(envFiles, ef)
			}
			s.EnvFile = envFiles
			completeMap(extServ.Environment, s.Environment)
			s.Ports = extendPorts(extServ.Ports, s.Ports)
			m := map[string]string{}
			for t, v := range extServ.Mounts {
				if isPath(v) {
					m[t] = absPath(v, extPod.File)
				} else {
					m[t] = v
				}
			}
			for t, v := range s.Mounts {
				if isPath(v) {
					m[t] = absPath(v, d.File)
				} else {
					m[t] = v
				}
			}
			s.Mounts = map[string]string{}
			for t, v := range m {
				if isPath(v) {
					s.Mounts[t] = relPath(v, d.File)
				} else {
					s.Mounts[t] = v
				}
			}
			if s.HealthCheck == nil {
				s.HealthCheck = extServ.HealthCheck
			}
			s.Extends = nil
		}
	}
}

func completeMap(src, dest map[string]string) {
	for k, v := range src {
		if _, ok := dest[k]; !ok {
			dest[k] = v
		}
	}
}

func extendPorts(base, ext []*PortBindingDescriptor) []*PortBindingDescriptor {
	r := []*PortBindingDescriptor{}
	m := map[string]bool{}
	for _, p := range ext {
		m[strconv.Itoa(int(p.Target))+"-"+p.Protocol] = true
	}
	for _, p := range base {
		if m[strconv.Itoa(int(p.Target))+"-"+p.Protocol] == false {
			r = append(r, p)
		}
	}
	return append(r, ext...)
}

func validate(d *PodDescriptor) {
	assertTrue(d.Services != nil && len(d.Services) > 0, "empty", ".services")
	for k, v := range d.Services {
		kPath := ".services." + k
		assertTrue(idRegexp.MatchString(k), "invalid service name", kPath)
		assertTrue(len(v.Image) > 0 || v.Build != nil || v.Extends != nil, "empty", kPath+".{image|build|extends}")
		assertTrue(v.Build == nil || len(v.Build.Context) > 0, "empty", kPath+".build.context")
		assertTrue(v.Extends == nil || len(v.Extends.Service) > 0, "empty", kPath+".extends.service")
	}
	for k, v := range d.Volumes {
		assertTrue(len(v.Source) > 0, "empty", ".volumes."+k+".source")
	}
}

func resolveDescriptorFile(file string) string {
	if !fileExists(file) {
		panic("File does not exist")
	}
	if isDirectory(file) {
		for _, f := range [3]string{"/pod.json", "/docker-compose.yml", "/docker-compose.yaml"} {
			f = file + "/" + f
			if fileExists(f) {
				return f
			}
		}
		panic("Descriptor file not found. Lookedup: pod.json, docker-compose.ya?ml")
	}
	return file
}

func readPodJson(file string, r *PodDescriptor) {
	bytes := readFile(file)
	err := json.Unmarshal(bytes, r)
	panicOnError(err)
}

func (self *Descriptors) readDockerCompose(file string, r *PodDescriptor) {
	c := dockerCompose{}
	bytes := readFile(file)
	err := yaml.Unmarshal(bytes, &c)
	panicOnError(err)
	self.transformDockerCompose(&c, r)
}

func readFile(file string) []byte {
	b, e := ioutil.ReadFile(filepath.FromSlash(file))
	panicOnError(e)
	return b
}

func (self *Descriptors) transformDockerCompose(c *dockerCompose, r *PodDescriptor) {
	version, err := strconv.ParseFloat(c.Version, 32)
	if err != nil {
		panic("Invalid version format: " + c.Version)
	}
	if version > 3 {
		os.Stderr.WriteString("Warn: docker compose version >3 is not supported\n")
	}
	r.SharedKeys = map[string]string{}
	for k, v := range c.Services {
		p := "services." + k
		s := &ServiceDescriptor{}
		if v.Extends != nil {
			s.Extends = &ServiceDescriptorExtension{v.Extends.File, v.Extends.Service}
		}
		if len(v.Image) > 0 {
			if v.Build == nil {
				s.Image = "docker://" + v.Image
			} else {
				s.Image = v.Image
			}
		}
		s.Build = toServiceBuildDescriptor(v.Build, p+".build")
		s.Entrypoint = toStringArray(v.Entrypoint, p+".entrypoint")
		s.Command = toStringArray(v.Command, p+".command")
		s.EnvFile = v.EnvFile
		s.Environment = toStringMap(v.Environment, p+".environment")
		if len(r.Hostname) == 0 {
			r.Hostname = v.Hostname
		}
		if len(r.Domainname) == 0 {
			r.Domainname = v.Domainname
		}
		if v.StopGracePeriod != "" {
			stopGracePeriod, err := time.ParseDuration(v.StopGracePeriod)
			if err != nil {
				panic("Invalid stop_grace_period format: " + v.StopGracePeriod)
			}
			if stopGracePeriod > time.Duration(r.StopGracePeriod) {
				r.StopGracePeriod = Duration(stopGracePeriod)
			}
		}
		s.Mounts = toVolumeMounts(v.Volumes, p+".volumes")
		s.Ports = toPorts(v.Ports, p+".ports")
		s.HealthCheck = toHealthCheckDescriptor(v.HealthCheck, p+".healthcheck")
		if httpHost := s.Environment["HTTP_HOST"]; httpHost != "" {
			httpPort := s.Environment["HTTP_PORT"]
			if httpPort == "" {
				panic("HTTP_HOST without HTTP_PORT env var defined in service: " + k)
			}
			r.SharedKeys["http/"+httpHost] = k + ":" + httpPort
		}
		r.Services[k] = s
	}
	for k := range c.Volumes {
		r.Volumes[k] = &VolumeDescriptor{self.defaultVolumeBaseDir + "/" + k, "host", false}
	}
}

func toPorts(p []string, path string) []*PortBindingDescriptor {
	r := []*PortBindingDescriptor{}
	for _, e := range p {
		sp := strings.Split(e, "/")
		if len(sp) > 2 {
			panic(fmt.Sprintf("Invalid port entry %q at %s", e, path))
		}
		prot := "tcp"
		if len(sp) == 2 {
			prot = strings.ToLower(sp[1])
		}
		s := strings.Split(sp[0], ":")
		if len(s) > 3 {
			panic(fmt.Sprintf("Invalid port entry %q at %s", e, path))
		}
		var hostIP, hostPortExpr, podPortExpr string
		switch len(s) {
		case 1:
			hostPortExpr = s[0]
			podPortExpr = hostPortExpr
		case 2:
			hostPortExpr = s[0]
			podPortExpr = s[1]
		case 3:
			hostIP = s[0]
			hostPortExpr = s[1]
			podPortExpr = s[2]
		}
		hostFrom, hostTo := toPortRange(hostPortExpr, path)
		podFrom, podTo := toPortRange(podPortExpr, path)
		rangeSize := podTo - podFrom
		if (hostTo - hostFrom) != rangeSize {
			panic(fmt.Sprintf("Port %q's range size differs between host and destination at %s", e, path))
		}
		for d := 0; d <= rangeSize; d++ {
			r = append(r, &PortBindingDescriptor{uint16(podFrom + d), uint16(hostFrom + d), hostIP, prot})
		}
	}
	return r
}

func toPortRange(rangeExpr string, path string) (from, to int) {
	s := strings.Split(rangeExpr, "-")
	if len(s) < 3 {
		from, err := strconv.Atoi(s[0])
		if err == nil {
			if len(s) == 2 {
				to, err := strconv.Atoi(s[1])
				if err == nil && from <= to {
					return from, to
				}
			} else {
				to = from
				return from, to
			}
		}
	}
	panic(fmt.Sprintf("Invalid port range %q at %s", rangeExpr, path))
}

func toVolumeMounts(dcVols []string, path string) map[string]string {
	if dcVols == nil {
		return nil
	}
	r := map[string]string{}
	for _, e := range dcVols {
		s := strings.SplitN(e, ":", 2)
		if len(s) != 2 {
			panic(fmt.Sprintf("Invalid volume entry %q at %s", e, path))
		}
		f := s[0]
		r[s[1]] = f
	}
	return r
}

func toServiceBuildDescriptor(d interface{}, path string) *ServiceBuildDescriptor {
	switch d.(type) {
	case string:
		return &ServiceBuildDescriptor{d.(string), "", nil}
	case map[interface{}]interface{}:
		m := d.(map[interface{}]interface{})
		r := &ServiceBuildDescriptor{}
		for k, v := range m {
			ks := toString(k, path)
			switch ks {
			case "context":
				r.Context = toString(v, path+"."+ks)
			case "dockerfile":
				r.Dockerfile = toString(v, path+"."+ks)
			case "args":
				r.Args = toStringMap(v, path+"."+ks)
			}
		}
		return r
	case nil:
		return nil
	default:
		panic(fmt.Sprintf("string or []string expected at %s but was: %s", path, d))
	}
}

func toHealthCheckDescriptor(c *dcHealthCheckDescriptor, path string) *HealthCheckDescriptor {
	if c == nil {
		return nil
	} else {
		test := toStringArray(c.Test, path)
		if len(test) == 0 {
			panic(fmt.Sprintf("%s: undefined health test command", path+".test"))
		}
		var cmd []string
		switch test[0] {
		case "CMD":
			cmd = test[1:]
		case "CMD-SHELL":
			cmd = append([]string{"/bin/sh", "-c"}, test[1:]...)
		default:
			cmd = append([]string{"/bin/sh", "-c"}, strings.Join(test, " "))
		}
		interval := toDuration(c.Interval, path+".interval")
		timeout := toDuration(c.Timeout, path+".timeout")
		return &HealthCheckDescriptor{cmd, "", interval, timeout, c.Retries, c.Disable}
	}
}

func toDuration(v, path string) Duration {
	if len(v) == 0 {
		return 0
	}
	d, e := time.ParseDuration(v)
	if e != nil {
		panic(fmt.Sprintf("%s: %s", path, e))
	}
	return Duration(d)
}

func toStringArray(v interface{}, path string) []string {
	switch v.(type) {
	case []interface{}:
		l := v.([]interface{})
		r := make([]string, len(l))
		for i, u := range l {
			r[i] = toString(u, path)
		}
		return r
	case string:
		return strings.Split(strings.Trim(v.(string), " "), " ")
	case nil:
		return []string{}
	default:
		panic(fmt.Sprintf("string or []string expected at %s but was: %s", path, v))
	}
}

func toStringMap(v interface{}, path string) map[string]string {
	switch v.(type) {
	case map[interface{}]interface{}:
		u := v.(map[interface{}]interface{})
		r := map[string]string{}
		for k, v := range u {
			r[toString(k, path)] = toString(v, path)
		}
		return r
	case []interface{}:
		r := map[string]string{}
		for _, u := range v.([]interface{}) {
			e := toString(u, path)
			s := strings.SplitN(e, "=", 2)
			if len(s) != 2 {
				panic(fmt.Sprintf("Invalid environment entry %q at %s", e, path))
			}
			r[s[0]] = s[1]
		}
		return r
	case nil:
		return map[string]string{}
	default:
		panic(fmt.Sprintf("map[string]string or []string expected at %s but was: %s", path, v))
	}
}

func toString(v interface{}, ctx string) string {
	switch v.(type) {
	case string:
		return v.(string)
	case bool:
		return strconv.FormatBool(v.(bool))
	case int:
		return strconv.Itoa(v.(int))
	default:
		panic(fmt.Sprintf("String expected at %s", ctx))
	}
}

// See https://docs.docker.com/compose/compose-file/
type dockerCompose struct {
	Version  string
	Services map[string]*dcServiceDescriptor
	Volumes  map[string]interface{}
}

type dcServiceDescriptor struct {
	Extends         *dcServiceDescriptorExtension
	Image           string
	Build           interface{} // string or map[interface{}]interface{}
	Hostname        string
	Domainname      string
	Entrypoint      interface{}              // string or array
	Command         interface{}              // string or array
	EnvFile         []string                 `yaml:"env_file"`
	Environment     interface{}              // array of VAR=VAL or map
	HealthCheck     *dcHealthCheckDescriptor `yaml:"healthcheck"`
	Ports           []string
	Volumes         []string
	StopGracePeriod string `yaml:"stop_grace_period"`
	// TODO: Checkout 'secret' dc property
}

type dcServiceDescriptorExtension struct {
	File    string
	Service string
}

type dcHealthCheckDescriptor struct {
	Test     interface{}
	Interval string
	Timeout  string
	Retries  uint8
	Disable  bool
}
