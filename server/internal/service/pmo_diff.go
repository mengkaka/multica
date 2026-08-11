package service

import (
	"reflect"
	"strings"
)

type PMOFieldDecision string

const (
	PMOUnchanged PMOFieldDecision = "unchanged"
	PMOIncoming  PMOFieldDecision = "incoming"
	PMOLocalOnly PMOFieldDecision = "local_only"
	PMOConverged PMOFieldDecision = "converged"
	PMOConflict  PMOFieldDecision = "conflict"
)

type PMOLocalType string

const (
	PMOLocalProject PMOLocalType = "project"
	PMOLocalIssue   PMOLocalType = "issue"
)

type PMOEntityAction string

const (
	PMOCreate          PMOEntityAction = "create"
	PMOUpdate          PMOEntityAction = "update"
	PMOEntityUnchanged PMOEntityAction = "unchanged"
	PMOExternalRemoved PMOEntityAction = "external_removed"
)

const PMOWarningUnresolvedAssignee = "unresolved_assignee"

type PMOFieldDiff struct {
	BaselineExternal any              `json:"baseline_external"`
	BaselineLocal    any              `json:"baseline_local"`
	External         any              `json:"external"`
	Local            any              `json:"local"`
	Decision         PMOFieldDecision `json:"decision"`
}

type PMOEntityDiff struct {
	ExternalType       string                  `json:"external_type"`
	ExternalKey        string                  `json:"external_key"`
	LocalType          PMOLocalType            `json:"local_type"`
	LocalID            string                  `json:"local_id,omitempty"`
	ProjectExternalKey string                  `json:"project_external_key,omitempty"`
	ParentExternalKey  string                  `json:"parent_external_key,omitempty"`
	Action             PMOEntityAction         `json:"action"`
	Fields             map[string]PMOFieldDiff `json:"fields"`
}

type PMODiffWarning struct {
	Code         string `json:"code"`
	ExternalID   string `json:"external_id"`
	DisplayName  string `json:"display_name"`
	ExternalType string `json:"external_type"`
	ExternalKey  string `json:"external_key"`
	Field        string `json:"field"`
}

type PMODiffSummary struct {
	Creates             int `json:"creates"`
	IncomingFields      int `json:"incoming_fields"`
	LocalOnlyFields     int `json:"local_only_fields"`
	ConvergedFields     int `json:"converged_fields"`
	Conflicts           int `json:"conflicts"`
	ExternalRemoved     int `json:"external_removed"`
	UnresolvedAssignees int `json:"unresolved_assignees"`
}

type PMODiff struct {
	Entities []PMOEntityDiff  `json:"entities"`
	Warnings []PMODiffWarning `json:"warnings"`
	Summary  PMODiffSummary   `json:"summary"`
}

type PMOLinkState struct {
	ExternalType      string
	ExternalKey       string
	LocalType         PMOLocalType
	LocalID           string
	BaselineExternal  map[string]any
	BaselineLocal     map[string]any
	CurrentLocal      map[string]any
	ExternallyRemoved bool
}

type PMODiffInput struct {
	Snapshot         PMOSnapshot
	Links            []PMOLinkState
	AssigneeMappings map[string]string
}

type pmoSourceEntity struct {
	externalType       string
	externalKey        string
	localType          PMOLocalType
	projectExternalKey string
	parentExternalKey  string
	fields             map[string]any
	owner              *PMOExternalOwner
	ownerField         string
}

func DiffPMOField(externalBase, localBase, externalNow, localNow any) PMOFieldDecision {
	externalChanged := !reflect.DeepEqual(externalBase, externalNow)
	localChanged := !reflect.DeepEqual(localBase, localNow)
	switch {
	case !externalChanged && !localChanged:
		return PMOUnchanged
	case externalChanged && !localChanged:
		return PMOIncoming
	case !externalChanged && localChanged:
		return PMOLocalOnly
	case reflect.DeepEqual(externalNow, localNow):
		return PMOConverged
	default:
		return PMOConflict
	}
}

