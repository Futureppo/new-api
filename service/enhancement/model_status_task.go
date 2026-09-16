package enhancement

import (
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
)

// ModelStatusSnapshot describes the last completed server-side calculation.
// GeneratedAt remains zero until the first successful calculation, including
// when all models are hidden by the request-count threshold.
type ModelStatusSnapshot struct {
	Statuses      []ModelStatus
	GeneratedAt   int64
	Ready         bool
	RefreshFailed bool
}

var modelStatusPublicCache = struct {
	sync.RWMutex
	key             string
	revision        uint64
	data            []ModelStatus
	generatedAt     int64
	lastCompletedAt time.Time
	refreshing      bool
	refreshFailed   bool
}{}

var modelStatusRefreshOnce sync.Once

func modelStatusRefreshInterval() time.Duration {
	seconds := setting.GetEnhancementSetting().ModelStatusRefreshSeconds
	if seconds < 60 {
		seconds = 60
	}
	if seconds > 24*60*60 {
		seconds = 24 * 60 * 60
	}
	return time.Duration(seconds) * time.Second
}

func modelStatusPublicCacheKey() string {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	cfg := setting.GetEnhancementSetting()
	// Include every setting affecting the snapshot, especially public visibility
	// and error exclusions. Never serve a snapshot from a different configuration.
	key, _ := common.Marshal([]interface{}{
		cfg.PublicEmbedEnabled, ModelStatusConfiguredWindow(), ModelStatusSlotMinutes(),
		cfg.ModelStatusGreenThreshold, cfg.ModelStatusYellowThreshold,
		cfg.ModelStatusRequestCountHideThreshold, cfg.ModelStatusIgnoreErrorKeywordsEnabled,
		cfg.ModelStatusIgnoredErrorKeywords, ratio_setting.GroupDisplay2JSONString(),
		setting.UserUsableGroups2JSONString(),
	})
	return string(key)
}

func ClearModelStatusPublicCache() {
	modelStatusPublicCache.Lock()
	defer modelStatusPublicCache.Unlock()
	modelStatusPublicCache.revision++
	modelStatusPublicCache.key = ""
	modelStatusPublicCache.data = nil
	modelStatusPublicCache.generatedAt = 0
	modelStatusPublicCache.lastCompletedAt = time.Time{}
	modelStatusPublicCache.refreshFailed = false
	// Keep refreshing set if a calculation is in flight. Its revision check
	// will discard the result, and the next server tick will load the new config.
}

// StartModelStatusRefreshTask starts one worker per server process, since the
// snapshots are held in process-local memory. HTTP requests never start it.
func StartModelStatusRefreshTask() {
	modelStatusRefreshOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				if err := runModelStatusRefreshOnce(); err != nil {
					common.SysError("failed to refresh model status: " + err.Error())
				}
				<-ticker.C
			}
		}()
	})
}

func runModelStatusRefreshOnce() error {
	if !setting.GetEnhancementSetting().PublicEmbedEnabled {
		ClearModelStatusPublicCache()
		return nil
	}
	key := modelStatusPublicCacheKey()
	interval := modelStatusRefreshInterval()
	modelStatusPublicCache.Lock()
	if modelStatusPublicCache.refreshing ||
		(modelStatusPublicCache.key == key && time.Now().Before(modelStatusPublicCache.lastCompletedAt.Add(interval))) {
		modelStatusPublicCache.Unlock()
		return nil
	}
	if modelStatusPublicCache.key != key {
		modelStatusPublicCache.key = key
		modelStatusPublicCache.data = nil
		modelStatusPublicCache.generatedAt = 0
		modelStatusPublicCache.refreshFailed = false
	}
	revision := modelStatusPublicCache.revision
	modelStatusPublicCache.refreshing = true
	modelStatusPublicCache.Unlock()
	defer func() {
		modelStatusPublicCache.Lock()
		modelStatusPublicCache.refreshing = false
		modelStatusPublicCache.Unlock()
	}()

	statuses, err := ModelStatusesForWindow(nil, ModelStatusConfiguredWindow(), true)
	finishedAt := time.Now()
	currentKey := modelStatusPublicCacheKey()
	modelStatusPublicCache.Lock()
	defer modelStatusPublicCache.Unlock()
	if revision != modelStatusPublicCache.revision || currentKey != key {
		return nil
	}
	// Wait a full interval after completion, including failures. Slow queries
	// cannot cause overlapping runs or an immediate retry loop.
	modelStatusPublicCache.lastCompletedAt = finishedAt
	modelStatusPublicCache.refreshFailed = err != nil
	if err != nil {
		return err
	}
	for i := range statuses {
		statuses[i].GeneratedAt = finishedAt.Unix()
	}
	modelStatusPublicCache.data = statuses
	modelStatusPublicCache.generatedAt = finishedAt.Unix()
	return nil
}

func readModelStatusPublicSnapshot() (ModelStatusSnapshot, error) {
	snapshot := ModelStatusSnapshot{Statuses: []ModelStatus{}}
	if err := requirePublicEmbedEnabled(); err != nil {
		return snapshot, err
	}
	key := modelStatusPublicCacheKey()
	modelStatusPublicCache.RLock()
	defer modelStatusPublicCache.RUnlock()
	if modelStatusPublicCache.key != key {
		return snapshot, nil
	}
	snapshot.Statuses = append(snapshot.Statuses, modelStatusPublicCache.data...)
	snapshot.GeneratedAt = modelStatusPublicCache.generatedAt
	snapshot.Ready = snapshot.GeneratedAt != 0
	snapshot.RefreshFailed = modelStatusPublicCache.refreshFailed
	return snapshot, nil
}

func GetModelStatusPublicSnapshot() (ModelStatusSnapshot, error) {
	snapshot, err := readModelStatusPublicSnapshot()
	if err == nil {
		snapshot.Statuses = filterLowRequestModelStatuses(snapshot.Statuses, setting.GetEnhancementSetting().ModelStatusRequestCountHideThreshold)
	}
	return snapshot, err
}

// The single-model and batch public endpoints also read the same snapshot;
// they must not offer a way to trigger log scans from a visitor's request.
func ModelStatusForPublicConfig(group, name string) (ModelStatus, error) {
	snapshot, err := readModelStatusPublicSnapshot()
	if err != nil {
		return ModelStatus{}, err
	}
	group, name = strings.TrimSpace(group), strings.TrimSpace(name)
	if group == "" {
		group = "default"
	}
	for _, status := range snapshot.Statuses {
		if status.Group == group && status.ModelName == name {
			return status, nil
		}
	}
	return ModelStatus{}, gorm.ErrRecordNotFound
}

func ModelStatusesForPublicModels(names []string) ([]ModelStatus, error) {
	snapshot, err := readModelStatusPublicSnapshot()
	if err != nil || len(names) == 0 {
		return snapshot.Statuses, err
	}
	names, err = validateModelList(names, true)
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[name] = true
	}
	statuses := make([]ModelStatus, 0, len(names))
	for _, status := range snapshot.Statuses {
		if wanted[status.ModelName] {
			statuses = append(statuses, status)
		}
	}
	return statuses, nil
}
