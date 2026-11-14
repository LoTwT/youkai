package app

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/LoTwT/youkai/internal/record"
)

type blockedRevisionStore struct {
	recordStore
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	fail    bool
}

func (s *blockedRevisionStore) SaveRevision(id string, seq, revision int64, edit record.ItemEdit) (record.ItemRecord, int64, error) {
	s.once.Do(func() {
		close(s.entered)
		<-s.release
	})
	if s.fail {
		return record.ItemRecord{}, 0, errors.New("injected revision write failure")
	}
	return s.recordStore.SaveRevision(id, seq, revision, edit)
}

// Exercise the real SaveItemRevision entry and real data-root lock. The
// blocked store leaves the accepted edit ahead of its durable write.
func TestQuitDrainsAcceptedRevisionBeforeReleasingRoot(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "durable", true: "write failure"}[fail], func(t *testing.T) {
			svc, id := reviewService(t, nil, reviewItem(t, 1, reviewData()))
			svc.shutdownOCR = func() error { return nil }
			store := &blockedRevisionStore{recordStore: svc.store, entered: make(chan struct{}), release: make(chan struct{}), fail: fail}
			svc.store = store
			saved := make(chan error, 1)
			go func() {
				_, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: reviewData()})
				saved <- err
			}()
			<-store.entered
			quit := make(chan error, 1)
			go func() { quit <- svc.Quit() }()
			waitFor(t, time.Second, "quit admission boundary", func() bool { return svc.Snapshot().StartBlocked == StartBlockedQuitting })
			select {
			case err := <-quit:
				t.Errorf("Quit returned before the accepted revision completed: %v", err)
				quit <- err
			case <-time.After(30 * time.Millisecond):
			}
			if other, err := AcquireDataRootLock(svc.cfg.Paths.Data); err == nil {
				other.Close()
				t.Error("data root became available while accepted revision was blocked")
			} else if !errors.Is(err, ErrLockHeld) {
				t.Fatal(err)
			}
			close(store.release)
			if err := <-saved; (err != nil) != fail {
				t.Fatalf("save = %v, fail=%v", err, fail)
			}
			if err := <-quit; (err != nil) != fail {
				t.Fatalf("Quit = %v, write failure=%v", err, fail)
			}
			if fail {
				if other, err := AcquireDataRootLock(svc.cfg.Paths.Data); err == nil {
					other.Close()
					t.Error("failed drain released the data root")
				}
				store.fail = false
				if _, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: reviewData()}); err != nil {
					t.Fatalf("repair after failed quit: %v", err)
				}
				if err := svc.Quit(); err != nil {
					t.Fatalf("retry Quit: %v", err)
				}
			}
			if item := loadedItem(t, svc, id, 1); item.Revision != 1 {
				t.Errorf("durable revision = %d, want 1", item.Revision)
			}
			other, err := AcquireDataRootLock(svc.cfg.Paths.Data)
			if err != nil {
				t.Fatalf("root after successful drain: %v", err)
			}
			other.Close()
			if _, err := svc.SaveItemRevision(id, 1, 1, ItemRevisionEdit{Data: reviewData()}); err == nil {
				t.Error("accepted a revision after root handoff")
			}
		})
	}
}

func TestQuitRejectsWritesDuringTeardownAndReopensAfterFailure(t *testing.T) {
	svc, id := reviewService(t, nil, reviewItem(t, 1, reviewData()))
	entered, release := make(chan struct{}), make(chan struct{})
	svc.shutdownOCR = func() error {
		close(entered)
		<-release
		return errors.New("injected shutdown failure")
	}
	quit := make(chan error, 1)
	go func() { quit <- svc.Quit() }()
	<-entered
	if _, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: reviewData()}); err == nil {
		t.Error("accepted revision during teardown")
	}
	if err := svc.RetrySave(); err == nil {
		t.Error("accepted save retry during teardown")
	}
	if _, err := svc.ClearCache(); err == nil {
		t.Error("accepted cache mutation during teardown")
	}
	if _, err := svc.ClearOldLogs(); err == nil {
		t.Error("accepted log mutation during teardown")
	}
	close(release)
	if err := <-quit; err == nil {
		t.Fatal("expected teardown failure")
	}
	if item := loadedItem(t, svc, id, 1); item.Revision != 0 {
		t.Errorf("refused write changed revision to %d", item.Revision)
	}
	if _, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: reviewData()}); err != nil {
		t.Fatalf("repair after teardown failure: %v", err)
	}
	if err := svc.RetrySave(); err != nil {
		t.Fatalf("retry after teardown failure: %v", err)
	}
	if other, err := AcquireDataRootLock(svc.cfg.Paths.Data); err == nil {
		other.Close()
		t.Fatal("teardown failure released data root")
	}
	if err := svc.Quit(); err == nil {
		t.Fatal("repeated quit lost cached teardown error")
	}
}

