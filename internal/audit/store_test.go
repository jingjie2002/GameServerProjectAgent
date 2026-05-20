package audit

import (
	"path/filepath"
	"testing"
)

func TestStoreAppendAndList(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "audit.log"))
	if err := store.Append(Event{Mode: "自动审查", Action: "test", ProjectID: "demo", Status: "ok"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	events, err := store.List(10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 1 || events[0].ProjectID != "demo" {
		t.Fatalf("unexpected events: %#v", events)
	}
}
