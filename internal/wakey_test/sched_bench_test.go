package wakey_test

import (
	"testing"
	"time"
	"wakey/internal/wakey"
)

func BenchmarkScheduleSingleJob(b *testing.B) {
	s := wakey.NewSched(b.N)
	s.SetJobFunc(func(wakey.JobID) {})
	s.Start()
	defer s.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Schedule(time.Now().Add(time.Millisecond*time.Duration(i)), wakey.JobID(i))
	}
}

func BenchmarkScheduleAndCancel(b *testing.B) {
	s := wakey.NewSched(b.N)
	s.SetJobFunc(func(wakey.JobID) {})
	s.Start()
	defer s.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Schedule(time.Now().Add(time.Hour), wakey.JobID(i))
		s.Cancel(wakey.JobID(i))
	}
}

func BenchmarkScheduleWithExecution(b *testing.B) {
	s := wakey.NewSched(b.N)
	s.SetJobFunc(func(wakey.JobID) {})
	s.Start()
	defer s.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Schedule(time.Now(), wakey.JobID(i))
	}
}

func BenchmarkParallelScheduling(b *testing.B) {
	s := wakey.NewSched(b.N)
	s.SetJobFunc(func(wakey.JobID) {})
	s.Start()
	defer s.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			s.Schedule(time.Now().Add(time.Millisecond), wakey.JobID(i))
			i++
		}
	})
}

func BenchmarkMixedOperations(b *testing.B) {
	s := wakey.NewSched(b.N)
	s.SetJobFunc(func(wakey.JobID) {})
	s.Start()
	defer s.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%2 == 0 {
				s.Schedule(time.Now().Add(time.Millisecond), wakey.JobID(i))
			} else {
				s.Cancel(wakey.JobID(i - 1))
			}
			i++
		}
	})
}
