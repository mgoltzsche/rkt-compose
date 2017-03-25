package model

type PodDecl struct {
	Version         uint8                  `json:"version"`
	Name            string                 `json:"name,omitempty"`
	Net             string                 `json:"net,omitempty"`
	Dns             []string               `json:"dns,omitempty"`
	DnsSearch       []string               `json:"dns_search,omitempty"`
	InjectHosts     bool                   `json:"inject_hosts"`
	Services        map[string]ServiceDecl `json:"services"`
	Volumes         map[string]VolumeDecl  `json:"volumes,omitempty"`
	StopGracePeriod string                 `json:"stop_grace_period,omitempty"`
	StopSignal      string                 `json:"stop_signal,omitempty"`
}

type ServiceDecl struct {
	Extends     *ServiceExtensionDecl `json:"extends,omitempty"`
	Image       string                `json:"image,omitempty"`
	Hostname    string                `json:"hostname,omitempty"`
	Domainname  string                `json:"domainname,omitempty"`
	Entrypoint  []string              `json:"entrypoint,omitempty"`
	EnvFile     []string              `json:"env_file,omitempty"`
	Environment interface{}           `json:"environment,omitempty"`
	Healthcheck *HealthCheckDecl      `json:"healthcheck,omitempty"`
	Ports       []string              `json:"ports,omitempty"`
	Volumes     map[string]string     `json:"volumes,omitempty"`
}

type ServiceExtensionDecl struct {
	File    string `json:"file"`
	Service string `json:"service"`
}

type VolumeDecl struct {
	Source   string `json:"source"`
	Kind     string `json:"kind,omitempty"`
	Readonly bool   `json:"readonly"`
}

type HealthCheckDecl struct {
	Test     string `json:"test,omitempty"`
	Interval string `json:"interval,omitempty"`
	Timeout  string `json:"timeout,omitempty"`
	Retries  uint8
	Disable  bool
}
