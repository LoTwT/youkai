package record

import (
	"log"
	"os"
	"time"
)

// storageTiming is enabled only in the local profiling candidate.
// Read, checkpoint and evidence spans must exclude the logging itself.
var storageTiming = os.Getenv("YOUKAI_RECORD_STORAGE_TIMING") == "1"

func (s *Store) lockStorageTiming(operation string, count int) func() {
	if !storageTiming {
		s.mu.Lock()
		return s.mu.Unlock
	}
	started := time.Now()
	s.mu.Lock()
	locked := time.Now()
	return func() {
		finished := time.Now()
		s.mu.Unlock()
		log.Printf("youkai: record-storage-timing op=%s count=%d start_ns=%d locked_ns=%d end_ns=%d wait_us=%d held_us=%d",
			operation, count, started.UnixNano(), locked.UnixNano(), finished.UnixNano(), locked.Sub(started).Microseconds(), finished.Sub(locked).Microseconds())
	}
}
