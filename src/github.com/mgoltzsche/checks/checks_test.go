package checks

import (
	"fmt"
	"github.com/mgoltzsche/log"
	"strings"
	"testing"
	"time"
)

var checkCount = 0
var reportCount uint
var reported *HealthCheckResults

func TestHealthChecksWithEmptyChecksDoesInitialReport(t *testing.T) {
	reportCount = 0
	reported = nil
	testee := NewHealthChecks(log.NewNopLogger(), mockHealthReporter, duration("10s"))
	testee.Start()
	if reportCount != 1 {
		t.Errorf("Did not report 1 time but %d times", reportCount)
	}
	if reported.Status() != STATUS_PASSING {
		t.Errorf("Reported invalid status: %s", reported.Status())
	}
	testee.Stop()
	if reportCount != 2 {
		t.Errorf("Did not report check termination")
		return
	}
	if reported.Status() != STATUS_CRITICAL {
		t.Errorf("Reported invalid status on health check termination: %s", reported.Status())
	}
}

func TestHealthChecksStatus(t *testing.T) {
	cases := []struct {
		eStatus HealthStatus
		eOut    string
		c       []*HealthCheck
	}{
		{
			STATUS_PASSING,
			"success",
			[]*HealthCheck{
				createCheck(STATUS_PASSING, "success"),
				createCheck(STATUS_PASSING, "success2"),
			},
		},
		{
			STATUS_WARNING,
			"one warning",
			[]*HealthCheck{
				createCheck(STATUS_PASSING, "success1"),
				createCheck(STATUS_WARNING, "one warning"),
				createCheck(STATUS_PASSING, "success3"),
			},
		},
		{
			STATUS_CRITICAL,
			"one failed",
			[]*HealthCheck{
				createCheck(STATUS_PASSING, "success1"),
				createCheck(STATUS_CRITICAL, "one failed"),
				createCheck(STATUS_PASSING, "success3"),
			},
		},
		{
			STATUS_CRITICAL,
			"critical with warning",
			[]*HealthCheck{
				createCheck(STATUS_PASSING, "successs1"),
				createCheck(STATUS_WARNING, "warn1"),
				createCheck(STATUS_CRITICAL, "critical with warning"),
			},
		},
		{
			STATUS_CRITICAL,
			"completely failed",
			[]*HealthCheck{
				createCheck(STATUS_CRITICAL, "failure1"),
				createCheck(STATUS_CRITICAL, "completely failed"),
				createCheck(STATUS_CRITICAL, "failure3"),
			},
		},
	}
	for _, c := range cases {
		reportCount = 0
		reported = nil
		testee := NewHealthChecks(log.NewNopLogger(), mockHealthReporter, duration("1ms"), c.c...)
		testee.Start()
		<-time.After(duration("10ms"))
		if reportCount == 0 {
			t.Errorf("Case %s: Did not report", c.eOut)
			return
		}
		if reported.Status() != c.eStatus {
			t.Errorf("Case %s: Reported invalid status: %s", c.eOut, reported.Status())
		}
		if strings.Index(reported.Output(), c.eOut) < 0 {
			t.Errorf("Case %s: Report output does not contain check output: %s", c.eOut, reported.Output())
		}
		testee.Stop()
		if reported.Status() != STATUS_CRITICAL {
			t.Errorf("Case %s: Reported invalid status on health check termination: %s", c.eOut, reported.Status())
		}
	}
}

func TestHealthChecksMinInterval(t *testing.T) {
	reportCount = 0
	reported = nil
	ck1 := createCheck(STATUS_PASSING, "success1")
	ck2 := createCheck(STATUS_PASSING, "success2")
	testee := NewHealthChecks(log.NewNopLogger(), mockHealthReporter, duration("30ms"), ck1, ck2)
	testee.Start()
	<-time.After(duration("100ms"))
	if reportCount != 4 {
		t.Errorf("Did not report 3 times but %d times", reportCount)
		return
	}
	if reported.Status() != STATUS_PASSING {
		t.Errorf("Reported invalid status: %s", reported.Status())
	}
	if strings.Index(reported.Output(), "success2") < 0 {
		t.Errorf("Report output does not contain check output: %s", reported.Output())
	}
	testee.Stop()
	if reported.Status() != STATUS_CRITICAL {
		t.Errorf("Reported invalid status on health check termination: %s", reported.Status())
	}
}

func createCheck(status HealthStatus, output string) *HealthCheck {
	checkCount++
	return NewHealthCheck(fmt.Sprintf("ck-%d", checkCount), duration("5ms"), func() *HealthCheckResult { return NewHealthCheckResult(status, output) })
}

func mockHealthReporter(r *HealthCheckResults) error {
	if r == nil {
		panic("Health reporter called with nil argument")
	}
	reported = r
	reportCount++
	return nil
}

func duration(d string) time.Duration {
	r, err := time.ParseDuration(d)
	if err != nil {
		panic("Invalid duration: " + d)
	}
	return r
}
