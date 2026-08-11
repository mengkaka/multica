package service

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestBuildPMOSyncPromptIsStrictAndInfrastructureAgnostic(t *testing.T) {
	prompt := BuildPMOSyncPrompt("EXT-P-001")
	for _, required := range []string{"EXT-P-001", `"schema_version"`, `"snapshot_complete"`, "JSON only"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("prompt missing %q: %s", required, prompt)
		}
	}
	for _, forbidden := range []string{"://", "skill", "sub-agent", "credential"} {
		if strings.Contains(strings.ToLower(prompt), forbidden) {
			t.Fatalf("prompt exposes infrastructure term %q", forbidden)
		}
	}
}

func TestPMOOrchestrationSquadValidation(t *testing.T) {
	workspaceID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	otherWorkspaceID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	leaderID := pgtype.UUID{Bytes: [16]byte{3}, Valid: true}
	squad := db.Squad{WorkspaceID: workspaceID, LeaderID: leaderID}
	leader := db.Agent{ID: leaderID, WorkspaceID: workspaceID}

	if err := (&PMOService{}).validateOrchestrationSquad(t.Context(), workspaceID, pgtype.UUID{}); err != nil {
		t.Fatalf("nil squad should remain backward compatible: %v", err)
	}
	if !validPMOOrchestrationSquad(workspaceID, squad, leader) {
		t.Fatal("valid same-workspace squad rejected")
	}
	leader.WorkspaceID = otherWorkspaceID
	if validPMOOrchestrationSquad(workspaceID, squad, leader) {
		t.Fatal("cross-workspace leader accepted")
	}
	leader.WorkspaceID = workspaceID
	leader.ArchivedAt = pgtype.Timestamptz{Valid: true}
	if validPMOOrchestrationSquad(workspaceID, squad, leader) {
		t.Fatal("archived agent leader accepted")
	}
	leader.ArchivedAt = pgtype.Timestamptz{}
	leader.ID = pgtype.UUID{Bytes: [16]byte{4}, Valid: true}
	if validPMOOrchestrationSquad(workspaceID, squad, leader) {
		t.Fatal("squad without its configured agent leader accepted")
	}
}
