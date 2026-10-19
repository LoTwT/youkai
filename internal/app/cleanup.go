package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/LoTwT/youkai/internal/record"
	"github.com/LoTwT/youkai/internal/scan"
)

// ErrCleanupBlocked is reported when screenshots or records cannot be
// deleted now: an active session, its wrap-up save or the explicit exit
// owns the state. The UI additionally refuses while an export or an
// unsaved edit is in flight, because those belong to it, not the service.
var ErrCleanupBlocked = errors.New("cleanup is blocked")

// CleanupBlockedError says why cleanup is refused. Reason is the text the
// interface shows next to the disabled action.
type CleanupBlockedError struct {
	Reason string
}

func (e *CleanupBlockedError) Error() string {
	return "app: cleanup is blocked: " + e.Reason
}

func (e *CleanupBlockedError) Unwrap() error { return ErrCleanupBlocked }

// CleanupPreview is what one cleanup action would remove, shown to the
// user for confirmation before anything is deleted. Capacity is measured,
// not guessed from a previous run; Unreadable counts records whose
// checkpoint could not be loaded, so PendingReview is then a lower bound
// (the interface says so instead of presenting it as exact).
type CleanupPreview struct {
	Records       int
	EvidenceFiles int
	EvidenceBytes int64
	TotalBytes    int64
	PendingReview int
	Unreadable    int
}

// DeleteRecordScreenshots removes one record's screenshots while keeping
// its results, revisions and review state. It is refused while a scan,
// its save or the exit is active.
func (s *Service) DeleteRecordScreenshots(recordID string) (record.EvidenceRemoval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.cleanupBlockedLocked(); err != nil {
		return record.EvidenceRemoval{}, err
	}
	removal, err := s.store.DeleteEvidence(recordID)
	s.notifyLocked()
	if err != nil {
		return removal, fmt.Errorf("app: delete screenshots of %q: %w", recordID, err)
	}
	return removal, nil
}

// DeleteAllScreenshots removes every record's screenshots, reporting
// per-record partial failures instead of stopping at the first one.
func (s *Service) DeleteAllScreenshots() (record.EvidenceBatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.cleanupBlockedLocked(); err != nil {
		return record.EvidenceBatch{}, err
	}
	batch, err := s.store.DeleteAllEvidence()
	s.notifyLocked()
	if err != nil {
		return batch, fmt.Errorf("app: delete all screenshots: %w", err)
	}
	return batch, nil
}

// DeleteRecord permanently removes one record and its screenshots.
func (s *Service) DeleteRecord(recordID string) (record.RecordDeletion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.cleanupBlockedLocked(); err != nil {
		return record.RecordDeletion{}, err
	}
	deletion, err := s.store.DeleteRecord(recordID)
	s.notifyLocked()
	if err != nil {
		return deletion, fmt.Errorf("app: delete record %q: %w", recordID, err)
	}
	return deletion, nil
}

// DeleteAllRecords permanently removes every record and its screenshots.
func (s *Service) DeleteAllRecords() (record.RecordDeletionBatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.cleanupBlockedLocked(); err != nil {
		return record.RecordDeletionBatch{}, err
	}
	batch, err := s.store.DeleteAllRecords()
	s.notifyLocked()
	if err != nil {
		return batch, fmt.Errorf("app: delete all records: %w", err)
	}
	return batch, nil
}

// PreviewRecordScreenshots measures deleting one record's screenshots.
// The record must load, because the deletion itself starts from that
// checkpoint.
func (s *Service) PreviewRecordScreenshots(recordID string) (CleanupPreview, error) {
	rec, err := s.store.Load(recordID)
	if err != nil {
		return CleanupPreview{}, fmt.Errorf("app: preview screenshots of %q: %w", recordID, err)
	}
	usage, err := s.store.Usage(recordID)
	if err != nil {
		return CleanupPreview{}, fmt.Errorf("app: preview screenshots of %q: %w", recordID, err)
	}
	return CleanupPreview{
		Records:       1,
		EvidenceFiles: usage.EvidenceFiles,
		EvidenceBytes: usage.EvidenceBytes,
		TotalBytes:    usage.TotalBytes,
		PendingReview: pendingReviewCount(rec.Checkpoint.Items),
	}, nil
}

// PreviewAllScreenshots measures deleting every record's screenshots.
// Records whose checkpoint does not load are counted as unreadable and
// skipped; the others still provide their measured totals.
func (s *Service) PreviewAllScreenshots() (CleanupPreview, error) {
	infos, err := s.store.ListRecords()
	if err != nil {
		return CleanupPreview{}, fmt.Errorf("app: preview all screenshots: %w", err)
	}
	preview := CleanupPreview{Records: len(infos)}
	for _, info := range infos {
		rec, err := s.store.Load(info.ID)
		if err != nil {
			preview.Unreadable++
			continue
		}
		preview.EvidenceFiles += info.EvidenceFiles
		preview.EvidenceBytes += info.EvidenceBytes
		preview.TotalBytes += info.TotalBytes
		preview.PendingReview += pendingReviewCount(rec.Checkpoint.Items)
	}
	return preview, nil
}

