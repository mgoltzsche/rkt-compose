package launcher

import (
	"fmt"
	"github.com/mgoltzsche/log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type HealthChecks struct {
	checks            []*HealthCheck
	reporter          HealthReporter
	minReportInterval time.Duration
	currentStatus     *HealthCheckResults
	statusCounts      [3]uint
	checkResults      []*HealthCheckResult
	statusChan        chan *HealthCheckResult
	quitChan          chan bool
	wait              sync.WaitGroup
	waitReporter      sync.WaitGroup
	debug             log.Logger
}

type HealthCheck struct {
	name     string
	interval time.Duration
	timeout  time.Duration
	test     HealthIndicator
}

type HealthStatus byte

var statusNameMap = [3]string{"passing", "warning", "critical"}

func (s HealthStatus) String() string {
	return statusNameMap[s]
}

const (
	STATUS_PASSING  HealthStatus = 0
	STATUS_WARNING  HealthStatus = 1
	STATUS_CRITICAL HealthStatus = 2
)

type HealthCheckResults struct {
	status HealthStatus
	output string
}

func (r *HealthCheckResults) Status() HealthStatus {
	return r.status
}

func (r *HealthCheckResults) Output() string {
	return r.output
}

type HealthCheckResult struct {
	index  uint
	name   string
	status HealthStatus
	output string
}

type HealthIndicator func() (HealthStatus, string)

type HealthReporter func(r *HealthCheckResults) error

func NewHealthChecks(debug log.Logger, reporter HealthReporter, minReportInterval time.Duration, checks ...*HealthCheck) *HealthChecks {
	c := &HealthChecks{}
	c.checks = checks
	c.reporter = reporter
	c.minReportInterval = minReportInterval
	c.debug = debug
	return c
}

func (c *HealthChecks) Start() {
	if c.statusChan != nil {
		panic("healthchecks already started")
	}
	checkCount := len(c.checks)
	c.currentStatus = &HealthCheckResults{STATUS_CRITICAL, "starting"}
	c.statusCounts = [3]uint{0, 0, uint(checkCount)}
	c.checkResults = make([]*HealthCheckResult, checkCount)
	for i := 0; i < checkCount; i++ {
		c.checkResults[i] = &HealthCheckResult{}
		c.checkResults[i].name = c.checks[i].name
		c.checkResults[i].status = STATUS_CRITICAL
		c.checkResults[i].output = "starting"
	}
	c.debug.Println("Starting health checks...")
	c.quitChan = make(chan bool, checkCount)
	c.statusChan = make(chan *HealthCheckResult)
	c.waitReporter.Add(1)
	c.wait.Add(checkCount)
	c.debug.Println("Starting health reporter...")
	go c.report(c.statusChan, c.quitChan)
	for i := 0; i < checkCount; i++ {
		check := c.checks[i]
		c.debug.Printf("Starting check %q...", check.name)
		go check.run(uint(i), c.statusChan, c.quitChan, &c.wait, c.debug)
	}
}

func (c *HealthChecks) Stop() {
	c.debug.Println("Stopping health checks...")
	reporter := c.reporter
	c.reporter = func(r *HealthCheckResults) error { return nil }
	close(c.quitChan)   // Stop check goroutines
	c.wait.Wait()       // Wait for check goroutines
	close(c.statusChan) // Stop reporter goroutine
	c.statusChan = nil
	c.quitChan = nil
	c.waitReporter.Wait() // Wait for reporter goroutine to terminate
	c.reporter = reporter
	c.doReportStatus()
}

func (c *HealthChecks) report(status <-chan *HealthCheckResult, quit <-chan bool) {
	defer c.waitReporter.Done()
	ticker := time.NewTicker(c.minReportInterval)
	resetTicker := func() {}
	if c.minReportInterval > 0 {
		resetTicker = func() {
			ticker.Stop()
			ticker = time.NewTicker(c.minReportInterval)
		}
	} else {
		ticker.Stop()
	}
	for {
		select {
		case s, ok := <-status:
			if ok {
				c.debug.Printf("Check %q %s", s.name, s.status)
				if c.updateStatus(s) {
					resetTicker()
					c.doReportStatus()
				}
			} else {
				ticker.Stop()
				return
			}
		case <-ticker.C:
			c.doReportStatus()
		}
	}
}

func (c *HealthChecks) doReportStatus() {
	err := c.reporter(c.currentStatus)
	if err != nil {
		os.Stderr.WriteString(fmt.Sprintf("Error: health reporter: %s\n", err))
	}
}

func (c *HealthChecks) updateStatus(r *HealthCheckResult) (changed bool) {
	last := c.checkResults[r.index]
	c.checkResults[r.index] = r
	if last.status != r.status {
		c.statusCounts[last.status]--
		c.statusCounts[r.status]++
		changed = true
	}
	status := c.currentStatus.status
	for i := byte(2); i >= 0; i-- {
		if c.statusCounts[i] > 0 {
			s := HealthStatus(i)
			if s != status {
				status = s
				changed = true
			}
			break
		}
	}
	c.currentStatus = &HealthCheckResults{status, c.combinedOutput()}
	return
}

func (c *HealthChecks) combinedOutput() string {
	msg := make([]string, len(c.checkResults))
	for i, r := range c.checkResults {
		msg[i] = fmt.Sprintf("%s %s - %s", r.name, r.status, strings.Replace(r.output, "\n", "\n  ", -1))
	}
	return strings.Join(msg, "\n")
}

func NewHealthCheck(name string, interval, timeout time.Duration, indicator HealthIndicator) *HealthCheck {
	return &HealthCheck{name, interval, timeout, indicator}
}

func (c *HealthCheck) run(index uint, status chan<- *HealthCheckResult, quit <-chan bool, wait *sync.WaitGroup, debug log.Logger) {
	defer wait.Done()
	defer func() { status <- &HealthCheckResult{index, c.name, STATUS_CRITICAL, "check terminated"} }()
	for i := 0; i < 30; i++ {
		select {
		case <-time.After(time.Second):
			r, out := c.test()
			if r != STATUS_CRITICAL {
				status <- &HealthCheckResult{index, c.name, r, out}
				i = 30
			}
		case <-quit:
			return
		}
	}
	ticker := time.NewTicker(c.interval)
	for {
		select {
		case <-ticker.C:
			r, out := c.test()
			status <- &HealthCheckResult{index, c.name, r, out}
		case <-quit:
			ticker.Stop()
			return
		}
	}
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
