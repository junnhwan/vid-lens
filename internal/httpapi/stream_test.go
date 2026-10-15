package httpapi

import (
	"strconv"
	"testing"
)

func TestSlowSubscribersReleaseAllPollingAndAdmissionState(t *testing.T) {
	for _, bytes := range []int{1, 600 << 10} {
		t.Run(strconv.Itoa(bytes), func(t *testing.T) {
			h := &Hub{groups: map[string]*group{}, sessions: map[string]int{}}
			ch, unsubscribe, ok := h.subscribe("visitor", "request", "request-id", "")
			if !ok {
				t.Fatal("initial subscription rejected")
			}
			key := "visitor:request:request-id"
			g := h.groups[key]
			for n := 0; n < 33; n++ {
				h.publish(g, strconv.Itoa(n), update{Bytes: bytes})
			}
			if h.count != 0 || len(h.groups) != 0 || len(h.sessions) != 0 {
				t.Fatal("slow subscriber retained its polling/admission state")
			}
			for range ch {
			} // A slow connection is closed, never left waiting on an orphan.
			_, nextUnsubscribe, ok := h.subscribe("visitor", "request", "request-id", "")
			if !ok {
				t.Fatal("released slot could not be reused")
			}
			unsubscribe()
			if h.count != 1 || len(h.groups) != 1 {
				t.Fatal("old unsubscribe removed the replacement subscription")
			}
			nextUnsubscribe()
			nextUnsubscribe()
			if h.count != 0 || len(h.groups) != 0 || len(h.sessions) != 0 {
				t.Fatal("unsubscribe was not idempotent")
			}
		})
	}
}