func DiffPMOSnapshot(input PMODiffInput) PMODiff {
	links := make(map[string]PMOLinkState, len(input.Links))
	for _, link := range input.Links {
		links[pmoIdentity(link.ExternalType, link.ExternalKey)] = link
	}

	sources := pmoSnapshotEntities(input.Snapshot)
	diff := PMODiff{
		Entities: make([]PMOEntityDiff, 0, len(sources)+len(input.Links)),
		Warnings: []PMODiffWarning{},
	}
	seen := make(map[string]struct{}, len(sources))
	warnedAssignees := make(map[string]struct{})

	for _, source := range sources {
		identity := pmoIdentity(source.externalType, source.externalKey)
		seen[identity] = struct{}{}
		link, linked := links[identity]
		fields := clonePMOFields(source.fields)

		if source.owner == nil {
			fields[source.ownerField] = nil
		} else if localID := input.AssigneeMappings[source.owner.ExternalID]; localID != "" {
			fields[source.ownerField] = localID
		} else if _, warned := warnedAssignees[source.owner.ExternalID]; !warned {
			warnedAssignees[source.owner.ExternalID] = struct{}{}
			diff.Warnings = append(diff.Warnings, PMODiffWarning{
				Code:         PMOWarningUnresolvedAssignee,
				ExternalID:   source.owner.ExternalID,
				DisplayName:  source.owner.DisplayName,
				ExternalType: source.externalType,
				ExternalKey:  source.externalKey,
				Field:        source.ownerField,
			})
		}

		if source.localType == PMOLocalIssue {
			if projectLink, ok := links[pmoIdentity("requirement", source.projectExternalKey)]; ok && projectLink.LocalID != "" {
				fields["project_id"] = projectLink.LocalID
			}
			if parentLink, ok := links[pmoIdentity("requirement", source.parentExternalKey)]; ok && parentLink.LocalType == PMOLocalIssue && parentLink.LocalID != "" {
				fields["parent_issue_id"] = parentLink.LocalID
			} else if source.parentExternalKey == source.projectExternalKey {
				fields["parent_issue_id"] = nil
			}
		}

		entity := PMOEntityDiff{
			ExternalType:       source.externalType,
			ExternalKey:        source.externalKey,
			LocalType:          source.localType,
			ProjectExternalKey: source.projectExternalKey,
			ParentExternalKey:  source.parentExternalKey,
			Fields:             map[string]PMOFieldDiff{},
		}
		if !linked || link.LocalID == "" {
			entity.Action = PMOCreate
			for field, value := range fields {
				entity.Fields[field] = PMOFieldDiff{External: value, Decision: PMOIncoming}
			}
			diff.Summary.Creates++
		} else {
			entity.LocalID = link.LocalID
			entity.Action = PMOEntityUnchanged
			for field, externalNow := range fields {
				fieldDiff := PMOFieldDiff{
					BaselineExternal: link.BaselineExternal[field],
					BaselineLocal:    link.BaselineLocal[field],
					External:         externalNow,
					Local:            link.CurrentLocal[field],
				}
				fieldDiff.Decision = DiffPMOField(fieldDiff.BaselineExternal, fieldDiff.BaselineLocal, fieldDiff.External, fieldDiff.Local)
				entity.Fields[field] = fieldDiff
				if fieldDiff.Decision != PMOUnchanged {
					entity.Action = PMOUpdate
				}
				countPMOFieldDecision(&diff.Summary, fieldDiff.Decision)
			}
		}
		diff.Entities = append(diff.Entities, entity)
	}

	for _, link := range input.Links {
		if link.ExternalType == "assignee" {
			continue
		}
		if _, exists := seen[pmoIdentity(link.ExternalType, link.ExternalKey)]; exists {
			continue
		}
		diff.Entities = append(diff.Entities, PMOEntityDiff{
			ExternalType: link.ExternalType,
			ExternalKey:  link.ExternalKey,
			LocalType:    link.LocalType,
			LocalID:      link.LocalID,
			Action:       PMOExternalRemoved,
			Fields:       map[string]PMOFieldDiff{},
		})
		diff.Summary.ExternalRemoved++
	}
	diff.Summary.UnresolvedAssignees = len(diff.Warnings)
	return diff
}

