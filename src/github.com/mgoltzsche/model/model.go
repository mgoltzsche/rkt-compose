package model

import (
	"encoding/json"
	"fmt"
	"strconv"
)

func NewPodDescriptor() *PodDescriptor {
	r := &PodDescriptor{}
	r.Version = 1
	r.Services = map[string]*ServiceDescriptor{}
	r.Volumes = map[string]*VolumeDescriptor{}
	r.Net = []string{}
	r.Dns = []string{}
	r.DnsSearch = []string{}
	return r
}

type PodDescriptor struct {
	File                      string                        `json:"-"`
	Version                   uint8                         `json:"version"`
	Name                      string                        `json:"name,omitempty"`
	Net                       []string                      `json:"net,omitempty"`
	Dns                       []string                      `json:"dns,omitempty"`
	DnsSearch                 []string                      `json:"dns_search,omitempty"`
	Hostname                  string                        `json:"hostname,omitempty"`
	Domainname                string                        `json:"domainname,omitempty"`
	DisableHostsInjection     BoolVal                       `json:"disable_hosts_injection,omitempty"`
	Environment               map[string]string             `json:"environment,omitempty"`
	Services                  map[string]*ServiceDescriptor `json:"services"`
	Volumes                   map[string]*VolumeDescriptor  `json:"volumes,omitempty"`
	SharedKeys                map[string]string             `json:"shared_keys,omitempty"`
	SharedKeysOverrideAllowed BoolVal                       `json:"shared_keys_overridable,omitempty"`
	StopGracePeriod           string                        `json:"stop_grace_period,omitempty"`
}

type ServiceDescriptor struct {
	Extends     *ServiceDescriptorExtension `json:"extends,omitempty"`
	Image       string                      `json:"image,omitempty"`
	Build       *ServiceBuildDescriptor     `json:"build,omitempty"`
	Entrypoint  []string                    `json:"entrypoint,omitempty"`
	Command     []string                    `json:"command,omitempty"`
	EnvFile     []string                    `json:"env_file,omitempty"`
	Environment map[string]string           `json:"environment,omitempty"`
	HealthCheck *HealthCheckDescriptor      `json:"healthcheck,omitempty"`
	Ports       []*PortBindingDescriptor    `json:"ports,omitempty"`
	Mounts      map[string]string           `json:"mounts,omitempty"`
}

type ServiceBuildDescriptor struct {
	Context    string `json:"context,omitempty"`
	Dockerfile string `json:"dockerfile,omitempty"`
	// TODO: use args
	Args map[string]string `json:"args,omitempty"`
}

type ServiceDescriptorExtension struct {
	File    string `json:"file"`
	Service string `json:"service"`
}

type PortBindingDescriptor struct {
	Target    NumberVal `json:"target"`
	Published NumberVal `json:"published,omitempty"`
	IP        string    `json:"ip,omitempty"`
	Protocol  string    `json:"protocol,omitempty"`
}

type VolumeDescriptor struct {
	Source   string  `json:"source"`
	Kind     string  `json:"kind,omitempty"`
	Readonly BoolVal `json:"readonly,omitempty"`
}

type HealthCheckDescriptor struct {
	Command  []string  `json:"cmd,omitempty"`
	Http     string    `json:"http,omitempty"`
	Interval string    `json:"interval,omitempty"`
	Timeout  string    `json:"timeout,omitempty"`
	Retries  NumberVal `json:"retries,omitempty"`
	Disable  BoolVal   `json:"disable,omitempty"`
}

func (d *PodDescriptor) JSON() string {
	j, e := json.MarshalIndent(d, "", "  ")
	if e != nil {
		panic("Failed to marshal effective pod model: " + e.Error())
	}
	return string(j)
}

type NumberVal string

func (n NumberVal) MarshalJSON() ([]byte, error) {
	r := string(n)
	if r == "" {
		r = "0"
	} else {
		_, err := strconv.Atoi(r)
		if err != nil {
			r = fmt.Sprintf("%q", r)
		}
	}
	return []byte(r), nil
}

func (n *NumberVal) UnmarshalJSON(v []byte) error {
	str := string(v)
	_, err := strconv.Atoi(str)
	if err == nil {
		*n = NumberVal(str)
	} else {
		str, err = strconv.Unquote(str)
		if err != nil {
			return err
		}
		*n = NumberVal(str)
	}
	return nil
}

type BoolVal string

func (b BoolVal) MarshalJSON() ([]byte, error) {
	r := string(b)
	switch r {
	case "":
		r = "false"
	case "false":
		r = "false"
	case "true":
		r = "true"
	default:
		r = fmt.Sprintf("%q", r)
	}
	return []byte(r), nil
}

func (b *BoolVal) UnmarshalJSON(v []byte) error {
	str := string(v)
	_, err := strconv.ParseBool(str)
	if err == nil {
		*b = BoolVal(str)
	} else {
		str, err = strconv.Unquote(str)
		if err != nil {
			return err
		}
		*b = BoolVal(str)
	}
	return nil
}