// PreviewRecordDeletion measures deleting one record. Its checkpoint need
// not load — an unrecoverable record is still a directory the user may
// remove; Unreadable then says the pending-review count is unknown.
func (s *Service) PreviewRecordDeletion(recordID string) (CleanupPreview, error) {
	usage, err := s.store.Usage(recordID)
	if err != nil {
		return CleanupPreview{}, fmt.Errorf("app: preview deletion of %q: %w", recordID, err)
	}
	preview := CleanupPreview{
		Records:       1,
		EvidenceFiles: usage.EvidenceFiles,
		EvidenceBytes: usage.EvidenceBytes,
		TotalBytes:    usage.TotalBytes,
	}
	rec, err := s.store.Load(recordID)
	if err != nil {
		preview.Unreadable = 1
		return preview, nil
	}
	preview.PendingReview = pendingReviewCount(rec.Checkpoint.Items)
	return preview, nil
}

// PreviewAllRecords measures deleting every record.
func (s *Service) PreviewAllRecords() (CleanupPreview, error) {
	infos, err := s.store.ListRecords()
	if err != nil {
		return CleanupPreview{}, fmt.Errorf("app: preview all records: %w", err)
	}
	preview := CleanupPreview{Records: len(infos)}
	for _, info := range infos {
		preview.EvidenceFiles += info.EvidenceFiles
		preview.EvidenceBytes += info.EvidenceBytes
		preview.TotalBytes += info.TotalBytes
		rec, err := s.store.Load(info.ID)
		if err != nil {
			preview.Unreadable++
			continue
		}
		preview.PendingReview += pendingReviewCount(rec.Checkpoint.Items)
	}
	return preview, nil
}

// cleanupBlockedLocked refuses cleanup while the active session still
// owns its record: during capture, while the final save is pending or
// failed, and during the explicit exit. A wrapped session's record is
// final and may be removed. Callers hold mu.
func (s *Service) cleanupBlockedLocked() error {
	if s.quitting {
		return &CleanupBlockedError{Reason: "正在退出应用，不能清理。"}
	}
	a := s.active
	if a == nil {
		return nil
	}
	if a.wrapped && a.saveErr == nil {
		return nil
	}
	if a.deliverer != nil && a.deliverer.failed() {
		return &CleanupBlockedError{Reason: "扫描结果保存失败，重试保存成功后才能清理。"}
	}
	if a.kernel != nil {
		if phase := a.kernel.Snapshot().Phase; phase == scan.PhaseFinalizing {
			return &CleanupBlockedError{Reason: "扫描正在收尾保存，完成前不能清理。"}
		}
	}
	return &CleanupBlockedError{Reason: "扫描会话进行中，结束后才能清理。"}
}

// pendingReviewCount counts produced items whose field quality still
// needs a person and whose payload has the structured interpretation the
// review editor works on. Auto-accepted and human-confirmed items are
// settled; failed and unprocessed items are not reviewable in this
// revision. It mirrors the results rows' PendingReview.
func pendingReviewCount(items []record.ItemRecord) int {
	count := 0
	for _, item := range items {
		if item.Status != record.StatusProduced || !hasStructured(item.Data) {
			continue
		}
		switch item.Quality {
		case record.QualityAutoAccepted, record.QualityHumanConfirmed:
		default:
			count++
		}
	}
	return count
}

// hasStructured reports whether a produced payload carries the structured
// drive-disc interpretation the review flow edits, as opposed to raw
// recognized lines only.
func hasStructured(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var doc struct {
		Structured json.RawMessage `json:"structured"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return false
	}
	value := bytes.TrimSpace(doc.Structured)
	return len(value) > 0 && !bytes.Equal(value, []byte("null")) && json.Valid(value)
}

// StorageUsage is the storeroom view: how much history, screenshots,
// cache and logs occupy, plus the free space of the volume holding the
// data root. DiskSpaceKnown is false when the volume could not be
// queried; the interface then says so instead of showing a stale or zero
// free space as exact.
type StorageUsage struct {
	Records        int
	RecordBytes    int64
	EvidenceBytes  int64
	EvidenceFiles  int
	CacheBytes     int64
	LogBytes       int64
	DiskFreeBytes  int64
	DiskTotalBytes int64
	DiskSpaceKnown bool
}

// StorageUsage measures the application's managed storage. It walks the
// record, cache and log trees on the calling goroutine, so callers run it
// in the background.
func (s *Service) StorageUsage() (StorageUsage, error) {
	usage, err := s.store.StoreUsage()
	if err != nil {
		return StorageUsage{}, fmt.Errorf("app: storage usage: %w", err)
	}
	cacheBytes, err := directorySize(s.cfg.Paths.Cache)
	if err != nil {
		return StorageUsage{}, fmt.Errorf("app: storage usage: cache: %w", err)
	}
	logBytes, err := directorySize(s.cfg.Paths.Logs)
	if err != nil {
		return StorageUsage{}, fmt.Errorf("app: storage usage: logs: %w", err)
	}
	out := StorageUsage{
		Records:       usage.Records,
		RecordBytes:   usage.TotalBytes,
		EvidenceBytes: usage.EvidenceBytes,
		EvidenceFiles: usage.EvidenceFiles,
		CacheBytes:    cacheBytes,
		LogBytes:      logBytes,
	}
	if free, total, ok := diskSpace(s.cfg.Paths.Data); ok {
		out.DiskFreeBytes = free
		out.DiskTotalBytes = total
		out.DiskSpaceKnown = true
	}
	return out, nil
}

// directorySize sums the regular files below root without following
// symbolic links. A missing directory is empty, not an error: cache and
// logs are created by whoever writes first.
func directorySize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	return total, err
}
