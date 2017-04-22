package launcher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

type Service struct {
	Name              string
	Address           string
	Tags              []string
	EnableTagOverride bool
	Check             HeartBeat
}

type HeartBeat struct {
	Notes string
	Ttl   string
}

type ConsulHealthStatus string

const (
	CONSUL_STATUS_PASSING  ConsulHealthStatus = "passing"
	CONSUL_STATUS_WARNING  ConsulHealthStatus = "warning"
	CONSUL_STATUS_CRITICAL ConsulHealthStatus = "critical"
)

type Health struct {
	Status ConsulHealthStatus
	Output string
}

type ConsulClient struct {
	address string
	client  *http.Client
}

func NewConsulClient(address string) *ConsulClient {
	return &ConsulClient{address, &http.Client{
		Timeout: time.Duration(5 * time.Second),
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     60 * time.Second,
			DisableCompression:  true,
			TLSHandshakeTimeout: 5 * time.Second,
		},
	}}
}

func (c *ConsulClient) CheckAvailability(maxRetries uint) bool {
	var err error
	for i := uint(0); i <= maxRetries; i++ {
		err = c.request(http.NewRequest("GET", fmt.Sprintf("%s/v1/kv/?keys", c.address), nil))
		//_, err = c.client.Get(c.address + "/v1/kv/?keys")
		if err == nil {
			return true
		}
		if i == 0 {
			os.Stderr.WriteString(fmt.Sprintf("Consul at %s unavailable. Retrying %d times...\n", c.address, maxRetries))
		}
		<-time.After(time.Second)
	}
	return false
}

// TODO: best case: register service with health checks.
//       To let consul execute command checks mount rkt? binary at consul container
//       Problem: consul container must have rkt permissions
//       Alternative; Only do command checks within this starter and
//       let consul perform HTTP checks on his own to save HTTP connections
//       since every check done here must be propagated to consul via HTTP!
func (c *ConsulClient) RegisterService(s *Service) error {
	j, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return toError("%s", err)
	}
	return c.request(http.NewRequest("PUT", fmt.Sprintf("%s/v1/agent/service/register", c.address), bytes.NewReader(j)))
}

func (c *ConsulClient) DeregisterService(id string) error {
	return c.request(http.NewRequest("GET", fmt.Sprintf("%s/v1/agent/service/deregister/%s", c.address, id), nil))
}

func (c *ConsulClient) ReportHealth(checkId string, r *Health) error {
	j, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return toError("%s", err)
	}
	return c.request(http.NewRequest("PUT", fmt.Sprintf("%s/v1/agent/check/update/%s", c.address, checkId), bytes.NewReader(j)))
}

func (c *ConsulClient) request(req *http.Request, err error) error {
	if err != nil {
		return toError("%s", err)
	}
	r, err := c.client.Do(req)
	if err != nil {
		return toError("%s", err)
	}
	if r.StatusCode != 200 {
		return toError("status %d", r.StatusCode)
	}
	return nil
}

func toError(f string, v ...interface{}) error {
	return fmt.Errorf("consul: "+f, v...)
}
