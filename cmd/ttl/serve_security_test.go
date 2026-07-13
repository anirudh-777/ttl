package main

import (
	"net"
	"testing"
)

func TestUnsafeWebhookIP(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "::1", "10.0.0.1", "172.16.0.1", "192.168.1.1", "169.254.169.254", "0.0.0.0"} {
		if !unsafeWebhookIP(net.ParseIP(raw)) {
			t.Errorf("%s should be rejected", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "2606:4700:4700::1111"} {
		if unsafeWebhookIP(net.ParseIP(raw)) {
			t.Errorf("%s should be allowed", raw)
		}
	}
}
