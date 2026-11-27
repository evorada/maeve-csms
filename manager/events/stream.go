// SPDX-License-Identifier: Apache-2.0

package events

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

const (
	// DefaultStreamName is the name of the stream of the domain events.
	DefaultStreamName = "CPMS_EVENTS"
	// DefaultSubjectPrefix is the first token of the subjects of the domain events.
	DefaultSubjectPrefix = "cpms"
	// DefaultStreamMaxAge is how long the stream keeps an event.
	DefaultStreamMaxAge = 30 * 24 * time.Hour
	// DefaultDuplicateWindow is how long the stream remembers the id of a message to drop
	// a second message with the same id. A relay run that published a batch but could not
	// mark it publishes it again on one of the next runs, a few seconds later.
	DefaultDuplicateWindow = 2 * time.Minute
)

// StreamConfig describes the stream that captures the domain events.
type StreamConfig struct {
	Name       string        // DefaultStreamName
	Subjects   []string      // e.g. ["cpms.>"]
	MaxAge     time.Duration // DefaultStreamMaxAge
	Replicas   int           // 1 in development, 3 in production
	Duplicates time.Duration // de-duplication window, DefaultDuplicateWindow
}

// EnsureStream creates the stream or updates it to cfg (development and tests): limits
// retention, file storage, the oldest events are discarded first.
func EnsureStream(ctx context.Context, js jetstream.JetStream, cfg StreamConfig) error {
	_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:       cfg.Name,
		Subjects:   cfg.Subjects,
		Retention:  jetstream.LimitsPolicy,
		Storage:    jetstream.FileStorage,
		Discard:    jetstream.DiscardOld,
		MaxAge:     cfg.MaxAge,
		Replicas:   cfg.Replicas,
		Duplicates: cfg.Duplicates,
	})
	if err != nil {
		return fmt.Errorf("create or update stream %s: %w", cfg.Name, err)
	}
	return nil
}

// CheckStream verifies that the stream exists and captures cfg.Subjects (production,
// where the stream is managed by the infrastructure). It only reads the stream. The
// caller logs the error and starts anyway: until the stream is there, publishing fails
// and the events wait in the outbox.
func CheckStream(ctx context.Context, js jetstream.JetStream, cfg StreamConfig) error {
	stream, err := js.Stream(ctx, cfg.Name)
	if err != nil {
		return fmt.Errorf("stream %s: %w", cfg.Name, err)
	}
	captured := stream.CachedInfo().Config.Subjects
	for _, subject := range cfg.Subjects {
		if !slices.ContainsFunc(captured, func(pattern string) bool { return subjectCovers(pattern, subject) }) {
			return fmt.Errorf("stream %s does not capture %s: its subjects are %v", cfg.Name, subject, captured)
		}
	}
	return nil
}

// subjectCovers reports whether every subject that subject matches is also matched by
// pattern: "*" in pattern matches any one token, ">" one or more tokens.
func subjectCovers(pattern, subject string) bool {
	patternTokens := strings.Split(pattern, ".")
	subjectTokens := strings.Split(subject, ".")
	for i, token := range patternTokens {
		if token == ">" {
			return len(subjectTokens) > i
		}
		if i >= len(subjectTokens) {
			return false
		}
		switch {
		case token == "*":
			// "*" matches one token, never the rest of the subject
			if subjectTokens[i] == ">" {
				return false
			}
		case token != subjectTokens[i]:
			return false
		}
	}
	return len(patternTokens) == len(subjectTokens)
}
