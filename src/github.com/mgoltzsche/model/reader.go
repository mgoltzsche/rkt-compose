package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
)

func ReadPodJson(file string) (r PodDecl, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = errors.New(fmt.Sprintf("Failed to read pod JSON %q: %s", file, e))
		}
	}()
	bytes, err := ioutil.ReadFile(file)
	panicOnError(err)
	err = json.Unmarshal(bytes, &r)
	panicOnError(err)
	return
}

func ReadDockerCompose(file string) (r PodDecl, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = errors.New(fmt.Sprintf("Failed to convert docker compose file %q: %s", file, e))
		}
	}()
	c := readDockerCompose(file)
	transformDockerCompose(&c, &r)
	return
}

func readDockerCompose(file string) (r dockerCompose) {
	bytes, err := ioutil.ReadFile(file)
	panicOnError(err)
	err = yaml.Unmarshal(bytes, &r)
	panicOnError(err)
	return
}

func transformDockerCompose(c *dockerCompose, r *PodDecl) {
	applyDefaultValues(r)
	version, err := strconv.ParseFloat(c.Version, 32)
	if err != nil {
		panic("Invalid version: " + c.Version)
	}
	if version > 3 {
		os.Stderr.WriteString("docker compose version >3 is not supported\n")
	}
	for k, v := range c.Services {
		//fmt.Println(k)
		//fmt.Println(v)
		p := "services." + k
		s := ServiceDecl{}
		if v.Extends != nil {
			s.Extends = &ServiceExtensionDecl{v.Extends.File, v.Extends.Service}
		}
		s.Image = v.Image
		s.Entrypoint = toStringArray(v.Entrypoint, p+".entrypoint")
		s.EnvFile = v.EnvFile
		s.Environment = toEnvironmentDecl(v.Environment, p+".environment")
		s.Ports = v.Ports
		s.Hostname = v.Hostname
		s.Domainname = v.Domainname
		s.Volumes = toVolumeMounts(&v)
		r.Services[k] = s
	}
	for k := range c.Volumes {
		r.Volumes[k] = VolumeDecl{"./volumes/" + k, "host", false}
	}
}

func applyDefaultValues(r *PodDecl) {
	r.Version = 1
	r.Services = map[string]ServiceDecl{}
	r.Volumes = map[string]VolumeDecl{}
	r.InjectHosts = true
	r.Net = "default"
	r.Dns = []string{"host"}
}

func toEnvironmentDecl(v interface{}, path string) map[string]string {
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
			r[s[0]] = s[1]
		}
		return r
	case nil:
		return map[string]string{}
	default:
		panic(fmt.Sprintf("map[string]string or []string expected at %s but was: %s", path, v))
	}
}

func toVolumeMounts(s *dcServiceDecl) map[string]string {
	if s.Volumes == nil {
		return nil
	}
	r := map[string]string{}
	for _, e := range s.Volumes {
		s := strings.SplitN(e, ":", 2)
		f := s[0]
		r[s[1]] = f
	}
	return r
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

func toString(v interface{}, ctx string) string {
	switch v.(type) {
	case string:
		return v.(string)
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
	Services map[string]dcServiceDecl
	Volumes  map[string]interface{}
}

type dcServiceDecl struct {
	Extends         *dcExtendsDecl
	Image           string
	Build           interface{}
	Entrypoint      interface{} // string or array
	EnvFile         []string    `yaml:"env_file"`
	Environment     interface{} // array of VAR=VAL or map
	Healthcheck     *dcHealthCheckDecl
	Ports           []string
	Volumes         []string
	Hostname        string
	Domainname      string
	StopGracePeriod string `yaml:"stop_grace_period"`
	StopSignal      string `yaml:"stop_signal"`
	// TODO: Checkout 'secret' dc property
}

type dcExtendsDecl struct {
	File    string
	Service string
}

type dcHealthCheckDecl struct {
	Test     []interface{}
	Interval string
	Timeout  string
	Retries  uint8
	Disable  bool
}
