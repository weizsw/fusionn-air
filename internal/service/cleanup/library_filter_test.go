package cleanup

import (
	"os"
	"testing"

	"github.com/fusionn-air/internal/client/emby"
	"github.com/fusionn-air/pkg/logger"
)

func TestMain(m *testing.M) {
	logger.Init(true)
	os.Exit(m.Run())
}

func TestResolveExcludedLibraryIDs(t *testing.T) {
	libraries := []emby.VirtualFolder{
		{Name: "TV Shows", ItemID: "100"},
		{Name: "Movies", ItemID: "200"},
		{Name: "Anime", ItemID: "300"},
		{Name: "Kids", ItemID: "400"},
	}

	tests := []struct {
		name       string
		configured []string
		wantIDs    map[string]bool
	}{
		{
			name:       "empty config returns nil",
			configured: nil,
			wantIDs:    nil,
		},
		{
			name:       "exact match",
			configured: []string{"Anime"},
			wantIDs:    map[string]bool{"300": true},
		},
		{
			name:       "case-insensitive match",
			configured: []string{"anime", "KIDS"},
			wantIDs:    map[string]bool{"300": true, "400": true},
		},
		{
			name:       "unmatched name is skipped",
			configured: []string{"Anime", "Nonexistent"},
			wantIDs:    map[string]bool{"300": true},
		},
		{
			name:       "all unmatched returns empty map",
			configured: []string{"Nonexistent"},
			wantIDs:    map[string]bool{},
		},
		{
			name:       "multiple matches",
			configured: []string{"Anime", "Kids", "Movies"},
			wantIDs:    map[string]bool{"200": true, "300": true, "400": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveExcludedLibraryIDs(tt.configured, libraries)

			if tt.wantIDs == nil {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}

			if len(got) != len(tt.wantIDs) {
				t.Errorf("expected %d IDs, got %d: %v", len(tt.wantIDs), len(got), got)
				return
			}

			for id := range tt.wantIDs {
				if !got[id] {
					t.Errorf("expected ID %q in result, got %v", id, got)
				}
			}
		})
	}
}

func TestFilterByLibrary(t *testing.T) {
	items := []emby.Item{
		{ID: "1", Name: "Show A", ParentID: "100"},
		{ID: "2", Name: "Show B", ParentID: "200"},
		{ID: "3", Name: "Show C", ParentID: "300"},
		{ID: "4", Name: "Show D", ParentID: "100"},
	}

	tests := []struct {
		name        string
		excludedIDs map[string]bool
		wantNames   []string
	}{
		{
			name:        "nil exclusion returns all",
			excludedIDs: nil,
			wantNames:   []string{"Show A", "Show B", "Show C", "Show D"},
		},
		{
			name:        "empty exclusion returns all",
			excludedIDs: map[string]bool{},
			wantNames:   []string{"Show A", "Show B", "Show C", "Show D"},
		},
		{
			name:        "exclude one library",
			excludedIDs: map[string]bool{"200": true},
			wantNames:   []string{"Show A", "Show C", "Show D"},
		},
		{
			name:        "exclude multiple libraries",
			excludedIDs: map[string]bool{"100": true, "300": true},
			wantNames:   []string{"Show B"},
		},
		{
			name:        "exclude all libraries",
			excludedIDs: map[string]bool{"100": true, "200": true, "300": true},
			wantNames:   []string{},
		},
		{
			name:        "exclude non-matching library ID",
			excludedIDs: map[string]bool{"999": true},
			wantNames:   []string{"Show A", "Show B", "Show C", "Show D"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterByLibrary(items, tt.excludedIDs)

			if len(got) != len(tt.wantNames) {
				t.Errorf("expected %d items, got %d", len(tt.wantNames), len(got))
				return
			}

			for i, name := range tt.wantNames {
				if got[i].Name != name {
					t.Errorf("item[%d]: expected %q, got %q", i, name, got[i].Name)
				}
			}
		})
	}
}
