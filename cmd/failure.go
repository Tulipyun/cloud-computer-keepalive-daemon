package cmd

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"time"
)

type failureKind string

const (
	failureAuth        failureKind = "authentication"
	failureMaintenance failureKind = "platform_maintenance"
	failureNetwork     failureKind = "network"
	failureProtocol    failureKind = "protocol"
	failureConfig      failureKind = "configuration"
	failureUnknown     failureKind = "unknown"
)

type retryDecision struct {
	kind        failureKind
	relogin     bool
	fatal       bool
	baseDelay   time.Duration
	maxDelay    time.Duration
	description string
}

func classifyKeepaliveError(err error) retryDecision {
	text := strings.ToLower(err.Error())

	if containsAny(text,
		"missing encrypted secret", "unsupported secret scheme", "password is required",
		"sub account cannot be empty", "missing soho token", "load config", "save config",
	) {
		return retryDecision{failureConfig, false, true, 0, 0, "local configuration requires attention"}
	}
	if containsAny(text,
		"1000100", "session invalid", "token expired", "unauthorized", "forbidden",
		"login failed", "oauth/token failed", "missing token or user id", "invalid token",
		// SOHO reports an expired or rejected token as business code 4015 with
		// the message "用户未登录，请先登录". Without this entry a stale token is
		// classified as unknown and retried forever instead of re-logging in.
		"4015", "用户未登录", "not logged in",
	) {
		return retryDecision{failureAuth, true, false, 3 * time.Second, 30 * time.Second, "credentials or session must be refreshed"}
	}
	if containsAny(text,
		"502", "503", "504", "gateway timeout", "service unavailable", "maintenance", "维护",
	) {
		return retryDecision{failureMaintenance, false, false, 30 * time.Second, 5 * time.Minute, "platform maintenance or upstream outage"}
	}
	if containsAny(text,
		"spice", "cag", "redq", "display readiness", "display channel", "surface_create",
		"draw_copy", "mark", "channels_list", "main_init", "firm auth is missing",
	) {
		return retryDecision{failureProtocol, false, false, 10 * time.Second, 2 * time.Minute, "protocol session did not become healthy"}
	}
	var netErr net.Error
	if errors.As(err, &netErr) || containsAny(text,
		"connection reset", "connection refused", "broken pipe", "network is unreachable",
		"no route to host", "server closed connection", "unexpected eof", "i/o timeout",
	) {
		return retryDecision{failureNetwork, false, false, 5 * time.Second, 2 * time.Minute, "transient transport failure"}
	}
	return retryDecision{failureUnknown, false, false, 15 * time.Second, 5 * time.Minute, "unclassified failure"}
}

func retryDelay(decision retryDecision, consecutive int) time.Duration {
	if decision.fatal || decision.baseDelay <= 0 {
		return 0
	}
	if consecutive < 1 {
		consecutive = 1
	}
	shift := consecutive - 1
	if shift > 5 {
		shift = 5
	}
	delay := decision.baseDelay * time.Duration(1<<shift)
	if delay > decision.maxDelay {
		delay = decision.maxDelay
	}
	return delay + retryJitter(delay/5)
}

func retryJitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return 0
	}
	return time.Duration(binary.LittleEndian.Uint64(raw[:]) % uint64(max+1))
}

func containsAny(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}
