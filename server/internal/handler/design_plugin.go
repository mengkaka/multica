package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/designcore"
	"github.com/multica-ai/multica/server/internal/storage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	designPluginProviderFigma         = "figma"
	designPluginScopeImport           = "design_import"
	designPluginAuthTTL               = 15 * time.Minute
	designPluginAssetTypeDesign       = "design"
	designPluginAssetTypeTemplate     = "template"
	designPluginAssetTypeDesignSystem = "design_system"
)

type CreateDesignPluginAuthSessionResponse struct {
	SessionID    string `json:"session_id"`
	UserCode     string `json:"user_code"`
	AuthorizeURL string `json:"authorize_url"`
	ExpiresAt    string `json:"expires_at"`
}

type PollDesignPluginAuthSessionResponse struct {
	Status      string  `json:"status"`
	Token       *string `json:"token,omitempty"`
	WorkspaceID *string `json:"workspace_id,omitempty"`
	ExpiresAt   *string `json:"expires_at,omitempty"`
}

type AuthorizeDesignPluginAuthSessionRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type ImportFigmaDesignWithTokenRequest struct {
	Title                   string          `json:"title"`
	Description             *string         `json:"description"`
	ProjectID               string          `json:"project_id"`
	FolderID                string          `json:"folder_id"`
	TargetDesignFileID      string          `json:"target_design_file_id"`
	DesignFileTitle         string          `json:"design_file_title"`
	AssetType               string          `json:"asset_type"`
	SourceRef               json.RawMessage `json:"source_ref"`
	NativeJSON              json.RawMessage `json:"native_json"`
	PublishAsTemplate       bool            `json:"publish_as_template"`
	TemplateLibraryKey      string          `json:"template_library_key"`
	TemplateKey             string          `json:"template_key"`
	TemplateName            string          `json:"template_name"`
	TemplateCategory        string          `json:"template_category"`
	TemplateSlotSchema      json.RawMessage `json:"template_slot_schema"`
	DesignSystemName        string          `json:"design_system_name"`
	DesignSystemDescription string          `json:"design_system_description"`
}

type UploadFigmaDesignAssetResponse struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	Filename    string `json:"filename"`
	Kind        string `json:"kind"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

type CreateFigmaPluginFolderRequest struct {
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
}

type FigmaPluginContextFolder struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id,omitempty"`
}

type FigmaPluginContextDesignFile struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	FolderID *string `json:"folder_id,omitempty"`
}

type FigmaPluginContextProject struct {
	ID          string                         `json:"id"`
	Title       string                         `json:"title"`
	Folders     []FigmaPluginContextFolder     `json:"folders"`
	DesignFiles []FigmaPluginContextDesignFile `json:"design_files"`
}