func TestSettingsUseServiceWriteAdmission(t *testing.T) {
	svc, _ := reviewService(t, nil, reviewItem(t, 1, reviewData()))
	settings := Settings{CloseBehavior: CloseBehaviorExit}
	if err := svc.SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	svc.shutdownOCR = func() error { return nil }
	if err := svc.Quit(); err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveSettings(DefaultSettings()); !errors.Is(err, ErrServiceClosing) {
		t.Fatalf("settings after root handoff = %v", err)
	}
	got, err := LoadSettings(svc.cfg.Paths.Data)
	if err != nil || got != settings {
		t.Fatalf("refused settings changed durable preferences: %+v, %v", got, err)
	}
}

func TestCachedFailedQuitStillDrainsLaterAcceptedSave(t *testing.T) {
	svc, id := reviewService(t, nil, reviewItem(t, 1, reviewData()))
	svc.shutdownOCR = func() error { return errors.New("injected teardown failure") }
	first := svc.Quit()
	if first == nil {
		t.Fatal("expected teardown failure")
	}
	store := &blockedRevisionStore{recordStore: svc.store, entered: make(chan struct{}), release: make(chan struct{})}
	svc.store = store
	saved := make(chan error, 1)
	go func() {
		_, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: reviewData()})
		saved <- err
	}()
	<-store.entered
	quit := make(chan error, 1)
	go func() { quit <- svc.Quit() }()
	select {
	case err := <-quit:
		t.Errorf("cached teardown result bypassed an accepted write: %v", err)
		quit <- err
	case <-time.After(30 * time.Millisecond):
	}
	close(store.release)
	if err := <-saved; err != nil {
		t.Fatal(err)
	}
	if err := <-quit; err == nil || err.Error() != first.Error() {
		t.Fatalf("repeated quit = %v, want cached %v", err, first)
	}
	if other, err := AcquireDataRootLock(svc.cfg.Paths.Data); err == nil {
		other.Close()
		t.Fatal("cached failed teardown handed over the root")
	}
	if _, err := svc.SaveItemRevision(id, 1, 1, ItemRevisionEdit{Data: reviewData()}); err != nil {
		t.Fatalf("repair after repeated failed quit: %v", err)
	}
}

func TestConcurrentQuitsJoinFailedWriteDrain(t *testing.T) {
	svc, id := reviewService(t, nil, reviewItem(t, 1, reviewData()))
	svc.shutdownOCR = func() error { return nil }
	store := &blockedRevisionStore{recordStore: svc.store, entered: make(chan struct{}), release: make(chan struct{}), fail: true}
	svc.store = store
	saved := make(chan error, 1)
	go func() {
		_, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: reviewData()})
		saved <- err
	}()
	<-store.entered
	quit := make(chan error, 2)
	go func() { quit <- svc.Quit() }()
	waitFor(t, time.Second, "quit admission boundary", func() bool { return svc.Snapshot().StartBlocked == StartBlockedQuitting })
	go func() { quit <- svc.Quit() }()
	select {
	case err := <-quit:
		t.Errorf("concurrent quit bypassed the write drain: %v", err)
		quit <- err
	case <-time.After(30 * time.Millisecond):
	}
	close(store.release)
	if err := <-saved; err == nil {
		t.Fatal("expected write failure")
	}
	for i := 0; i < 2; i++ {
		if err := <-quit; err == nil {
			t.Error("concurrent quit lost the joined write failure")
		}
	}
	if other, err := AcquireDataRootLock(svc.cfg.Paths.Data); err == nil {
		other.Close()
		t.Fatal("concurrent failed drain handed over the root")
	}
}

func TestSystemTerminationAdmissionStaysClosedAfterFailedQuit(t *testing.T) {
	svc, id := reviewService(t, nil, reviewItem(t, 1, reviewData()))
	svc.shutdownOCR = func() error { return errors.New("injected shutdown failure") }
	svc.PrepareSystemTermination()
	if _, err := svc.SaveItemRevision(id, 1, 0, ItemRevisionEdit{Data: reviewData()}); !errors.Is(err, ErrServiceClosing) {
		t.Fatalf("write after termination preparation = %v", err)
	}
	if err := svc.Quit(); err == nil {
		t.Fatal("expected teardown failure")
	}
	if err := svc.SaveSettings(DefaultSettings()); !errors.Is(err, ErrServiceClosing) {
		t.Fatalf("irreversible termination reopened writes after failure: %v", err)
	}
	if other, err := AcquireDataRootLock(svc.cfg.Paths.Data); err == nil {
		other.Close()
		t.Fatal("failed termination cleanup released root")
	}
}
