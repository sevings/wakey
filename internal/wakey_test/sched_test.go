package wakey_test

import (
	"sync"
	"testing"
	"time"

	"wakey/internal/wakey"

	"github.com/stretchr/testify/require"
)

func TestNewSched(t *testing.T) {
	r := require.New(t)
	s := wakey.NewSched(10)
	r.NotNil(s, "NewSched should not return nil")
}

func TestScheduleAndRun(t *testing.T) {
	r := require.New(t)
	s := wakey.NewSched(10)

	var wg sync.WaitGroup
	wg.Add(1)

	executed := false
	s.SetJobFunc(func(id wakey.JobID) {
		r.Equal(wakey.JobID(1), id, "Job ID should be 1")
		executed = true
		wg.Done()
	})

	s.Start()
	s.Schedule(time.Now().Add(50*time.Millisecond), wakey.JobID(1))

	wg.Wait()
	s.Stop()

	r.True(executed, "Job should have been executed")
}

func TestCancel(t *testing.T) {
	r := require.New(t)
	s := wakey.NewSched(10)

	executed := false
	s.SetJobFunc(func(id wakey.JobID) {
		executed = true
	})

	s.Start()
	s.Schedule(time.Now().Add(100*time.Millisecond), wakey.JobID(1))
	s.Cancel(wakey.JobID(1))

	time.Sleep(200 * time.Millisecond)
	s.Stop()

	r.False(executed, "Job should not have been executed after cancellation")
}

func TestMultipleJobs(t *testing.T) {
	r := require.New(t)
	s := wakey.NewSched(10)

	var mu sync.Mutex
	executed := make(map[wakey.JobID]bool)

	s.SetJobFunc(func(id wakey.JobID) {
		mu.Lock()
		executed[id] = true
		mu.Unlock()
	})

	s.Start()
	s.Schedule(time.Now().Add(50*time.Millisecond), wakey.JobID(1))
	s.Schedule(time.Now().Add(60*time.Millisecond), wakey.JobID(2))
	s.Schedule(time.Now().Add(70*time.Millisecond), wakey.JobID(3))

	time.Sleep(100 * time.Millisecond)
	s.Stop()

	r.Len(executed, 3, "All three jobs should have been executed")
	r.True(executed[wakey.JobID(1)], "Job 1 should have been executed")
	r.True(executed[wakey.JobID(2)], "Job 2 should have been executed")
	r.True(executed[wakey.JobID(3)], "Job 3 should have been executed")
}

func TestConcurrency(t *testing.T) {
	r := require.New(t)
	s := wakey.NewSched(100)

	var mu sync.Mutex
	executedCount := 0

	s.SetJobFunc(func(id wakey.JobID) {
		mu.Lock()
		executedCount++
		mu.Unlock()
	})

	s.Start()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id wakey.JobID) {
			defer wg.Done()
			s.Schedule(time.Now().Add(50*time.Millisecond), id)
			if id%2 == 0 {
				time.Sleep(10 * time.Millisecond)
				s.Cancel(id)
			}
		}(wakey.JobID(i))
	}

	wg.Wait()
	time.Sleep(100 * time.Millisecond)
	s.Stop()

	r.Less(executedCount, 100, "Some jobs should have been cancelled")
	r.Greater(executedCount, 0, "Some jobs should have been executed")
}
