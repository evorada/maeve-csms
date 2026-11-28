// SPDX-License-Identifier: Apache-2.0

package sessions

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	clockTest "k8s.io/utils/clock/testing"
)

// The times of the last ratings are not kept for ever: a transaction whose end the
// manager never saw is forgotten once many others have been rated since.
func TestTheTimesOfTheLastRatingsAreForgotten(t *testing.T) {
	start := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	clk := clockTest.NewFakePassiveClock(start)
	s := &Service{RunningEvery: time.Minute, Clock: clk}

	// transactions that were rated more than a day ago, and two that were rated since
	for i := 0; i < throttleSize-2; i++ {
		assert.True(t, s.runningCostDue(fmt.Sprintf("cs/old-%d", i)))
	}
	clk.SetTime(start.Add(throttleKeep + time.Second))
	assert.True(t, s.runningCostDue("cs/recent"))
	assert.True(t, s.runningCostDue("cs/other"))
	assert.Len(t, s.lastRunning, throttleSize, "nothing is forgotten below the size, however old")

	// the next one makes room: those that were not rated for longer than a day go
	clk.SetTime(start.Add(throttleKeep + 2*time.Second))
	assert.True(t, s.runningCostDue("cs/new"))
	assert.Len(t, s.lastRunning, 3)
	assert.Contains(t, s.lastRunning, "cs/recent")
	assert.Contains(t, s.lastRunning, "cs/other")
	assert.Contains(t, s.lastRunning, "cs/new")
	assert.False(t, s.runningCostDue("cs/recent"), "a transaction that was rated a second ago is still held back")

	// a long interval keeps the times for twice as long as itself
	long := &Service{RunningEvery: 36 * time.Hour, Clock: clk}
	clk.SetTime(start)
	for i := 0; i < throttleSize; i++ {
		assert.True(t, long.runningCostDue(fmt.Sprintf("cs/old-%d", i)))
	}
	clk.SetTime(start.Add(72 * time.Hour))
	assert.True(t, long.runningCostDue("cs/new"))
	assert.Len(t, long.lastRunning, throttleSize+1, "none is older than twice the interval")
	clk.SetTime(start.Add(72*time.Hour + time.Second))
	assert.True(t, long.runningCostDue("cs/newer"))
	assert.Len(t, long.lastRunning, 2)

	// a service that does not hold ratings back keeps no times
	every := &Service{Clock: clk}
	assert.True(t, every.runningCostDue("cs/any"))
	assert.True(t, every.runningCostDue("cs/any"))
	assert.Empty(t, every.lastRunning)

	// End forgets the time of its transaction
	s.forgetRunning("cs/new")
	assert.NotContains(t, s.lastRunning, "cs/new")
	assert.True(t, s.runningCostDue("cs/new"))
}
