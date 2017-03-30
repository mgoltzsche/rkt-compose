package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mgoltzsche/utils"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var idRegexp = regexp.MustCompile("^[a-z0-9\\-]+$")

func LoadModel(file string) (r *PodDescriptor, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = errors.New(fmt.Sprintf("model: %s", e))
		}
	}()
	file = filepath.ToSlash(file)
	r = loadModel(file)
	return
}

func loadModel(file string) (r *PodDescriptor) {
	defer func() {
		if e := recover(); e != nil {
			panic(fmt.Sprintf("%q: %s", file, e))
		}
	}()
	file = resolveDescriptorFile(file)
	fileExt := filepath.Ext(file)
	r = NewPodDescriptor()
	if fileExt == ".yml" || fileExt == ".yaml" {
		readDockerCompose(file, r)
	} else {
		readPodJson(file, r)
	}
	for _, v := range r.Services {
		if v.EnvFile == nil {
			v.EnvFile = []string{}
		}
		if v.Environment == nil {
			v.Environment = map[string]string{}
		}
		if v.Ports == nil {
			v.Ports = map[string]string{}
		}
		if v.Mounts == nil {
			v.Mounts = map[string]string{}
		}
	}
	validate(r)
	resolveExtensions(file, r)
	return r
}

func resolveExtensions(file string, d *PodDescriptor) {
	for k, s := range d.Services {
		if s.Extends != nil {
			kPath := ".services." + k
			assertTrue(len(s.Extends.File) > 0, "empty", kPath+".extends.file")
			f := utils.AbsPath(s.Extends.File, file)
			assertTrue(fileExists(f), "file does not exist: "+f, kPath+".extends.file")
			extServ := loadModel(f).Services[s.Extends.Service]
			assertTrue(extServ != nil, "unresolvable: "+s.Extends.Service, kPath+".extends.service")
			if extServ.Build != nil {
				if s.Build == nil {
					s.Build = &ServiceBuildDescriptor{}
				}
				if len(s.Build.Context) == 0 {
					s.Build.Context = utils.RelPath(utils.AbsPath(extServ.Build.Context, f), file)
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
			if len(s.Hostname) == 0 {
				s.Hostname = extServ.Hostname
			}
			if len(s.Domainname) == 0 {
				s.Domainname = extServ.Domainname
			}
			if s.Entrypoint == nil {
				s.Entrypoint = extServ.Entrypoint
			}
			if s.Command == nil {
				s.Command = extServ.Command
			}
			envFiles := []string{}
			for _, ef := range extServ.EnvFile {
				envFiles = append(envFiles, utils.RelPath(utils.AbsPath(ef, f), file))
			}
			for _, ef := range s.EnvFile {
				envFiles = append(envFiles, ef)
			}
			s.EnvFile = envFiles
			completeMap(extServ.Environment, s.Environment)
			completeMap(extServ.Ports, s.Ports)
			m := map[string]string{}
			for t, v := range extServ.Mounts {
				if utils.IsPath(v) {
					m[t] = utils.AbsPath(v, f)
				} else {
					m[t] = v
				}
			}
			for t, v := range s.Mounts {
				if utils.IsPath(v) {
					m[t] = utils.AbsPath(v, file)
				} else {
					m[t] = v
				}
			}
			s.Mounts = map[string]string{}
			for t, v := range m {
				if utils.IsPath(v) {
					s.Mounts[t] = utils.RelPath(v, file)
				} else {
					s.Mounts[t] = v
				}
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

func validate(d *PodDescriptor) {
	assertTrue(len(d.Net) > 0, "empty", ".net")
	assertTrue(len(d.Dns) > 0, "empty", ".dns")
	assertTrue(d.Services != nil && len(d.Services) > 0, "empty", ".services")
	for k, v := range d.Services {
		kPath := ".services." + k
		assertTrue(idRegexp.MatchString(k), "invalid service name", kPath)
		assertTrue(len(v.Image) > 0 || v.Build != nil || v.Extends != nil, "empty", kPath+".{image|build|extends}")
		assertTrue(v.Build == nil || len(v.Build.Context) > 0, "empty", kPath+".build.context")
		assertTrue(v.Extends == nil || len(v.Extends.File) > 0, "empty", kPath+".extends.file")
		assertTrue(v.Extends == nil || len(v.Extends.Service) > 0, "empty", kPath+".extends.service")
	}
	for k, v := range d.Volumes {
		assertTrue(len(v.Source) > 0, "empty", ".volumes."+k+".source")
	}
}

func assertTrue(e bool, msg, jpath string) {
	if !e {
		panic(fmt.Sprintf("%s: %s", jpath, msg))
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

func fileExists(file string) bool {
	if _, err := os.Stat(filepath.FromSlash(file)); err != nil {
		if os.IsNotExist(err) {
			return false
		} else {
			panic(err)
		}
	}
	return true
}

func isDirectory(file string) bool {
	s, e := os.Stat(filepath.FromSlash(file))
	if e != nil {
		panic(e)
	}
	return s.IsDir()
}

func readPodJson(file string, r *PodDescriptor) {
	bytes := readFile(file)
	err := json.Unmarshal(bytes, r)
	panicOnError(err)
}

func readDockerCompose(file string, r *PodDescriptor) {
	c := dockerCompose{}
	bytes := readFile(file)
	err := yaml.Unmarshal(bytes, &c)
	panicOnError(err)
	transformDockerCompose(&c, r)
}

func readFile(file string) []byte {
	b, e := ioutil.ReadFile(filepath.FromSlash(file))
	panicOnError(e)
	return b
}

func transformDockerCompose(c *dockerCompose, r *PodDescriptor) {
	version, err := strconv.ParseFloat(c.Version, 32)
	if err != nil {
		panic("Invalid version format: " + c.Version)
	}
	if version > 3 {
		os.Stderr.WriteString("Warn: docker compose version >3 is not supported\n")
	}
	for k, v := range c.Services {
		p := "services." + k
		s := &ServiceDescriptor{}
		if v.Extends != nil {
			s.Extends = &ServiceDescriptorExtension{v.Extends.File, v.Extends.Service}
		}
		if len(v.Image) > 0 {
			s.Image = "docker://" + v.Image
		}
		s.Build = toServiceBuildDescriptor(v.Build, p+".build")
		s.Entrypoint = toStringArray(v.Entrypoint, p+".entrypoint")
		s.Command = toStringArray(v.Command, p+".command")
		s.EnvFile = v.EnvFile
		s.Environment = toStringMap(v.Environment, p+".environment")
		s.Hostname = v.Hostname
		s.Domainname = v.Domainname
		s.Mounts = toVolumeMounts(v.Volumes, p+".volumes")
		s.Ports = toPorts(v.Ports, p+".ports")
		s.HealthCheck = toHealthCheckDescriptor(v.HealthCheck, p+".healthcheck")
		r.Services[k] = s
	}
	for k := range c.Volumes {
		r.Volumes[k] = &VolumeDescriptor{"./volumes/" + k, "host", false}
	}
}

func toPorts(p []string, path string) map[string]string {
	r := map[string]string{}
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
		var hostIP, hostPortExpr, destPortExpr string
		switch len(s) {
		case 1:
			hostPortExpr = s[0]
			destPortExpr = hostPortExpr
		case 2:
			hostPortExpr = s[0]
			destPortExpr = s[1]
		case 3:
			hostIP = s[0]
			hostPortExpr = s[1]
			destPortExpr = s[2]
		}
		hostFrom, hostTo := toPortRange(hostPortExpr, path)
		destFrom, destTo := toPortRange(destPortExpr, path)
		rangeSize := destTo - destFrom
		if (hostTo - hostFrom) != rangeSize {
			panic(fmt.Sprintf("Port %q's range size differs between host and destination at %s", e, path))
		}
		for i := 0; i <= rangeSize; i++ {
			portName := strconv.Itoa(destFrom+i) + "-" + prot
			hostPort := strconv.Itoa(hostFrom + i)
			if hostIP == "" {
				r[portName] = hostPort
			} else {
				r[portName] = hostIP + ":" + hostPort
			}
			//r[portName] = &HostPortDescriptor{hostIP, uint16(hostFrom + i)}
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
		return &HealthCheckDescriptor{toStringArray(c.Test, path), c.Interval, c.Timeout, c.Retries, c.Disable}
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

func panicOnError(e error) {
	if e != nil {
		panic(e)
	}
}

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
	StopSignal      string `yaml:"stop_signal"`
	// TODO: Checkout 'secret' dc property
}

type dcServiceDescriptorExtension struct {
	File    string
	Service string
}

type dcHealthCheckDescriptor struct {
	Test     []interface{}
	Interval string
	Timeout  string
	Retries  uint8
	Disable  bool
}
