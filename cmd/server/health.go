package main

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const readinessTimeout = 2 * time.Second

type dependencyCheck struct {
	Name     string
	Required bool
	Check    func(context.Context) error
}

func livenessHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "VidLens"})
	}
}

func readinessHandler(checks []dependencyCheck, timeout time.Duration) gin.HandlerFunc {
	if timeout <= 0 {
		timeout = readinessTimeout
	}
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()

		statuses := make(map[string]string, len(checks))
		hasRequiredFailure := false
		hasOptionalFailure := false
		for _, dependency := range checks {
			status := "up"
			if dependency.Check == nil || dependency.Check(ctx) != nil {
				status = "down"
				if dependency.Required {
					hasRequiredFailure = true
				} else {
					hasOptionalFailure = true
				}
			}
			statuses[dependency.Name] = status
		}

		status := "ready"
		statusCode := http.StatusOK
		if hasRequiredFailure {
			status = "not_ready"
			statusCode = http.StatusServiceUnavailable
		} else if hasOptionalFailure {
			status = "degraded"
		}
		c.JSON(statusCode, gin.H{
			"status":       status,
			"service":      "VidLens",
			"dependencies": statuses,
		})
	}
}
