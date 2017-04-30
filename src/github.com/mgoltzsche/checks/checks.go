package checks

import (
	"fmt"
	"github.com/mgoltzsche/log"
	"io/ioutil"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
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

func NewHealthCheckResult(status HealthStatus, output string) *HealthCheckResult {
	return &HealthCheckResult{0, "", status, output}
}

type HealthIndicator func() *HealthCheckResult

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
	if checkCount > 0 {
		c.currentStatus = &HealthCheckResults{STATUS_CRITICAL, "starting"}
	} else {
		c.currentStatus = &HealthCheckResults{STATUS_PASSING, "rkt-compose running"}
		c.doReportStatus()
	}
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
	c.currentStatus.status = STATUS_CRITICAL
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
			if !ok {
				ticker.Stop()
				return
			}
			c.debug.Printf("Check %q %s", s.name, s.status)
			if c.updateStatus(s) {
				resetTicker()
				c.doReportStatus()
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
	if len(c.checkResults) == 1 {
		return c.checkResults[0].output
	} else {
		msg := make([]string, len(c.checkResults))
		for i, r := range c.checkResults {
			if len(r.output) > 0 {
				msg[i] = fmt.Sprintf("%s %s - %s", r.name, r.status, strings.Replace(r.output, "\n", "\n  ", -1))
			} else {
				msg[i] = fmt.Sprintf("%s %s", r.name, r.status)
			}
		}
		return strings.Join(msg, "\n")
	}
}

func NewHealthCheck(name string, interval time.Duration, indicator HealthIndicator) *HealthCheck {
	return &HealthCheck{name, interval, indicator}
}

func (c *HealthCheck) run(index uint, status chan<- *HealthCheckResult, quit <-chan bool, wait *sync.WaitGroup, debug log.Logger) {
	defer wait.Done()
	defer func() { status <- &HealthCheckResult{index, c.name, STATUS_CRITICAL, "check terminated"} }()
	initInterval := time.Duration(math.Min(float64(time.Second), float64(c.interval)))
	for i := 0; i < 10; i++ {
		select {
		case <-time.After(initInterval):
			r := c.test()
			r.index = index
			r.name = c.name
			status <- r
			if r.status != STATUS_CRITICAL {
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
			r := c.test()
			r.index = index
			r.name = c.name
			status <- r
		case <-quit:
			ticker.Stop()
			return
		}
	}
}

func NewCommandBasedHealthIndicator(debug log.Logger, timeout time.Duration, args ...string) HealthIndicator {
	c := args[0]
	a := args[1:]
	return func() *HealthCheckResult {
		cmd := exec.Command(c, a...)
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return NewHealthCheckResult(STATUS_CRITICAL, "Cannot create health check indicator stderr pipe: "+err.Error())
		}
		err = cmd.Start()
		if err != nil {
			return NewHealthCheckResult(STATUS_CRITICAL, "Cannot start health check indicator cmd: "+err.Error())
		}
		done := make(chan *HealthCheckResult, 1)
		go func() {
			outb, _ := ioutil.ReadAll(stderr)
			out := strings.Trim(string(outb), "\n")
			err := cmd.Wait()
			status := STATUS_CRITICAL
			if err == nil {
				status = STATUS_PASSING
			} else if len(out) == 0 {
				out = fmt.Sprintf("%s", err)
			}
			done <- NewHealthCheckResult(status, out)
		}()
		select {
		case r := <-done:
			close(done)
			return r
		case <-time.After(timeout):
			stderr.Close() // If not closed here process doesn't get killed
			cmd.Process.Signal(syscall.SIGINT)
			cmd.Process.Kill()
			r := <-done
			close(done)
			return NewHealthCheckResult(STATUS_CRITICAL, "Indicator timed out - "+r.output)
		}
	}
}
