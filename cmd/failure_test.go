package cmd

import (
	"errors"
	"testing"
	"time"
)

func TestClassifyKeepaliveError(t *testing.T) {
	tests := []struct {
		message string
		kind    failureKind
		relogin bool
		fatal   bool
	}{
		{"ZTE getToken failed: code=1000100 session invalid", failureAuth, true, false},
		// Observed 2026-10: an expired SOHO token came back as code 4015 and was
		// previously retried forever as an unclassified failure.
		{"getFirmAuth failed: getFirmAuth failed: code=4015, msg=用户未登录，请先登录", failureAuth, true, false},
		{"getFirmAuth failed: code=4015", failureAuth, true, false},
		{"getFirmAuth failed: 503 service unavailable", failureMaintenance, false, false},
		{"ZTE display readiness timeout", failureProtocol, false, false},
		{"raw connection reset by peer", failureNetwork, false, false},
		{"password is required to refresh login", failureConfig, false, true},
	}
	for _, test := range tests {
		decision := classifyKeepaliveError(errors.New(test.message))
		if decision.kind != test.kind || decision.relogin != test.relogin || decision.fatal != test.fatal {
			t.Fatalf("%q classified as %+v", test.message, decision)
		}
	}
}

func TestRetryDelayCaps(t *testing.T) {
	decision := retryDecision{baseDelay: 5 * time.Second, maxDelay: 20 * time.Second}
	for _, attempt := range []int{3, 4, 20} {
		delay := retryDelay(decision, attempt)
		if delay < 20*time.Second || delay > 24*time.Second {
			t.Fatalf("attempt %d delay %s outside capped jitter range", attempt, delay)
		}
	}
}
