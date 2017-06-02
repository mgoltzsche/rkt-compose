package launcher

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type ConsulService struct {
	ID                string
	Name              string
	Address           string
	Tags              []string
	EnableTagOverride bool
	Check             HeartBeat `json:"Check,omitempty"`
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
	for i := uint(0); i <= maxRetries; i++ {
		_, err := c.request("GET", "kv/?keys", nil, 200)
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
func (c *ConsulClient) RegisterService(s *ConsulService) error {
	j, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return toError("unmarshallable service registration payload: %s", err)
	}
	_, err = c.request("PUT", "agent/service/register", bytes.NewReader(j), 200)
	return err
}

func (c *ConsulClient) DeregisterService(id string) error {
	_, err := c.request("GET", "agent/service/deregister/"+id, nil, 200)
	return err
}

func (c *ConsulClient) ReportHealth(checkId string, r *Health) error {
	j, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return toError("unmarshallable check update payload: %s", err)
	}
	_, err = c.request("PUT", "agent/check/update/"+checkId, bytes.NewReader(j), 200)
	return err
}

func (c *ConsulClient) GetKey(k string) (string, error) {
	return c.request("GET", "kv/"+k+"?raw", nil, 200, 404)
}

func (c *ConsulClient) SetKey(k, v string) error {
	_, err := c.request("PUT", "kv/"+k, bytes.NewReader([]byte(v)), 200)
	return err
}

func (c *ConsulClient) request(method, path string, body io.Reader, successStatusCodes ...int) (string, error) {
	req, err := http.NewRequest(method, fmt.Sprintf("%s/v1/%s", c.address, path), body)
	if err != nil {
		return "", toError("invalid request: %s", err)
	}
	r, err := c.client.Do(req)
	if err != nil {
		return "", toError("request failed: %s", err)
	}
	success := false
	for _, successCode := range successStatusCodes {
		if r.StatusCode == successCode {
			success = true
			break
		}
	}
	if !success {
		return "", toError("status %d: %s %s", r.StatusCode, req.Method, req.URL)
	}
	buf := new(bytes.Buffer)
	buf.ReadFrom(r.Body)
	return buf.String(), nil
}

func toError(f string, v ...interface{}) error {
	return fmt.Errorf("consul: "+f, v...)
}
