package model

import (
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var idRegexp = regexp.MustCompile("^[a-z0-9\\-]+$")

type Descriptors struct {
	descriptors          map[string]*PodDescriptor
	defaultVolumeBaseDir string
}

func NewDescriptors(defaultVolumeBaseDir string) *Descriptors {
	return &Descriptors{map[string]*PodDescriptor{}, defaultVolumeBaseDir}
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
		}
		if r.SharedKeys == nil {
			r.SharedKeys = map[string]string{}
		}
		validate(r)
		self.descriptors[filePath] = r
	}
	return r
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
		if v.Image != "" {
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
		if v.Hostname != "" {
			r.Hostname = v.Hostname
		}
		if v.Domainname != "" {
			r.Domainname = v.Domainname
		}
		if v.StopGracePeriod != "" {
			r.StopGracePeriod = v.StopGracePeriod
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
		r.Volumes[k] = &VolumeDescriptor{self.defaultVolumeBaseDir + "/" + k, "host", "false"}
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
			r = append(r, &PortBindingDescriptor{NumberVal(strconv.Itoa(podFrom + d)), NumberVal(strconv.Itoa(hostFrom + d)), hostIP, prot})
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
		interval := c.Interval
		timeout := c.Timeout
		return &HealthCheckDescriptor{cmd, "", interval, timeout, NumberVal(c.Retries), BoolVal(c.Disable)}
	}
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
	Retries  string
	Disable  string
}