type FigmaPluginContextResponse struct {
	Workspace struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"workspace"`
	User struct {
		ID        string  `json:"id"`
		Name      string  `json:"name"`
		Email     string  `json:"email"`
		AvatarURL *string `json:"avatar_url,omitempty"`
	} `json:"user"`
	Projects []FigmaPluginContextProject `json:"projects"`
}

func randomHexToken(prefix string, bytesLen int) (string, error) {
	b := make([]byte, bytesLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}

func pluginAuthorizeBaseURL() string {
	if raw := strings.TrimRight(strings.TrimSpace(os.Getenv("MULTICA_PUBLIC_URL")), "/"); raw != "" {
		return raw
	}
	if raw := strings.TrimRight(strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN")), "/"); raw != "" {
		return raw
	}
	return "http://localhost:3031"
}

func (h *Handler) resolveFigmaPluginToken(w http.ResponseWriter, r *http.Request) (db.DesignPluginToken, bool) {
	authHeader := r.Header.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == authHeader || !strings.HasPrefix(token, "mfp_") {
		writeError(w, http.StatusUnauthorized, "invalid plugin token")
		return db.DesignPluginToken{}, false
	}
	pluginToken, err := h.Queries.GetDesignPluginTokenByHash(r.Context(), db.GetDesignPluginTokenByHashParams{TokenHash: auth.HashToken(token), Provider: designPluginProviderFigma})
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid plugin token")
		return db.DesignPluginToken{}, false
	}
	if pluginToken.Scope != designPluginScopeImport {
		writeError(w, http.StatusForbidden, "plugin token scope denied")
		return db.DesignPluginToken{}, false
	}
	if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{UserID: pluginToken.UserID, WorkspaceID: pluginToken.WorkspaceID}); err != nil {
		writeError(w, http.StatusForbidden, "workspace access denied")
		return db.DesignPluginToken{}, false
	}
	return pluginToken, true
}

func (h *Handler) CreateFigmaPluginAuthSession(w http.ResponseWriter, r *http.Request) {
	userCode, err := randomHexToken("", 4)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create auth session")
		return
	}
	expiresAt := time.Now().Add(designPluginAuthTTL)
	session, err := h.Queries.CreateDesignPluginAuthSession(r.Context(), db.CreateDesignPluginAuthSessionParams{
		Provider:  designPluginProviderFigma,
		UserCode:  strings.ToUpper(userCode),
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create auth session")
		return
	}
	sessionID := uuidToString(session.ID)
	writeJSON(w, http.StatusCreated, CreateDesignPluginAuthSessionResponse{
		SessionID:    sessionID,
		UserCode:     session.UserCode,
		AuthorizeURL: pluginAuthorizeBaseURL() + "/design-plugin/figma/authorize?session_id=" + sessionID,
		ExpiresAt:    timestampToString(session.ExpiresAt),
	})
}

func (h *Handler) PollFigmaPluginAuthSession(w http.ResponseWriter, r *http.Request) {
	sessionUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "sessionId"), "session id")
	if !ok {
		return
	}
	session, err := h.Queries.GetDesignPluginAuthSession(r.Context(), db.GetDesignPluginAuthSessionParams{ID: sessionUUID, Provider: designPluginProviderFigma})
	if err != nil {
		writeJSON(w, http.StatusOK, PollDesignPluginAuthSessionResponse{Status: "expired"})
		return
	}
	if session.ConsumedAt.Valid {
		writeJSON(w, http.StatusOK, PollDesignPluginAuthSessionResponse{Status: "consumed"})
		return
	}
	if session.ExpiresAt.Time.Before(time.Now()) {
		writeJSON(w, http.StatusOK, PollDesignPluginAuthSessionResponse{Status: "expired"})
		return
	}
	if !session.ApprovedAt.Valid {
		writeJSON(w, http.StatusOK, PollDesignPluginAuthSessionResponse{Status: "pending"})
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to poll auth session")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	locked, err := qtx.GetConsumableDesignPluginAuthSessionForUpdate(r.Context(), db.GetConsumableDesignPluginAuthSessionForUpdateParams{ID: sessionUUID, Provider: designPluginProviderFigma})
	if err != nil {
		writeJSON(w, http.StatusOK, PollDesignPluginAuthSessionResponse{Status: "consumed"})
		return
	}
	token, err := randomHexToken("mfp_", 32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create plugin token")
		return
	}
	if _, err := qtx.CreateDesignPluginToken(r.Context(), db.CreateDesignPluginTokenParams{
		Provider:    designPluginProviderFigma,
		TokenHash:   auth.HashToken(token),
		TokenPrefix: token[:12],
		UserID:      locked.UserID,
		WorkspaceID: locked.WorkspaceID,
		Scope:       designPluginScopeImport,
		Name:        "Figma Plugin",
		ExpiresAt:   pgtype.Timestamptz{},
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create plugin token")
		return
	}
	if err := qtx.ConsumeDesignPluginAuthSession(r.Context(), locked.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to consume auth session")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to poll auth session")
		return
	}
	workspaceID := uuidToString(locked.WorkspaceID)
	writeJSON(w, http.StatusOK, PollDesignPluginAuthSessionResponse{Status: "approved", Token: &token, WorkspaceID: &workspaceID})
}

func (h *Handler) AuthorizeFigmaPluginAuthSession(w http.ResponseWriter, r *http.Request) {
	sessionUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "sessionId"), "session id")
	if !ok {
		return
	}
	var req AuthorizeDesignPluginAuthSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, req.WorkspaceID, "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{UserID: userUUID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusForbidden, "workspace access denied")
		return
	}
	session, err := h.Queries.ApproveDesignPluginAuthSession(r.Context(), db.ApproveDesignPluginAuthSessionParams{ID: sessionUUID, Provider: designPluginProviderFigma, UserID: userUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusBadRequest, "auth session expired or already used")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "approved", "session_id": uuidToString(session.ID)})
}

func (h *Handler) GetFigmaPluginContext(w http.ResponseWriter, r *http.Request) {
	pluginToken, ok := h.resolveFigmaPluginToken(w, r)
	if !ok {
		return
	}
	workspace, err := h.Queries.GetWorkspace(r.Context(), pluginToken.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	user, err := h.Queries.GetUser(r.Context(), pluginToken.UserID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	projects, err := h.Queries.ListProjects(r.Context(), db.ListProjectsParams{WorkspaceID: pluginToken.WorkspaceID, ArchivedMode: "active"})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load projects")
		return
	}
	resp := FigmaPluginContextResponse{Projects: make([]FigmaPluginContextProject, 0, len(projects))}
	resp.Workspace.ID = uuidToString(workspace.ID)
	resp.Workspace.Name = workspace.Name
	resp.Workspace.Slug = workspace.Slug
	resp.User.ID = uuidToString(user.ID)
	resp.User.Name = user.Name
	resp.User.Email = user.Email
	resp.User.AvatarURL = textToPtr(user.AvatarUrl)
	for _, project := range projects {
		folders, err := h.Queries.ListDesignFolders(r.Context(), db.ListDesignFoldersParams{WorkspaceID: pluginToken.WorkspaceID, ProjectID: project.ID})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load folders")
			return
		}
		files, err := h.Queries.ListDesignFilesByProject(r.Context(), db.ListDesignFilesByProjectParams{WorkspaceID: pluginToken.WorkspaceID, ProjectID: project.ID})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load design files")
			return
		}
		projectResp := FigmaPluginContextProject{ID: uuidToString(project.ID), Title: project.Title, Folders: make([]FigmaPluginContextFolder, 0, len(folders)), DesignFiles: make([]FigmaPluginContextDesignFile, 0, len(files))}
		for _, folder := range folders {
			projectResp.Folders = append(projectResp.Folders, FigmaPluginContextFolder{ID: uuidToString(folder.ID), Name: folder.Name, ParentID: uuidToPtr(folder.ParentID)})
		}
		for _, file := range files {
			projectResp.DesignFiles = append(projectResp.DesignFiles, FigmaPluginContextDesignFile{ID: uuidToString(file.ID), Title: file.Title, FolderID: uuidToPtr(file.FolderID)})
		}
		resp.Projects = append(resp.Projects, projectResp)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) UploadFigmaDesignAssetWithPluginToken(w http.ResponseWriter, r *http.Request) {
	pluginToken, ok := h.resolveFigmaPluginToken(w, r)
	if !ok {
		return
	}
	designAssetStorage := h.figmaDesignAssetStorage()
	if designAssetStorage == nil {
		writeError(w, http.StatusServiceUnavailable, "design asset upload not configured")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or invalid multipart form")
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("missing file field: %v", err))
		return
	}
	defer file.Close()

	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, "failed to read file")
		return
	}
	contentType := http.DetectContentType(buf[:n])
	if ct, ok := extContentTypes[strings.ToLower(path.Ext(header.Filename))]; ok {
		contentType = ct
	}
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		writeError(w, http.StatusBadRequest, "design asset must be an image")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}
	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read file")
		return
	}

	id, err := uuid.NewV7()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create asset id")
		return
	}
	ext := strings.ToLower(path.Ext(header.Filename))
	if ext == "" {
		ext = ".png"
	}
	filename := id.String() + ext
	key := "workspaces/" + uuidToString(pluginToken.WorkspaceID) + "/design-assets/" + filename
	url, err := designAssetStorage.Upload(r.Context(), key, data, contentType, header.Filename)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "upload failed")
		return
	}
	go h.Queries.UpdateDesignPluginTokenLastUsed(r.Context(), pluginToken.ID)
	writeJSON(w, http.StatusOK, UploadFigmaDesignAssetResponse{
		ID:          id.String(),
		URL:         url,
		Filename:    header.Filename,
		Kind:        strings.TrimSpace(r.FormValue("kind")),
		ContentType: contentType,
		SizeBytes:   int64(len(data)),
	})
}

func (h *Handler) figmaDesignAssetStorage() storage.Storage {
	if h.DesignAssetStorage != nil {
		return h.DesignAssetStorage
	}
	return h.Storage
}

func (h *Handler) CreateFigmaPluginDesignFolder(w http.ResponseWriter, r *http.Request) {
	pluginToken, ok := h.resolveFigmaPluginToken(w, r)
	if !ok {
		return
	}
	var req CreateFigmaPluginFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	projectUUID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.ProjectID), "project_id")
	if !ok {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectUUID, WorkspaceID: pluginToken.WorkspaceID}); err != nil {
		writeError(w, http.StatusForbidden, "project access denied")
		return
	}
	folder, err := h.Queries.CreateDesignFolder(r.Context(), db.CreateDesignFolderParams{WorkspaceID: pluginToken.WorkspaceID, ProjectID: projectUUID, Name: name, Position: 0, CreatedBy: pluginToken.UserID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create folder")
		return
	}
	go h.Queries.UpdateDesignPluginTokenLastUsed(r.Context(), pluginToken.ID)
	writeJSON(w, http.StatusCreated, FigmaPluginContextFolder{ID: uuidToString(folder.ID), Name: folder.Name, ParentID: uuidToPtr(folder.ParentID)})
}

func (h *Handler) importPluginDesignFileRevision(r *http.Request, workspaceID pgtype.UUID, projectID pgtype.UUID, folderID pgtype.UUID, targetFileID pgtype.UUID, userID pgtype.UUID, title string, descriptionPtr *string, sourceRef json.RawMessage, nativeJSON json.RawMessage) (db.DesignFile, db.DesignRevision, error) {
	var zeroFile db.DesignFile
	var zeroRevision db.DesignRevision
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		return zeroFile, zeroRevision, err
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	var description pgtype.Text
	if descriptionPtr != nil {
		description = pgtype.Text{String: *descriptionPtr, Valid: true}
	}
	file := db.DesignFile{}
	revisionNumber := int32(1)
	revisionJSON := []byte(nativeJSON)
	if targetFileID.Valid {
		file, err = qtx.GetDesignFileInWorkspace(r.Context(), db.GetDesignFileInWorkspaceParams{ID: targetFileID, WorkspaceID: workspaceID})
		if err != nil {
			return zeroFile, zeroRevision, err
		}
		if uuidToString(file.ProjectID) != uuidToString(projectID) || uuidToString(file.FolderID) != uuidToString(folderID) {
			return zeroFile, zeroRevision, fmt.Errorf("target design file is not in selected project/folder")
		}
		if title == "" {
			title = file.Title
		}
		if file.CurrentRevisionID.Valid {
			current, err := qtx.GetDesignRevisionInWorkspace(r.Context(), db.GetDesignRevisionInWorkspaceParams{ID: file.CurrentRevisionID, WorkspaceID: workspaceID})
			if err != nil {
				return zeroFile, zeroRevision, err
			}
			revisionJSON, err = mergeFigmaNativeJSON(current.NativeJson, []byte(nativeJSON))
			if err != nil {
				return zeroFile, zeroRevision, err
			}
		}
		revisionJSON, err = annotateImportFidelityReport(revisionJSON)
		if err != nil {
			return zeroFile, zeroRevision, err
		}
		revisionNumber, err = qtx.GetNextDesignRevisionNumber(r.Context(), file.ID)
		if err != nil {
			return zeroFile, zeroRevision, err
		}
	} else {
		if title == "" {
			title = "Figma import"
		}
		file, err = qtx.CreateDesignFile(r.Context(), db.CreateDesignFileParams{WorkspaceID: workspaceID, ProjectID: projectID, FolderID: folderID, Title: title, Description: description, SourceType: "import", SourceRef: []byte(sourceRef), CreatedBy: userID})
		if err != nil {
			return zeroFile, zeroRevision, err
		}
		revisionJSON, err = annotateImportFidelityReport(revisionJSON)
		if err != nil {
			return zeroFile, zeroRevision, err
		}
	}
	revision, err := qtx.CreateDesignRevision(r.Context(), db.CreateDesignRevisionParams{FileID: file.ID, WorkspaceID: workspaceID, RevisionNumber: revisionNumber, Status: "valid", NativeJson: revisionJSON, ValidationErrors: []byte(`[]`), CreatedBy: userID})
	if err != nil {
		return zeroFile, zeroRevision, err
	}
	file, err = qtx.UpdateDesignFile(r.Context(), db.UpdateDesignFileParams{ID: file.ID, WorkspaceID: workspaceID, Title: pgtype.Text{String: title, Valid: title != ""}, Description: description, ProjectID: projectID, FolderID: folderID, SourceRef: []byte(sourceRef), CurrentRevisionID: revision.ID})
	if err != nil {
		return zeroFile, zeroRevision, err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return zeroFile, zeroRevision, err
	}
	return file, revision, nil
}

func mergeFigmaNativeJSON(existingRaw []byte, incomingRaw []byte) ([]byte, error) {
	existing, err := designcore.ParseNativeJSON(existingRaw)
	if err != nil {
		return nil, err
	}
	incoming, err := designcore.ParseNativeJSON(incomingRaw)
	if err != nil {
		return nil, err
	}
	replacedFrameIDs := map[string]bool{}
	existingFrameBySource := map[string]string{}
	usedFrameIDs := map[string]bool{}
	for _, frame := range existing.Frames {
		usedFrameIDs[frame.ID] = true
		if strings.TrimSpace(frame.SourceNodeID) != "" {
			existingFrameBySource[frame.SourceNodeID] = frame.ID
		}
	}
	nextFrames := make([]designcore.Frame, 0, len(existing.Frames)+len(incoming.Frames))
	for _, frame := range incoming.Frames {
		if existingID, ok := existingFrameBySource[frame.SourceNodeID]; ok && strings.TrimSpace(frame.SourceNodeID) != "" {
			replacedFrameIDs[existingID] = true
		}
	}
	for _, frame := range existing.Frames {
		if !replacedFrameIDs[frame.ID] {
			nextFrames = append(nextFrames, frame)
		}
	}
	if existing.Layers == nil {
		existing.Layers = map[string]designcore.Layer{}
	}
	if existing.Assets == nil {
		existing.Assets = map[string]designcore.Asset{}
	}
	for id, layer := range existing.Layers {
		if replacedFrameIDs[layer.FrameID] {
			delete(existing.Layers, id)
		}
	}
	for id, asset := range existing.Assets {
		if replacedFrameIDs[asset.FrameID] {
			delete(existing.Assets, id)
		}
	}
	for _, incomingFrame := range incoming.Frames {
		oldFrameID := incomingFrame.ID
		targetFrameID := existingFrameBySource[incomingFrame.SourceNodeID]
		if targetFrameID == "" || strings.TrimSpace(incomingFrame.SourceNodeID) == "" {
			targetFrameID = uniqueFrameID(stableFrameID(incomingFrame), usedFrameIDs)
		}
		usedFrameIDs[targetFrameID] = true
		assetIDMap := map[string]string{}
		incomingFrame.ID = targetFrameID
		if incomingFrame.PreviewAssetID != "" {
			assetIDMap[incomingFrame.PreviewAssetID] = "frame_preview-" + targetFrameID
			incomingFrame.PreviewAssetID = assetIDMap[incomingFrame.PreviewAssetID]
		}
		if incomingFrame.ThumbnailAssetID != "" {
			assetIDMap[incomingFrame.ThumbnailAssetID] = "frame_thumbnail-" + targetFrameID
			incomingFrame.ThumbnailAssetID = assetIDMap[incomingFrame.ThumbnailAssetID]
		}
		nextFrames = append(nextFrames, incomingFrame)
		for id, layer := range incoming.Layers {
			if layer.FrameID != oldFrameID {
				continue
			}
			layer.FrameID = targetFrameID
			existing.Layers[id] = layer
		}
		for id, asset := range incoming.Assets {
			if asset.FrameID != "" && asset.FrameID != oldFrameID {
				continue
			}
			if asset.FrameID == oldFrameID {
				asset.FrameID = targetFrameID
			}
			nextID := id
			if mapped := assetIDMap[id]; mapped != "" {
				nextID = mapped
				asset.ID = mapped
			}
			existing.Assets[nextID] = asset
		}
	}
	existing.Frames = nextFrames
	if existing.Source == nil {
		existing.Source = map[string]any{}
	}
	existing.Source["lastMergeSource"] = incoming.Source
	return json.Marshal(existing)
}

func stableFrameID(frame designcore.Frame) string {
	if strings.TrimSpace(frame.SourceNodeID) != "" {
		return "frame-" + cleanDesignID(frame.SourceNodeID)
	}
	return "frame-" + cleanDesignID(frame.Name)
}

func uniqueFrameID(base string, used map[string]bool) string {
	if base == "frame-" || base == "" {
		base = "frame-import"
	}
	if !used[base] {
		return base
	}
	for index := 2; ; index++ {
		candidate := fmt.Sprintf("%s-%d", base, index)
		if !used[candidate] {
			return candidate
		}
	}
}

func cleanDesignID(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func normalizeDesignPluginAssetType(req ImportFigmaDesignWithTokenRequest) (string, error) {
	assetType := strings.TrimSpace(req.AssetType)
	if assetType == "" {
		if req.PublishAsTemplate {
			return designPluginAssetTypeTemplate, nil
		}
		return designPluginAssetTypeDesign, nil
	}
	switch assetType {
	case designPluginAssetTypeDesign, designPluginAssetTypeTemplate, designPluginAssetTypeDesignSystem:
		if assetType == designPluginAssetTypeDesignSystem && req.PublishAsTemplate {
			return "", fmt.Errorf("asset_type conflicts with publish_as_template")
		}
		return assetType, nil
	default:
		return "", fmt.Errorf("unsupported asset_type")
	}
}

func (h *Handler) ImportFigmaDesignWithPluginToken(w http.ResponseWriter, r *http.Request) {
	pluginToken, ok := h.resolveFigmaPluginToken(w, r)
	if !ok {
		return
	}

	var req ImportFigmaDesignWithTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		req.Title = "Figma import"
	}
	assetType, err := normalizeDesignPluginAssetType(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	projectUUID, ok := parseOptionalUUIDOrBadRequest(w, req.ProjectID, "project_id")
	if !ok {
		return
	}
	folderUUID, ok := parseOptionalUUIDOrBadRequest(w, req.FolderID, "folder_id")
	if !ok {
		return
	}
	if !h.validateDesignProjectFolder(w, r, pluginToken.WorkspaceID, projectUUID, folderUUID, true) {
		return
	}
	targetFileUUID, ok := parseOptionalUUIDOrBadRequest(w, req.TargetDesignFileID, "target_design_file_id")
	if !ok {
		return
	}
	if assetType == designPluginAssetTypeTemplate || assetType == designPluginAssetTypeDesignSystem {
		folderUUID = pgtype.UUID{}
		targetFileUUID = pgtype.UUID{}
	}
	if len(req.NativeJSON) == 0 {
		writeError(w, http.StatusBadRequest, "native_json is required")
		return
	}
	validation := designcore.ValidateNativeJSON(req.NativeJSON)
	if !validation.Valid {
		writeJSON(w, http.StatusBadRequest, validation)
		return
	}
	if err := validateNativeJSONNoEmbeddedBinary(req.NativeJSON); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sourceRef := req.SourceRef
	if len(sourceRef) == 0 {
		sourceRef = json.RawMessage(`{}`)
	}
	if !json.Valid(sourceRef) {
		writeError(w, http.StatusBadRequest, "source_ref must be valid JSON")
		return
	}
	title := strings.TrimSpace(req.DesignFileTitle)
	if title == "" && !targetFileUUID.Valid {
		title = req.Title
	}
	file, revision, err := h.importPluginDesignFileRevision(r, pluginToken.WorkspaceID, projectUUID, folderUUID, targetFileUUID, pluginToken.UserID, title, req.Description, sourceRef, req.NativeJSON)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to import figma design")
		return
	}
	var templateResp *DesignCatalogTemplateResponse
	if assetType == designPluginAssetTypeTemplate {
		libraryKey := slugOrDefault(req.TemplateLibraryKey, "figma")
		libraryName := "Figma Templates"
		name := strings.TrimSpace(req.TemplateName)
		if name == "" {
			name = req.Title
		}
		templateKey := slugOrDefault(req.TemplateKey, name+"-"+randomHex(4))
		category := strings.TrimSpace(req.TemplateCategory)
		if category == "" {
			category = "figma"
		}
		slotSchema := validJSONOrDefault(req.TemplateSlotSchema, `{}`)
		tx, err := h.TxStarter.Begin(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to start template transaction")
			return
		}
		defer tx.Rollback(r.Context())
		qtx := h.Queries.WithTx(tx)
		baseMetadata, _ := json.Marshal(map[string]any{"project_id": uuidToString(projectUUID), "source": "figma_plugin"})
		metadataRaw := designTemplateMetadataWithProfile(baseMetadata, revision.NativeJson, name, category)
		library, err := qtx.EnsureDesignTemplateLibrary(r.Context(), db.EnsureDesignTemplateLibraryParams{WorkspaceID: pluginToken.WorkspaceID, Key: libraryKey, Name: libraryName, Description: pgtype.Text{}, Metadata: []byte(`{}`), CreatedBy: pluginToken.UserID})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to ensure template library")
			return
		}
		template, err := qtx.GetDesignCatalogTemplateByKey(r.Context(), db.GetDesignCatalogTemplateByKeyParams{WorkspaceID: pluginToken.WorkspaceID, LibraryID: library.ID, Key: templateKey})
		if err == pgx.ErrNoRows {
			template, err = qtx.CreateDesignCatalogTemplate(r.Context(), db.CreateDesignCatalogTemplateParams{WorkspaceID: pluginToken.WorkspaceID, LibraryID: library.ID, Key: templateKey, Name: name, Description: ptrToText(req.Description), Category: category, Metadata: metadataRaw, CreatedBy: pluginToken.UserID})
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create design template")
			return
		}
		nextNumber, err := qtx.GetNextDesignTemplateRevisionNumber(r.Context(), template.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get next template revision")
			return
		}
		templateRevision, err := qtx.CreateDesignTemplateRevision(r.Context(), db.CreateDesignTemplateRevisionParams{WorkspaceID: pluginToken.WorkspaceID, TemplateID: template.ID, DesignRevisionID: revision.ID, RevisionNumber: nextNumber, Status: "published", SlotSchema: slotSchema, Metadata: metadataRaw, CreatedBy: pluginToken.UserID})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create template revision")
			return
		}
		updated, err := qtx.UpdateDesignCatalogTemplateCurrentRevision(r.Context(), db.UpdateDesignCatalogTemplateCurrentRevisionParams{ID: template.ID, WorkspaceID: pluginToken.WorkspaceID, CurrentRevisionID: templateRevision.ID})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update design template")
			return
		}
		row, err := qtx.GetDesignCatalogTemplate(r.Context(), db.GetDesignCatalogTemplateParams{ID: updated.ID, WorkspaceID: pluginToken.WorkspaceID})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load design template")
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit design template")
			return
		}
		resp := designCatalogTemplateRowToResponse(row)
		resp.ThumbnailURL = thumbnailFromNativeJSON(revision.NativeJson)
		templateResp = &resp
	}
	var designSystemResp *DesignSystemProfileResponse
	if assetType == designPluginAssetTypeDesignSystem {
		name := strings.TrimSpace(req.DesignSystemName)
		if name == "" {
			name = req.Title
		}
		description := strings.TrimSpace(req.DesignSystemDescription)
		if description == "" && req.Description != nil {
			description = strings.TrimSpace(*req.Description)
		}
		profile, err := h.createDesignSystemProfileFromRevision(r.Context(), createDesignSystemProfileFromRevisionParams{
			WorkspaceID: pluginToken.WorkspaceID,
			ProjectID:   projectUUID,
			File:        file,
			Revision:    revision,
			Name:        name,
			Description: description,
			IsDefault:   true,
			CreatedBy:   pluginToken.UserID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create design system")
			return
		}
		resp := h.designSystemProfileToResponseWithThumbnail(r.Context(), profile)
		designSystemResp = &resp
	}
	go h.Queries.UpdateDesignPluginTokenLastUsed(r.Context(), pluginToken.ID)
	h.publishDesignReady(r, file, revision, pluginToken.UserID, templateResp)
	revisionResp := designRevisionToResponse(revision)
	writeJSON(w, http.StatusCreated, struct {
		DesignFileDetailResponse
		Template     *DesignCatalogTemplateResponse `json:"template,omitempty"`
		DesignSystem *DesignSystemProfileResponse   `json:"design_system,omitempty"`
	}{DesignFileDetailResponse: DesignFileDetailResponse{File: designFileToResponse(file), CurrentRevision: &revisionResp}, Template: templateResp, DesignSystem: designSystemResp})
}
