package launcher

import (
	"os/exec"
	"strings"
	"sync"
	"time"
)

type HealthChecks struct {
	checks       []*HealthCheck
	reporter     HealthReporter
	status       chan *HealthCheckResult
	quit         chan bool
	wait         sync.WaitGroup
	waitReporter sync.WaitGroup
}

type HealthCheck struct {
	name     string
	interval time.Duration
	timeout  time.Duration
	test     HealthIndicator
}

type HealthStatus string

const (
	HEALTH_UP   HealthStatus = "up"
	HEALTH_WARN HealthStatus = "warn"
	HEALTH_DOWN HealthStatus = "down"
)

type HealthCheckResult struct {
	Name   string
	Status HealthStatus
	Output string
}

type HealthIndicator func() (HealthStatus, string)

type HealthReporter func(r *HealthCheckResult)

func NewHealthChecks(reporter HealthReporter, checks ...*HealthCheck) *HealthChecks {
	c := &HealthChecks{}
	c.checks = checks
	c.reporter = reporter
	return c
}

func (c *HealthChecks) Start() {
	if c.status != nil {
		panic("healthchecks already started")
	}
	c.quit = make(chan bool, len(c.checks))
	c.status = make(chan *HealthCheckResult)
	c.waitReporter.Add(1)
	c.wait.Add(len(c.checks))
	go c.report(c.status, c.quit)
	for _, check := range c.checks {
		go check.run(c.status, c.quit, &c.wait)
	}
}

func (c *HealthChecks) Stop() {
	close(c.quit)   // Stop check goroutines
	c.wait.Wait()   // Wait for check goroutines
	close(c.status) // Stop stop reporter goroutine
	c.quit = nil
	c.status = nil
	c.waitReporter.Wait() // Wait for reporter goroutine to terminate
}

func (c *HealthChecks) report(status <-chan *HealthCheckResult, quit <-chan bool) {
	defer c.waitReporter.Done()
	for {
		if s, ok := <-status; ok {
			c.reporter(s)
		} else {
			return
		}
	}
}

func NewHealthCheck(name string, interval, timeout time.Duration, indicator HealthIndicator) *HealthCheck {
	return &HealthCheck{name, interval, timeout, indicator}
}

func (c *HealthCheck) run(status chan<- *HealthCheckResult, quit <-chan bool, wait *sync.WaitGroup) {
	defer wait.Done()
	select { // Run for the 1st time after 5 seconds
	case <-time.After(5 * time.Second):
		c.check(status)
	case <-quit:
		return
	}
	ticker := time.NewTicker(c.interval)
	for {
		select {
		case <-ticker.C:
			c.check(status)
		case <-quit:
			ticker.Stop()
			return
		}
	}
}

func (c *HealthCheck) check(status chan<- *HealthCheckResult) {
	r, out := c.test()
	status <- &HealthCheckResult{c.name, r, out}
}

func CommandBasedHealthIndicator(args ...string) HealthIndicator {
	c := args[0]
	a := args[1:]
	return func() (HealthStatus, string) {
		cmd := exec.Command(c, a...)
		out, e := cmd.CombinedOutput()
		status := HEALTH_DOWN
		if e == nil {
			status = HEALTH_UP
		}
		return status, strings.Trim(string(out), "\n")
	}
}
