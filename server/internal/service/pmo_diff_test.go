package service

import "testing"

func TestDiffPMOFieldMatrix(t *testing.T) {
	cases := []struct {
		name         string
		externalBase any
		localBase    any
		externalNow  any
		localNow     any
		want         PMOFieldDecision
	}{
		{"unchanged", "a", "a", "a", "a", PMOUnchanged},
		{"external only", "a", "a", "b", "a", PMOIncoming},
		{"local only", "a", "a", "a", "b", PMOLocalOnly},
		{"converged", "a", "a", "b", "b", PMOConverged},
		{"conflict", "a", "a", "b", "c", PMOConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DiffPMOField(tc.externalBase, tc.localBase, tc.externalNow, tc.localNow); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestDiffPMOSnapshotCreatesProjectBeforeIssuesAndRetainsHierarchy(t *testing.T) {
	snapshot := mustParsePMOSnapshot(t, validPMOSnapshotJSON())
	diff := DiffPMOSnapshot(PMODiffInput{Snapshot: snapshot})

	if len(diff.Entities) != 3 {
		t.Fatalf("got %d entities: %#v", len(diff.Entities), diff.Entities)
	}
	assertPMOEntity(t, diff.Entities[0], PMOLocalProject, "requirement", "EXT-P-001", "", PMOCreate)
	assertPMOEntity(t, diff.Entities[1], PMOLocalIssue, "requirement", "EXT-C-001", "EXT-P-001", PMOCreate)
	assertPMOEntity(t, diff.Entities[2], PMOLocalIssue, "task", "TASK-001", "EXT-C-001", PMOCreate)
	if _, exists := diff.Entities[1].Fields["workload"]; !exists {
		t.Fatal("child requirement workload is missing from synced fields")
	}
	if diff.Entities[2].ProjectExternalKey != "EXT-P-001" {
		t.Fatalf("child issue lost project reference: %#v", diff.Entities[2])
	}
	if diff.Summary.Creates != 3 {
		t.Fatalf("create summary = %#v", diff.Summary)
	}
}

func TestDiffPMOSnapshotFormatsOnlyProjectTitle(t *testing.T) {
	snapshot := mustParsePMOSnapshot(t, validPMOSnapshotJSON())
	snapshot.Parent.DisplayNumber = " REQ-1234 "
	snapshot.Parent.Title = " Add invoice export "
	snapshot.Children[0].Title = "Child title"
	snapshot.Children[0].Tasks[0].Title = "Task title"

	diff := DiffPMOSnapshot(PMODiffInput{Snapshot: snapshot})
	if got := diff.Entities[0].Fields["title"].External; got != "REQ-1234 Add invoice export" {
		t.Fatalf("project title = %q", got)
	}
	if got := diff.Entities[1].Fields["title"].External; got != "Child title" {
		t.Fatalf("child title = %q", got)
	}
	if got := diff.Entities[2].Fields["title"].External; got != "Task title" {
		t.Fatalf("task title = %q", got)
	}
}

func TestDiffPMOSnapshotMarksMissingLinkedEntitiesExternallyRemoved(t *testing.T) {
	snapshot := mustParsePMOSnapshot(t, validPMOSnapshotJSON())
	diff := DiffPMOSnapshot(PMODiffInput{
		Snapshot: snapshot,
		Links: []PMOLinkState{{
			ExternalType: "task",
			ExternalKey:  "TASK-OLD",
			LocalType:    PMOLocalIssue,
			LocalID:      "issue-old",
		}},
	})

	removed := findPMOEntityDiff(t, diff, "task", "TASK-OLD")
	if removed.Action != PMOExternalRemoved {
		t.Fatalf("got action %q, want %q", removed.Action, PMOExternalRemoved)
	}
	if diff.Summary.ExternalRemoved != 1 {
		t.Fatalf("remove summary = %#v", diff.Summary)
	}
}

func TestDiffPMOSnapshotKeepsIncomingFieldsWhenAssigneeIsUnresolved(t *testing.T) {
	snapshot := mustParsePMOSnapshot(t, validPMOSnapshotJSON())
	snapshot.Parent.Title = "Changed externally"
	diff := DiffPMOSnapshot(PMODiffInput{
		Snapshot: snapshot,
		Links: []PMOLinkState{{
			ExternalType:     "requirement",
			ExternalKey:      "EXT-P-001",
			LocalType:        PMOLocalProject,
			LocalID:          "project-1",
			BaselineExternal: map[string]any{"title": "Old title"},
			BaselineLocal:    map[string]any{"title": "Old title"},
			CurrentLocal:     map[string]any{"title": "Old title", "lead_id": nil},
		}},
	})

	project := findPMOEntityDiff(t, diff, "requirement", "EXT-P-001")
	if project.Fields["title"].Decision != PMOIncoming {
		t.Fatalf("title decision = %#v", project.Fields["title"])
	}
	if _, exists := project.Fields["lead_id"]; exists {
		t.Fatalf("unresolved lead must not produce a write: %#v", project.Fields["lead_id"])
	}
	if len(diff.Warnings) != 1 || diff.Warnings[0].Code != PMOWarningUnresolvedAssignee {
		t.Fatalf("warnings = %#v", diff.Warnings)
	}
}

func assertPMOEntity(t *testing.T, got PMOEntityDiff, localType PMOLocalType, externalType, externalKey, parentExternalKey string, action PMOEntityAction) {
	t.Helper()
	if got.LocalType != localType || got.ExternalType != externalType || got.ExternalKey != externalKey || got.ParentExternalKey != parentExternalKey || got.Action != action {
		t.Fatalf("entity = %#v", got)
	}
}

func findPMOEntityDiff(t *testing.T, diff PMODiff, externalType, externalKey string) PMOEntityDiff {
	t.Helper()
	for _, entity := range diff.Entities {
		if entity.ExternalType == externalType && entity.ExternalKey == externalKey {
			return entity
		}
	}
	t.Fatalf("missing %s %s in %#v", externalType, externalKey, diff.Entities)
	return PMOEntityDiff{}
}
