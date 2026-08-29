package handler

import (
	"errors"
	"net/http"
	"testing"

	"vid-lens/internal/service"
)

func TestDeleteTaskHTTPStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{name: "not found", err: service.ErrTaskNotFound, want: http.StatusNotFound},
		{name: "forbidden", err: service.ErrTaskForbidden, want: http.StatusForbidden},
		{name: "active", err: service.ErrTaskActive, want: http.StatusConflict},
		{name: "wrapped active", err: errors.Join(errors.New("request"), service.ErrTaskActive), want: http.StatusConflict},
		{name: "internal", err: errors.New("database unavailable"), want: http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deleteTaskHTTPStatus(tc.err); got != tc.want {
				t.Fatalf("deleteTaskHTTPStatus() = %d, want %d", got, tc.want)
			}
		})
	}
}
