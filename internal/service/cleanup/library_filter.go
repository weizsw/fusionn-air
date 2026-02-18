package cleanup

import (
	"strings"

	"github.com/fusionn-air/internal/client/emby"
	"github.com/fusionn-air/pkg/logger"
)

// ResolveExcludedLibraryIDs maps configured library names to Emby library IDs.
// Returns a set of IDs to exclude. Logs a warning for any configured name
// that doesn't match an actual Emby library (catches typos / renames).
func ResolveExcludedLibraryIDs(configuredNames []string, libraries []emby.VirtualFolder) map[string]bool {
	if len(configuredNames) == 0 {
		return nil
	}

	byName := make(map[string]string, len(libraries))
	for _, lib := range libraries {
		byName[strings.ToLower(lib.Name)] = lib.ItemID
	}

	excluded := make(map[string]bool, len(configuredNames))
	for _, name := range configuredNames {
		id, ok := byName[strings.ToLower(name)]
		if !ok {
			logger.Warnf("⚠️  Excluded library %q not found in Emby — check spelling", name)
			continue
		}
		excluded[id] = true
		logger.Infof("🚫 Excluding Emby library %q (ID=%s) from cleanup", name, id)
	}

	return excluded
}

// filterByLibrary removes items whose ParentID is in the excluded set.
func filterByLibrary(items []emby.Item, excludedIDs map[string]bool) []emby.Item {
	if len(excludedIDs) == 0 {
		return items
	}

	filtered := make([]emby.Item, 0, len(items))
	for _, item := range items {
		if !excludedIDs[item.ParentID] {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
