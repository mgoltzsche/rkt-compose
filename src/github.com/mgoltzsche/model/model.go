package model

import "time"

func NewPodDescriptor() *PodDescriptor {
	r := &PodDescriptor{}
	r.Version = 1
	r.Services = map[string]*ServiceDescriptor{}
	r.Volumes = map[string]*VolumeDescriptor{}
	r.InjectHosts = true
	r.Net = "default"
	r.Dns = []string{"host"}
	r.DnsSearch = []string{}
	return r
}

type PodDescriptor struct {
	File            string                        `json:"-"`
	Version         uint8                         `json:"version"`
	Name            string                        `json:"name,omitempty"`
	Net             string                        `json:"net,omitempty"`
	Dns             []string                      `json:"dns,omitempty"`
	DnsSearch       []string                      `json:"dns_search,omitempty"`
	Hostname        string                        `json:"hostname,omitempty"`
	Domainname      string                        `json:"domainname,omitempty"`
	InjectHosts     bool                          `json:"inject_hosts"`
	Environment     map[string]string             `json:"environment,omitempty"`
	Services        map[string]*ServiceDescriptor `json:"services"`
	Volumes         map[string]*VolumeDescriptor  `json:"volumes,omitempty"`
	StopGracePeriod string                        `json:"stop_grace_period,omitempty"`
	StopSignal      string                        `json:"stop_signal,omitempty"`
}

type ServiceDescriptor struct {
	Extends      *ServiceDescriptorExtension `json:"extends,omitempty"`
	FetchedImage *ImageMetadata              `json:"-"`
	Image        string                      `json:"image,omitempty"`
	Build        *ServiceBuildDescriptor     `json:"build,omitempty"`
	Entrypoint   []string                    `json:"entrypoint,omitempty"`
	Command      []string                    `json:"command,omitempty"`
	EnvFile      []string                    `json:"env_file,omitempty"`
	Environment  map[string]string           `json:"environment,omitempty"`
	HealthCheck  *HealthCheckDescriptor      `json:"healthcheck,omitempty"`
	Ports        map[string]string           `json:"ports,omitempty"`
	Mounts       map[string]string           `json:"mounts,omitempty"`
}

type ServiceBuildDescriptor struct {
	Context    string            `json:"context,omitempty"`
	Dockerfile string            `json:"dockerfile,omitempty"`
	Args       map[string]string `json:"args,omitempty"`
}

type ServiceDescriptorExtension struct {
	File    string `json:"file"`
	Service string `json:"service"`
}

type HostPortDescriptor struct {
	HostIP   string `json:"ip,omitempty"`
	HostPort uint16 `json:"port"`
}

type VolumeDescriptor struct {
	Source   string `json:"source"`
	Kind     string `json:"kind"`
	Readonly bool   `json:"readonly"`
}

type HealthCheckDescriptor struct {
	Command  []string `json:"cmd,omitempty"`
	Http     string   `json:"http,omitempty"`
	Interval Duration `json:"interval"`
	Timeout  Duration `json:"timeout,omitempty"`
	Retries  uint8    `json:"retries"`
	Disable  bool     `json:"disable"`
}

type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return []byte("\"" + time.Duration(d).String() + "\""), nil
}

func (d Duration) UnmarshalJSON(str []byte) error {
	parsed, e := time.ParseDuration(string(str))
	d = Duration(parsed)
	return e
}

func ParseDuration(str string) (Duration, error) {
	d, e := time.ParseDuration(str)
	return Duration(d), e
}