func pmoSnapshotEntities(snapshot PMOSnapshot) []pmoSourceEntity {
	entities := make([]pmoSourceEntity, 0, 1+len(snapshot.Children)+len(snapshot.Tasks))
	entities = append(entities, pmoRequirementEntity(snapshot.Parent, PMOLocalProject, "", ""))
	for _, child := range snapshot.Children {
		entities = append(entities, pmoRequirementEntity(child, PMOLocalIssue, snapshot.Parent.Key, snapshot.Parent.Key))
	}
	for _, task := range snapshot.Tasks {
		entities = append(entities, pmoTaskEntity(task, snapshot.Parent.Key, snapshot.Parent.Key))
	}
	for _, child := range snapshot.Children {
		for _, task := range child.Tasks {
			entities = append(entities, pmoTaskEntity(task, snapshot.Parent.Key, child.Key))
		}
	}
	return entities
}

func pmoRequirementEntity(requirement PMORequirement, localType PMOLocalType, projectExternalKey, parentExternalKey string) pmoSourceEntity {
	ownerField := "assignee_id"
	fields := map[string]any{
		"title":       requirement.Title,
		"description": requirement.Description,
		"status":      requirement.Status,
		"start_date":  pmoStringValue(requirement.StartDate),
		"due_date":    pmoStringValue(requirement.DueDate),
	}
	if localType == PMOLocalProject {
		ownerField = "lead_id"
		fields["title"] = pmoProjectTitle(requirement)
	} else {
		fields["workload"] = pmoFloatValue(requirement.Workload)
	}
	return pmoSourceEntity{
		externalType:       "requirement",
		externalKey:        requirement.Key,
		localType:          localType,
		projectExternalKey: projectExternalKey,
		parentExternalKey:  parentExternalKey,
		owner:              requirement.Owner,
		ownerField:         ownerField,
		fields:             fields,
	}
}

func pmoProjectTitle(requirement PMORequirement) string {
	return strings.TrimSpace(requirement.DisplayNumber) + " " + strings.TrimSpace(requirement.Title)
}

func pmoTaskEntity(task PMOTask, projectExternalKey, parentExternalKey string) pmoSourceEntity {
	return pmoSourceEntity{
		externalType:       "task",
		externalKey:        task.TaskID,
		localType:          PMOLocalIssue,
		projectExternalKey: projectExternalKey,
		parentExternalKey:  parentExternalKey,
		owner:              task.Owner,
		ownerField:         "assignee_id",
		fields: map[string]any{
			"title":       task.Title,
			"description": task.Description,
			"status":      task.Status,
			"start_date":  pmoStringValue(task.StartDate),
			"due_date":    pmoStringValue(task.DueDate),
			"workload":    pmoFloatValue(task.Workload),
		},
	}
}

func countPMOFieldDecision(summary *PMODiffSummary, decision PMOFieldDecision) {
	switch decision {
	case PMOIncoming:
		summary.IncomingFields++
	case PMOLocalOnly:
		summary.LocalOnlyFields++
	case PMOConverged:
		summary.ConvergedFields++
	case PMOConflict:
		summary.Conflicts++
	}
}

func pmoIdentity(externalType, externalKey string) string {
	return externalType + "\x00" + externalKey
}

func clonePMOFields(fields map[string]any) map[string]any {
	cloned := make(map[string]any, len(fields)+3)
	for key, value := range fields {
		cloned[key] = value
	}
	return cloned
}

func pmoStringValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func pmoFloatValue(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}
