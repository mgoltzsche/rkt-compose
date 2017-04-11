package launcher

import (
	"fmt"
	"os"
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
	STATUS_PASSING  HealthStatus = "passing"
	STATUS_WARNING  HealthStatus = "warning"
	STATUS_CRITICAL HealthStatus = "critical"
)

type HealthCheckResult struct {
	Name   string
	Status HealthStatus
	Output string
}

type HealthIndicator func() (HealthStatus, string)

type HealthReporter func(r *HealthCheckResult) error

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
	c.status = nil
	c.quit = nil
	c.waitReporter.Wait() // Wait for reporter goroutine to terminate
}

func (c *HealthChecks) report(status <-chan *HealthCheckResult, quit <-chan bool) {
	defer c.waitReporter.Done()
	defer handleError("health reporter")
	for {
		if s, ok := <-status; ok {
			err := c.reporter(s)
			if err != nil {
				os.Stderr.WriteString(fmt.Sprintf("Error: health reporter: %s", err))
			}
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
	defer handleError("health check")
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
		status := STATUS_CRITICAL
		if e == nil {
			status = STATUS_PASSING
		}
		return status, strings.Trim(string(out), "\n")
	}
}

func handleError(opName string) {
	if e := recover(); e != nil {
		os.Stderr.WriteString(fmt.Sprintf("error: %s terminated: %s\n", opName, e))
	}
}
