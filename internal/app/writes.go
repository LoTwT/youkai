package app

import "errors"

// ErrServiceClosing refuses a new write during quit or after data-root
// handoff. A failed exit that retained the root reopens repair writes.
var ErrServiceClosing = errors.New("app: the service is closing; no new writes are accepted")

func (s *Service) beginWrite() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writesClosed {
		return ErrServiceClosing
	}
	s.acceptedWrites.Add(1)
	return nil
}

func (s *Service) finishWrite(err error) {
	s.mu.Lock()
	if s.writesClosed && err != nil {
		s.drainErr = errors.Join(s.drainErr, err)
	}
	s.mu.Unlock()
	s.acceptedWrites.Done()
}

// SealWritesForExit closes admission permanently and joins writes that
// were accepted before the final exit. Call it after Quit has settled and
// the user has committed to exiting, including after a failed teardown
// that temporarily reopened writes while the data root was still held.
func (s *Service) SealWritesForExit() error {
	s.mu.Lock()
	s.writesClosed = true
	s.writesSealed = true
	s.mu.Unlock()
	return s.waitForAcceptedWrites()
}

func (s *Service) waitForAcceptedWrites() error {
	s.acceptedWrites.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drainErr
}

// SaveSettings writes preferences under the same admission and occupancy
// contract as record revisions. Startup reads still use LoadSettings.
func (s *Service) SaveSettings(settings Settings) (err error) {
	if err = s.beginWrite(); err != nil {
		return err
	}
	defer func() { s.finishWrite(err) }()
	return SaveSettings(s.cfg.Paths.Data, settings)
}
