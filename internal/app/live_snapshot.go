package app

import (
	"log"
	"os"
	"time"

	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// LoadActiveRecord serves a display refresh for the current capture using
// its latest successfully saved generation. History, review and final
// results continue to verify on disk through LoadRecord.
func (s *Service) LoadActiveRecord(id string) (*record.Recovery, error) {
	if saved := s.activeSavedSnapshot(id); saved != nil {
		if os.Getenv("YOUKAI_RECORD_STORAGE_TIMING") == "1" {
			started := time.Now()
			defer func() {
				log.Printf("youkai: active-checkpoint snapshot revision=%d items=%d elapsed_us=%d", saved.SavedRevision, len(saved.Items), time.Since(started).Microseconds())
			}()
		}
		return record.ActiveCheckpointRecovery(saved), nil
	}
	return s.LoadRecord(id)
}

func (s *Service) activeSavedSnapshot(id string) *record.Checkpoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.active
	if current == nil || current.wrapped || string(current.id) != id ||
		current.deliverer == nil || current.kernel == nil {
		return nil
	}
	// A refresh scheduled during capture can run after the kernel begins
	// wrapping up. Keep its saved snapshot path during that race.
	if os.Getenv("YOUKAI_DEFER_WRAP_LIVE_READ") != "1" && current.kernel.Snapshot().Phase != scan.PhaseCapturing {
		return nil
	}
	return current.deliverer.savedLiveSnapshot.Load()
}
