package launcher

import (
	"encoding/json"
	"fmt"
	"time"
)

type Pod struct {
	File                      string              `json:"-"`
	Name                      string              `json:"name"`
	Net                       []string            `json:"net"`
	Dns                       []string            `json:"dns"`
	DnsSearch                 []string            `json:"dns_search"`
	Hostname                  string              `json:"hostname"`
	Domainname                string              `json:"domainname"`
	DisableHostsInjection     bool                `json:"disable_hosts_injection"`
	Environment               map[string]string   `json:"environment"`
	Services                  map[string]*Service `json:"services"`
	Volumes                   map[string]*Volume  `json:"volumes"`
	SharedKeys                map[string]string   `json:"shared_keys"`
	SharedKeysOverrideAllowed bool                `json:"shared_keys_overridable"`
	StopGracePeriod           time.Duration       `json:"stop_grace_period"`
}

type Service struct {
	Image       string                 `json:"image"`
	Entrypoint  []string               `json:"entrypoint"`
	Command     []string               `json:"command"`
	Environment map[string]string      `json:"environment"`
	HealthCheck *HealthCheckDescriptor `json:"healthcheck"`
	Ports       []*PortBinding         `json:"ports"`
	Mounts      map[string]string      `json:"mounts"`
}

func NewService() *Service {
	r := &Service{}
	r.Entrypoint = []string{}
	r.Command = []string{}
	r.Environment = map[string]string{}
	r.Ports = []*PortBinding{}
	r.Mounts = map[string]string{}
	r.HealthCheck = &HealthCheckDescriptor{nil, "", time.Duration(10), time.Duration(10), 0, true}
	return r
}

type PortBinding struct {
	Target    uint16 `json:"target"`
	Published uint16 `json:"published"`
	IP        string `json:"ip"`
	Protocol  string `json:"protocol"`
}

type Volume struct {
	Source   string `json:"source"`
	Kind     string `json:"kind"`
	Readonly bool   `json:"readonly"`
}

type HealthCheckDescriptor struct {
	Command  []string      `json:"cmd"`
	Http     string        `json:"http"`
	Interval time.Duration `json:"interval"`
	Timeout  time.Duration `json:"timeout"`
	Retries  uint          `json:"retries"`
	Disable  bool          `json:"disable"`
}

func (d *Pod) JSON() string {
	j, e := json.MarshalIndent(d, "", "  ")
	if e != nil {
		panic(fmt.Sprintf("Failed to marshal pod: %s", e))
	}
	return string(j)
}
