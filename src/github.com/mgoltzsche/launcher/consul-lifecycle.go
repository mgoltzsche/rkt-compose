package launcher

import (
	"errors"
	"fmt"
	"github.com/mgoltzsche/checks"
	"github.com/mgoltzsche/log"
	"github.com/mgoltzsche/model"
	"time"
)

type ConsulLifecycle struct {
	descriptor        *model.PodDescriptor
	podUUID           string
	client            *ConsulClient
	checks            *checks.HealthChecks
	minReportInterval time.Duration
	service           *Service
	debug             log.Logger
}

func NewConsulLifecycleFactory(consulAddress string, checkTtl time.Duration, debug log.Logger) (LifecycleListenerFactory, error) {
	client := NewConsulClient(consulAddress)
	if !client.CheckAvailability(30) {
		return nil, errors.New("Consul unavailable")
	}
	return func(pod *model.PodDescriptor) LifecycleListener {
		// Health checks done within the launcher to be able to run commands within the container
		tags := toTags(pod.Services)
		minReportInterval := time.Duration(checkTtl / 2)
		checkNote := fmt.Sprintf("aggregated checks (Interval: %s, TTL: %s)", minReportInterval.String(), checkTtl.String())
		service := &Service{pod.Name, "", tags, false, HeartBeat{checkNote, checkTtl.String()}}
		c := &ConsulLifecycle{pod, "", client, nil, minReportInterval, service, debug}
		return c
	}, nil
}

func (c *ConsulLifecycle) Start(podUUID, podIP string) (err error) {
	c.podUUID = podUUID
	c.checks, err = toHealthChecks(c.descriptor, podUUID, c.reportHealth, c.minReportInterval, c.debug)
	if err != nil {
		return
	}
	c.service.Address = podIP
	c.debug.Printf("Setting IP of consul service %q to %s...", c.descriptor.Name, podIP)
	err = c.client.RegisterService(c.service)
	if err != nil {
		return
	}
	// TODO: create health checks before container start to raise possible errors early
	c.checks.Start()
	return nil
}

func (c *ConsulLifecycle) Terminate() error {
	c.checks.Stop()
	c.debug.Printf("Deregistering service %q...", c.descriptor.Name)
	if err := c.client.DeregisterService(c.descriptor.Name); err != nil {
		return fmt.Errorf("Failed to deregister consul service %q", c.descriptor.Name)
	}
	return nil
}

func (c *ConsulLifecycle) reportHealth(r *checks.HealthCheckResults) error {
	status := r.Status().String()
	c.debug.Printf("Reporting status %s...", status)
	return c.client.ReportHealth("service:"+c.descriptor.Name, &Health{ConsulHealthStatus(status), r.Output()})
}

func toHealthChecks(pod *model.PodDescriptor, podUUID string, reporter checks.HealthReporter, minReportInterval time.Duration, debug log.Logger) (*checks.HealthChecks, error) {
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

func toHealthIndicator(pod *model.PodDescriptor, app, podUUID string, h *model.HealthCheckDescriptor, debug log.Logger) (checks.HealthIndicator, error) {
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

func toTags(m map[string]*model.ServiceDescriptor) []string {
	t := make([]string, len(m))
	i := 0
	for k := range m {
		t[i] = k
		i++
	}
	return t
}
