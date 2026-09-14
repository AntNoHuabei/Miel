package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"trpc.group/trpc-go/trpc-agent-go/artifact"
)

const artifactByteLimit int64 = 64 << 20
const artifactImportLimit int64 = 512 << 20
const artifactTextLimit int64 = 1 << 20

var ErrArtifactTooLarge = errors.New("产物超过读取或导入大小限制")

type ArtifactRef struct {
	ID           string `json:"id"`
	Version      int    `json:"version"`
	Name         string `json:"name"`
	MimeType     string `json:"mimeType"`
	Kind         string `json:"kind"`
	Size         int64  `json:"size"`
	Availability string `json:"availability"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
}

type ArtifactPreview struct {
	Artifact  ArtifactRef `json:"artifact"`
	URL       string      `json:"url"`
	Text      string      `json:"text"`
	Truncated bool        `json:"truncated"`
}

type ArtifactPicker interface {
	PickArtifact() (string, error)
	PickArtifactExport(name string) (string, error)
}

type ArtifactService struct {
	db     *sql.DB
	dirs   *DirectoryManager
	mu     sync.Mutex
	token  string
	picker ArtifactPicker
	agent  *AgentService
	notify func(string, any)
}

var _ artifact.Service = (*ArtifactService)(nil)

func artifactID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func NewArtifactService(db *sql.DB, dirs *DirectoryManager) (*ArtifactService, error) {
	s := &ArtifactService{db: db, dirs: dirs, token: artifactID()}
	for _, kind := range []DirectoryKind{DirectoryArtifactFiles, DirectoryArtifactCache, DirectoryArtifactTrash} {
		if _, err := dirs.Ensure(kind); err != nil {
			return nil, err
		}
	}
	// A blob without a committed version is never visible and can be recovered safely.
	rows, err := db.Query("SELECT blob_name FROM artifact_versions")
	if err != nil {
		return nil, err
	}
	committed := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		committed[name] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dirs.Path(DirectoryArtifactFiles))
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() && (strings.HasPrefix(entry.Name(), ".pending-") || (strings.HasSuffix(entry.Name(), ".blob") && !committed[entry.Name()])) {
			if err := os.Remove(filepath.Join(dirs.Path(DirectoryArtifactFiles), entry.Name())); err != nil {
				return nil, err
			}
		}
	}
	return s, nil
}

//wails:ignore
func (s *ArtifactService) SetPicker(p ArtifactPicker) { s.picker = p }

func artifactKind(name, contentType string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch {
	case strings.HasPrefix(contentType, "image/"):
		return "image"
	case strings.HasPrefix(contentType, "audio/"):
		return "audio"
	case strings.HasPrefix(contentType, "video/"):
		return "video"
	case ext == ".csv":
		return "csv"
	case ext == ".pdf" || contentType == "application/pdf":
		return "pdf"
	case ext == ".md" || ext == ".markdown":
		return "markdown"
	case ext == ".html" || ext == ".htm":
		return "html"
	case strings.Contains("|.go|.js|.jsx|.ts|.tsx|.py|.rs|.java|.c|.cpp|.h|.css|.json|.yaml|.yml|.sql|.sh|.ps1|.xml|", "|"+ext+"|"):
		return "code"
	case strings.HasPrefix(contentType, "text/"):
		return "text"
	default:
		return "file"
	}
}

func validArtifactName(name string) bool {
	return strings.TrimSpace(name) != "" && len(name) <= 240 && !strings.ContainsAny(name, "\x00\r\n")
}

//wails:ignore
func (s *ArtifactService) SaveArtifact(ctx context.Context, info artifact.SessionInfo, name string, data *artifact.Artifact) (int, error) {
	if data == nil || int64(len(data.Data)) > artifactByteLimit {
		return 0, ErrArtifactTooLarge
	}
	ref, _, err := s.save(ctx, info, name, data.MimeType, bytes.NewReader(data.Data), artifactByteLimit)
	return ref.Version, err
}

func (s *ArtifactService) save(ctx context.Context, info artifact.SessionInfo, name, contentType string, reader io.Reader, limit int64) (ArtifactRef, string, error) {
	if !validArtifactName(name) || info.AppName == "" || info.UserID == "" || info.SessionID == "" {
		return ArtifactRef{}, "", errors.New("产物名称或会话无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return ArtifactRef{}, "", err
	}
	tmp, err := os.CreateTemp(s.dirs.Path(DirectoryArtifactFiles), ".pending-")
	if err != nil {
		return ArtifactRef{}, "", err
	}
	defer os.Remove(tmp.Name())
	size, err := io.Copy(tmp, io.LimitReader(reader, limit+1))
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return ArtifactRef{}, "", err
	}
	if size > limit {
		return ArtifactRef{}, "", ErrArtifactTooLarge
	}
	if err := ctx.Err(); err != nil {
		return ArtifactRef{}, "", err
	}
	if contentType == "" {
		contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	}
	if contentType == "" {
		f, err := os.Open(tmp.Name())
		if err != nil {
			return ArtifactRef{}, "", err
		}
		var head [512]byte
		n, _ := f.Read(head[:])
		f.Close()
		contentType = http.DetectContentType(head[:n])
	}
	contentType, _, err = mime.ParseMediaType(contentType)
	if err != nil {
		return ArtifactRef{}, "", errors.New("产物 MIME 类型无效")
	}
	kind := artifactKind(name, contentType)
	width, height := 0, 0
	if kind == "image" {
		f, err := os.Open(tmp.Name())
		if err == nil {
			if cfg, _, err := image.DecodeConfig(f); err == nil {
				width, height = cfg.Width, cfg.Height
			}
			f.Close()
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ArtifactRef{}, "", err
	}
	defer tx.Rollback()
	id, version, deleted := "", 0, 0
	err = tx.QueryRowContext(ctx, "SELECT id, deleted FROM artifacts WHERE app_name=? AND user_id=? AND session_id=? AND name=?", info.AppName, info.UserID, info.SessionID, name).Scan(&id, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		id = artifactID()
		_, err = tx.ExecContext(ctx, "INSERT INTO artifacts(id,app_name,user_id,session_id,name,created_at) VALUES(?,?,?,?,?,?)", id, info.AppName, info.UserID, info.SessionID, name, now())
	} else if err == nil {
		if deleted != 0 {
			return ArtifactRef{}, "", errors.New("同名产物已删除，请恢复后再生成新版本")
		}
		err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),-1)+1 FROM artifact_versions WHERE artifact_id=?", id).Scan(&version)
	}
	if err != nil {
		return ArtifactRef{}, "", err
	}
	blob := artifactID() + ".blob"
	path := filepath.Join(s.dirs.Path(DirectoryArtifactFiles), blob)
	if err := os.Rename(tmp.Name(), path); err != nil {
		return ArtifactRef{}, "", err
	}
	committed := false
	defer func() {
		if !committed {
			os.Remove(path)
		}
	}()
	_, err = tx.ExecContext(ctx, "INSERT INTO artifact_versions(artifact_id,version,mime_type,kind,size,blob_name,width,height,created_at) VALUES(?,?,?,?,?,?,?,?,?)", id, version, contentType, kind, size, blob, width, height, now())
	if err != nil {
		return ArtifactRef{}, "", err
	}
	if scope, ok := ctx.Value(artifactScopeKey{}).(artifactScope); ok && scope.ConversationID > 0 {
		_, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO artifact_links(artifact_id,version,conversation_id,request_id,user_message_id) VALUES(?,?,?,?,?)", id, version, scope.ConversationID, scope.RequestID, scope.UserMessageID)
		if err != nil {
			return ArtifactRef{}, "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return ArtifactRef{}, "", err
	}
	committed = true
	if s.notify != nil {
		s.notify("artifacts.changed", "saved")
	}
	return ArtifactRef{ID: id, Version: version, Name: name, MimeType: contentType, Kind: kind, Size: size, Availability: "available", Width: width, Height: height}, path, nil
}

func (s *ArtifactService) refPath(id string, version int) (ArtifactRef, string, error) {
	var ref ArtifactRef
	var blob string
	var deleted int
	err := s.db.QueryRow(`SELECT a.id,v.version,a.name,v.mime_type,v.kind,v.size,a.deleted,v.blob_name,v.width,v.height FROM artifacts a JOIN artifact_versions v ON a.id=v.artifact_id WHERE a.id=? AND v.version=?`, id, version).Scan(&ref.ID, &ref.Version, &ref.Name, &ref.MimeType, &ref.Kind, &ref.Size, &deleted, &blob, &ref.Width, &ref.Height)
	if err != nil {
		return ref, "", err
	}
	if filepath.Base(blob) != blob {
		return ref, "", errors.New("产物存储路径无效")
	}
	kind := DirectoryArtifactFiles
	ref.Availability = "available"
	if deleted != 0 {
		kind = DirectoryArtifactTrash
		ref.Availability = "deleted"
	}
	path := filepath.Join(s.dirs.Path(kind), blob)
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		if deleted == 0 {
			ref.Availability = "missing"
		}
		return ref, path, nil
	}
	if !isWithin(s.dirs.Path(kind), resolved) {
		ref.Availability = "missing"
		return ref, path, nil
	}
	return ref, resolved, nil
}

func (s *ArtifactService) Get(id string, version int) (ArtifactRef, error) {
	ref, _, err := s.refPath(id, version)
	return ref, err
}

func (s *ArtifactService) List(search, kind string, deleted bool) ([]ArtifactRef, error) {
	rows, err := s.db.Query(`SELECT a.id,MAX(v.version) FROM artifacts a JOIN artifact_versions v ON a.id=v.artifact_id WHERE a.deleted=? AND (?='' OR instr(lower(a.name),lower(?))>0) GROUP BY a.id ORDER BY MAX(v.created_at) DESC,a.name`, deleted, search, search)
	if err != nil {
		return nil, err
	}
	type key struct {
		id      string
		version int
	}
	var keys []key
	for rows.Next() {
		var k key
		if err := rows.Scan(&k.id, &k.version); err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, k)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	refs := []ArtifactRef{}
	for _, k := range keys {
		ref, err := s.Get(k.id, k.version)
		if err != nil {
			return nil, err
		}
		if kind == "" || kind == ref.Kind {
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

func (s *ArtifactService) Versions(id string) ([]ArtifactRef, error) {
	rows, err := s.db.Query("SELECT version FROM artifact_versions WHERE artifact_id=? ORDER BY version DESC", id)
	if err != nil {
		return nil, err
	}
	var versions []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return nil, err
		}
		versions = append(versions, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	refs := []ArtifactRef{}
	for _, v := range versions {
		ref, err := s.Get(id, v)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

//wails:ignore
func (s *ArtifactService) LoadArtifact(ctx context.Context, info artifact.SessionInfo, name string, version *int) (*artifact.Artifact, error) {
	var id string
	err := s.db.QueryRowContext(ctx, "SELECT id FROM artifacts WHERE app_name=? AND user_id=? AND session_id=? AND name=? AND deleted=0", info.AppName, info.UserID, info.SessionID, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v := 0
	if version == nil {
		if err := s.db.QueryRowContext(ctx, "SELECT MAX(version) FROM artifact_versions WHERE artifact_id=?", id).Scan(&v); err != nil {
			return nil, err
		}
	} else {
		v = *version
	}
	ref, path, err := s.refPath(id, v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if ref.Availability != "available" {
		return nil, os.ErrNotExist
	}
	if ref.Size > artifactByteLimit {
		return nil, ErrArtifactTooLarge
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, artifactByteLimit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > artifactByteLimit {
		return nil, ErrArtifactTooLarge
	}
	return &artifact.Artifact{Data: data, MimeType: ref.MimeType, Name: ref.Name}, nil
}

//wails:ignore
func (s *ArtifactService) ListArtifactKeys(ctx context.Context, info artifact.SessionInfo) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT name FROM artifacts WHERE app_name=? AND user_id=? AND session_id=? AND deleted=0 ORDER BY name", info.AppName, info.UserID, info.SessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		keys = append(keys, name)
	}
	return keys, rows.Err()
}

//wails:ignore
func (s *ArtifactService) ListVersions(ctx context.Context, info artifact.SessionInfo, name string) ([]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT v.version FROM artifacts a JOIN artifact_versions v ON a.id=v.artifact_id WHERE a.app_name=? AND a.user_id=? AND a.session_id=? AND a.name=? AND a.deleted=0 ORDER BY v.version`, info.AppName, info.UserID, info.SessionID, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := []int{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

//wails:ignore
func (s *ArtifactService) DeleteArtifact(ctx context.Context, info artifact.SessionInfo, name string) error {
	var id string
	err := s.db.QueryRowContext(ctx, "SELECT id FROM artifacts WHERE app_name=? AND user_id=? AND session_id=? AND name=?", info.AppName, info.UserID, info.SessionID, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.Delete(id)
}

func (s *ArtifactService) Delete(id string) error  { return s.move(id, true) }
func (s *ArtifactService) Restore(id string) error { return s.move(id, false) }

func (s *ArtifactService) move(id string, trash bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var deleted bool
	if err := s.db.QueryRow("SELECT deleted FROM artifacts WHERE id=?", id).Scan(&deleted); err != nil {
		return err
	}
	if deleted == trash {
		return nil
	}
	refs, err := s.Versions(id)
	if err != nil {
		return err
	}
	from, to := DirectoryArtifactFiles, DirectoryArtifactTrash
	if !trash {
		from, to = to, from
	}
	type movedFile struct{ from, to string }
	moved := []movedFile{}
	success := false
	defer func() {
		if !success {
			for i := len(moved) - 1; i >= 0; i-- {
				os.Rename(moved[i].to, moved[i].from)
			}
		}
	}()
	for _, ref := range refs {
		_, path, err := s.refPath(id, ref.Version)
		if err != nil {
			return err
		}
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || !isWithin(s.dirs.Path(from), resolved) {
			return errors.New("产物路径越界")
		}
		dest := filepath.Join(s.dirs.Path(to), filepath.Base(path))
		if err := os.Rename(path, dest); err != nil {
			return err
		}
		moved = append(moved, movedFile{path, dest})
	}
	if _, err := s.db.Exec("UPDATE artifacts SET deleted=? WHERE id=?", trash, id); err != nil {
		return err
	}
	success = true
	if s.notify != nil {
		s.notify("artifacts.changed", "updated")
	}
	return nil
}

func (s *ArtifactService) Preview(id string, version int) (ArtifactPreview, error) {
	ref, path, err := s.refPath(id, version)
	result := ArtifactPreview{Artifact: ref}
	if err != nil || ref.Availability != "available" {
		return result, err
	}
	result.URL = fmt.Sprintf("/artifacts/%s/%d?token=%s", id, version, s.token)
	if ref.Kind == "text" || ref.Kind == "markdown" || ref.Kind == "code" || ref.Kind == "html" || ref.Kind == "csv" {
		f, err := os.Open(path)
		if err != nil {
			return result, err
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, artifactTextLimit+1))
		if err != nil {
			return result, err
		}
		result.Truncated = int64(len(data)) > artifactTextLimit
		if result.Truncated {
			data = data[:artifactTextLimit]
		}
		result.Text = strings.ToValidUTF8(string(data), "\uFFFD")
	}
	return result, nil
}

//wails:ignore
func (s *ArtifactService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("token")), []byte(s.token)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/artifacts/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	v, err := strconv.Atoi(parts[1])
	if err != nil || v < 0 {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	ref, path, err := s.refPath(parts[0], v)
	if err != nil || ref.Availability != "available" {
		s.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(path)
	s.mu.Unlock()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ref.MimeType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": filepath.Base(ref.Name)}))
	http.ServeContent(w, r, ref.Name, stat.ModTime(), f)
}

func (s *ArtifactService) Import() (*ArtifactRef, error) {
	if s.picker == nil {
		return nil, errors.New("文件选择器未初始化")
	}
	path, err := s.picker.PickArtifact()
	if err != nil || path == "" {
		return nil, err
	}
	ref, err := s.importFile(context.Background(), artifact.SessionInfo{AppName: aguiAppName, UserID: aguiUserID, SessionID: "library-" + artifactID()}, path)
	return &ref, err
}

func (s *ArtifactService) importFile(ctx context.Context, info artifact.SessionInfo, path string) (ArtifactRef, error) {
	f, err := os.Open(path)
	if err != nil {
		return ArtifactRef{}, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return ArtifactRef{}, err
	}
	if !stat.Mode().IsRegular() {
		return ArtifactRef{}, errors.New("仅支持普通文件")
	}
	if stat.Size() > artifactImportLimit {
		return ArtifactRef{}, ErrArtifactTooLarge
	}
	ref, _, err := s.save(ctx, info, filepath.Base(path), "", f, artifactImportLimit)
	return ref, err
}

func (s *ArtifactService) Export(id string, version int) error {
	ref, path, err := s.refPath(id, version)
	if err != nil {
		return err
	}
	if ref.Availability != "available" {
		return os.ErrNotExist
	}
	if s.picker == nil {
		return errors.New("文件选择器未初始化")
	}
	dest, err := s.picker.PickArtifactExport(filepath.Base(ref.Name))
	if err != nil || dest == "" {
		return err
	}
	// An exported copy must never overwrite the managed store, including via symlinks.
	if isWithin(s.dirs.Path(DirectoryArtifacts), canonicalPathForBoundary(dest)) {
		return errors.New("不能覆盖托管产物目录")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".miel-export-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = io.Copy(tmp, f)
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}

func canonicalPathForBoundary(path string) string {
	clean := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(clean)
	if err == nil {
		return resolved
	}
	parent := filepath.Dir(clean)
	base := filepath.Base(clean)
	if resolved, err := filepath.EvalSymlinks(parent); err == nil {
		return filepath.Join(resolved, base)
	}
	return clean
}

func (s *ArtifactService) Open(id string, version int) error {
	ref, path, err := s.refPath(id, version)
	if err != nil {
		return err
	}
	if ref.Availability != "available" {
		return os.ErrNotExist
	}
	// Use a correctly named disposable copy, not the internal .blob file.
	dir, err := os.MkdirTemp(s.dirs.Path(DirectoryArtifactCache), "open-")
	if err != nil {
		return err
	}
	dest := filepath.Join(dir, safeArtifactFilename(ref.Name))
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, f)
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return openArtifactFile(dest)
}

func safeArtifactFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune("<>:\"/\\|?*", r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		return "artifact.txt"
	}
	return name
}

//wails:ignore
func (s *ArtifactService) ImportLegacy() error {
	for _, kind := range []DirectoryKind{DirectoryOutputReports, DirectoryOutputDocuments, DirectoryOutputTables} {
		dir := s.dirs.Path(kind)
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			var count int
			if err := s.db.QueryRow("SELECT COUNT(*) FROM artifact_imports WHERE source_path=?", path).Scan(&count); err != nil {
				return err
			}
			if count > 0 {
				continue
			}
			ref, err := s.importFile(context.Background(), artifact.SessionInfo{AppName: aguiAppName, UserID: aguiUserID, SessionID: "legacy-" + string(kind)}, path)
			if errors.Is(err, ErrArtifactTooLarge) {
				continue
			}
			if err != nil {
				return err
			}
			if _, err := s.db.Exec("INSERT OR IGNORE INTO artifact_imports(source_path,artifact_id) VALUES(?,?)", path, ref.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
