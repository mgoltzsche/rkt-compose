package launcher

import (
	"errors"
	"fmt"
	"github.com/mgoltzsche/checks"
	"github.com/mgoltzsche/log"
	"time"
)

type ConsulLifecycle struct {
	descriptor        *Pod
	podUUID           string
	client            *ConsulClient
	checks            *checks.HealthChecks
	minReportInterval time.Duration
	checkTTL          time.Duration
	service           *ConsulService
	debug             log.Logger
}

func NewConsulLifecycleFactory(address string, checkTTL time.Duration, debug log.Logger) (LifecycleListenerFactory, error) {
	client := NewConsulClient(address)
	if !client.CheckAvailability(30) {
		return nil, errors.New("Consul unavailable")
	}
	return func(pod *Pod) LifecycleListener {
		// Health checks done within the launcher to be able to run commands within the container
		minReportInterval := checkTTL / 2
		c := &ConsulLifecycle{pod, "", client, nil, minReportInterval, checkTTL, nil, debug}
		return c
	}, nil
}

func (c *ConsulLifecycle) Start(podUUID, podIP string) (err error) {
	c.podUUID = podUUID
	c.checks, err = toHealthChecks(c.descriptor, podUUID, c.reportHealth, c.minReportInterval, c.debug)
	if err != nil {
		return
	}
	tags := toTags(c.descriptor.Services)
	checkTTL := c.checkTTL.String()
	checkNote := fmt.Sprintf("Aggregated checks (Interval: %s, TTL: %s)", c.minReportInterval.String(), checkTTL)
	check := HeartBeat{checkNote, checkTTL}
	service := &ConsulService{c.serviceId(), c.descriptor.Name, podIP, tags, false, check}
	err = c.client.RegisterService(service)
	if err != nil {
		return
	}
	err = c.registerSharedKeys()
	if err != nil {
		c.client.DeregisterService(c.serviceId())
		return
	}
	c.checks.Start()
	return nil
}

func (c *ConsulLifecycle) Terminate() error {
	c.checks.Stop()
	serviceId := c.serviceId()
	c.debug.Printf("Deregistering service %q...", serviceId)
	if err := c.client.DeregisterService(serviceId); err != nil {
		return fmt.Errorf("Failed to deregister consul service %q", serviceId)
	}
	return nil
}

func (c *ConsulLifecycle) reportHealth(r *checks.HealthCheckResults) error {
	status := r.Status().String()
	c.debug.Printf("Reporting status %s...", status)
	return c.client.ReportHealth("service:"+c.serviceId(), &Health{ConsulHealthStatus(status), r.Output()})
}

func (c *ConsulLifecycle) serviceId() string {
	return "rkt-" + c.podUUID
}

func (c *ConsulLifecycle) registerSharedKeys() error {
	for k, v := range c.descriptor.SharedKeys {
		pubVal, err := c.client.GetKey(k)
		if err != nil {
			return err
		}
		if pubVal != "" && pubVal != v && !c.descriptor.SharedKeysOverrideAllowed {
			return fmt.Errorf("Shared key %q is already set and key override is disabled", k)
		}
		err = c.client.SetKey(k, v)
		if err != nil {
			return err
		}
	}
	return nil
}

func toHealthChecks(pod *Pod, podUUID string, reporter checks.HealthReporter, minReportInterval time.Duration, debug log.Logger) (*checks.HealthChecks, error) {
	c := []*checks.HealthCheck{}
	i := 1
	for k, s := range pod.Services {
		h := s.HealthCheck
		if h != nil && len(h.Command) > 0 {
			indicator, err := toHealthIndicator(pod, k, podUUID, h, debug)
			if err != nil {
				return nil, err
			}
			check := checks.NewHealthCheck(k, time.Duration(h.Interval), indicator)
			c = append(c, check)
			i++
		}
	}
	return checks.NewHealthChecks(debug, reporter, minReportInterval, c...), nil
}

func toHealthIndicator(pod *Pod, app, podUUID string, h *HealthCheckDescriptor, debug log.Logger) (checks.HealthIndicator, error) {
	switch {
	case len(h.Command) > 0:
		cmd := append([]string{"rkt", "enter", "--app=" + app, podUUID}, h.Command...)
		return checks.NewCommandBasedHealthIndicator(debug, time.Duration(h.Timeout), cmd...), nil
	case len(h.Http) > 0:
		return nil, errors.New("HTTP health check unsupported")
	default:
		return nil, fmt.Errorf("no health check indicator defined for %q", app)
	}
}

func toTags(m map[string]*Service) []string {
	t := make([]string, len(m))
	i := 0
	for k := range m {
		t[i] = k
		i++
	}
	return t
}
