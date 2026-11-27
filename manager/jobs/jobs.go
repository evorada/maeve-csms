// SPDX-License-Identifier: Apache-2.0

// Package jobs runs named units of periodic background work.
package jobs

import (
	"context"
	"fmt"
	"regexp"
	"time"
)

// Job is a named unit of periodic background work.
type Job struct {
	Name     string                          // unique among the jobs of a Runner, [a-z0-9-]+
	Interval time.Duration                   // pause between the end of one run and the start of the next (> 0)
	Run      func(ctx context.Context) error // must return when ctx is cancelled
}

// Runner runs jobs until ctx is cancelled.
type Runner interface {
	Run(ctx context.Context, jobs []Job) error
}

var jobNamePattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// Validate checks names are unique and well-formed, intervals are positive
// and every job has a Run function.
func Validate(jobs []Job) error {
	seen := make(map[string]struct{}, len(jobs))
	for _, job := range jobs {
		if !jobNamePattern.MatchString(job.Name) {
			return fmt.Errorf("invalid job name %q: must match [a-z0-9-]+", job.Name)
		}
		if _, ok := seen[job.Name]; ok {
			return fmt.Errorf("duplicate job name %q", job.Name)
		}
		seen[job.Name] = struct{}{}
		if job.Interval <= 0 {
			return fmt.Errorf("job %q: interval must be positive, got %s", job.Name, job.Interval)
		}
		if job.Run == nil {
			return fmt.Errorf("job %q: no run function", job.Name)
		}
	}
	return nil
}
