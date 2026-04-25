package filemanager

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/qoder/device-mgmt/internal/protocol"
	"go.uber.org/zap"
)

const (
	defaultChunkSize = 256 * 1024  // 256KB
	maxEditFileSize  = 1024 * 1024 // 1MB
)

// Manager manages file operations on the agent
type Manager struct {
	uploads   map[string]*UploadSession
	downloads map[string]*DownloadSession
	mu        sync.Mutex
	logger    *zap.Logger
	sendFn    func(*protocol.Envelope) error
}

// UploadSession tracks an active upload
type UploadSession struct {
	ID        string
	Path      string
	TotalSize int64
	ChunkSize int
	TempFile  *os.File
	Received  map[int]bool
	Hash      hash.Hash
	CreatedAt time.Time
	mu        sync.Mutex // per-session lock for concurrent chunk writes
}

// DownloadSession tracks an active download
type DownloadSession struct {
	ID          string
	Path        string
	File        *os.File
	ChunkSize   int
	TotalSize   int64
	Offset      int64
	Hash        hash.Hash
	CreatedAt   time.Time
	CleanupPath string // temp file to delete after download (for ZIP archives)
}

// NewManager creates a new file manager
func NewManager(logger *zap.Logger, sendFn func(*protocol.Envelope) error) *Manager {
	return &Manager{
		uploads:   make(map[string]*UploadSession),
		downloads: make(map[string]*DownloadSession),
		logger:    logger,
		sendFn:    sendFn,
	}
}

// Browse lists directory contents
func (m *Manager) Browse(path string) (*protocol.FileBrowseResult, error) {
	// Security: prevent path traversal
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("path traversal not allowed")
	}

	// Handle root path: list available drives on Windows
	if path == "/" || path == "\\" || path == "" {
		drives := listWindowsDrives()
		if drives != nil {
			return &protocol.FileBrowseResult{Path: "/", Entries: drives}, nil
		}
	}

	// Normalize path for the current OS (handles /C: → C:\ on Windows)
	cleanPath := normalizePath(path)

	entries, err := os.ReadDir(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	var fileEntries []protocol.FileEntry
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		entryType := "file"
		if entry.IsDir() {
			entryType = "dir"
		}

		fe := protocol.FileEntry{
			Name:    entry.Name(),
			Type:    entryType,
			Size:    info.Size(),
			Mode:    info.Mode().String(),
			ModTime: info.ModTime().Format(time.RFC3339),
		}

		// Try to get UID/GID on Unix
		if stat, ok := info.Sys().(*syscallStat); ok {
			fe.UID = stat.Uid
			fe.GID = stat.Gid
		}

		fileEntries = append(fileEntries, fe)
	}

	// Sort: directories first, then alphabetically
	sort.Slice(fileEntries, func(i, j int) bool {
		if fileEntries[i].Type != fileEntries[j].Type {
			return fileEntries[i].Type == "dir"
		}
		return fileEntries[i].Name < fileEntries[j].Name
	})

	return &protocol.FileBrowseResult{
		Path:    toForwardSlash(cleanPath),
		Entries: fileEntries,
	}, nil
}

// ReadFile reads a file for editing
func (m *Manager) ReadFile(path string) (*protocol.FileReadResult, error) {
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("path traversal not allowed")
	}
	cleanPath := normalizePath(path)

	info, err := os.Stat(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}

	if info.IsDir() {
		return nil, fmt.Errorf("cannot read directory")
	}

	if info.Size() > maxEditFileSize {
		return nil, fmt.Errorf("file too large for editing (max %d bytes)", maxEditFileSize)
	}

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	language := detectLanguage(cleanPath)

	return &protocol.FileReadResult{
		Path:     toForwardSlash(cleanPath),
		Content:  string(data),
		Size:     info.Size(),
		Language: language,
	}, nil
}

// WriteFile writes content to a file
func (m *Manager) WriteFile(path, content string) error {
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal not allowed")
	}
	cleanPath := normalizePath(path)

	// Write to temp file first, then rename (atomic write)
	dir := filepath.Dir(cleanPath)
	tmpFile, err := os.CreateTemp(dir, ".qoder-edit-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	tmpFile.Close()

	if err := os.Rename(tmpPath, cleanPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename file: %w", err)
	}

	return nil
}

// StartUpload initiates a chunked upload session
func (m *Manager) StartUpload(transferID, path string, size int64) (*protocol.FileUploadAckPayload, error) {
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("path traversal not allowed")
	}
	cleanPath := normalizePath(path)

	dir := filepath.Dir(cleanPath)
	tmpFile, err := os.CreateTemp(dir, ".qoder-upload-*")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.uploads[transferID] = &UploadSession{
		ID:        transferID,
		Path:      cleanPath,
		TotalSize: size,
		ChunkSize: defaultChunkSize,
		TempFile:  tmpFile,
		Received:  make(map[int]bool),
		Hash:      sha256.New(),
		CreatedAt: time.Now(),
	}

	return &protocol.FileUploadAckPayload{
		TransferID: transferID,
		ChunkSize:  defaultChunkSize,
	}, nil
}

// ReceiveChunk receives a chunk of file data
func (m *Manager) ReceiveChunk(transferID string, index int, data string) error {
	m.mu.Lock()
	session, exists := m.uploads[transferID]
	m.mu.Unlock()

	if !exists {
		return fmt.Errorf("upload session %s not found", transferID)
	}

	// Per-session lock prevents concurrent map write and hash corruption
	session.mu.Lock()
	defer session.mu.Unlock()

	if session.Received[index] {
		return nil // Already received, skip
	}

	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return fmt.Errorf("decode chunk: %w", err)
	}

	// Validate chunk size and index
	if len(decoded) > session.ChunkSize {
		return fmt.Errorf("chunk %d size %d exceeds chunk size %d", index, len(decoded), session.ChunkSize)
	}
	expectedChunks := int((session.TotalSize + int64(session.ChunkSize) - 1) / int64(session.ChunkSize))
	if expectedChunks > 0 && index >= expectedChunks {
		return fmt.Errorf("chunk index %d exceeds expected chunk count %d", index, expectedChunks)
	}

	// Write at correct offset
	offset := int64(index) * int64(session.ChunkSize)
	if _, err := session.TempFile.WriteAt(decoded, offset); err != nil {
		return fmt.Errorf("write chunk: %w", err)
	}

	session.Received[index] = true
	// Hash is computed in CompleteUpload from the file to handle out-of-order chunks correctly

	return nil
}

// CompleteUpload finalizes an upload
func (m *Manager) CompleteUpload(transferID string) (*protocol.FileUploadDoneResult, error) {
	m.mu.Lock()
	session, exists := m.uploads[transferID]
	if !exists {
		m.mu.Unlock()
		return nil, fmt.Errorf("upload session %s not found", transferID)
	}
	delete(m.uploads, transferID)
	m.mu.Unlock()

	session.mu.Lock()
	session.TempFile.Close()

	// Compute hash from the completed file (correct regardless of chunk arrival order)
	fileHash := sha256.New()
	tmpFile, err := os.Open(session.TempFile.Name())
	if err == nil {
		if _, err := io.Copy(fileHash, tmpFile); err != nil {
			m.logger.Warn("failed to hash uploaded file", zap.Error(err))
		}
		tmpFile.Close()
	}
	hashHex := hex.EncodeToString(fileHash.Sum(nil))

	// Rename temp file to final path
	if err := os.Rename(session.TempFile.Name(), session.Path); err != nil {
		os.Remove(session.TempFile.Name())
		session.mu.Unlock()
		return nil, fmt.Errorf("rename file: %w", err)
	}
	session.mu.Unlock()

	return &protocol.FileUploadDoneResult{
		TransferID: transferID,
		SHA256:     hashHex,
	}, nil
}

// StartDownload initiates a chunked download session
func (m *Manager) StartDownload(transferID, path string) (*protocol.FileDownloadStartResult, error) {
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("path traversal not allowed")
	}
	cleanPath := normalizePath(path)

	file, err := os.Open(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}

	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("stat file: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.downloads[transferID] = &DownloadSession{
		ID:        transferID,
		Path:      cleanPath,
		File:      file,
		ChunkSize: defaultChunkSize,
		TotalSize: info.Size(),
		Hash:      sha256.New(),
		CreatedAt: time.Now(),
	}

	// Start sending chunks in background
	go m.sendDownloadChunks(transferID)

	return &protocol.FileDownloadStartResult{
		TransferID: transferID,
		TotalSize:  info.Size(),
		ChunkSize:  defaultChunkSize,
	}, nil
}

func (m *Manager) sendDownloadChunks(transferID string) {
	m.mu.Lock()
	session, exists := m.downloads[transferID]
	m.mu.Unlock()

	if !exists {
		return
	}

	buf := make([]byte, session.ChunkSize)
	index := 0

	for {
		n, err := session.File.Read(buf)
		if n > 0 {
			session.Hash.Write(buf[:n])
			encoded := base64.StdEncoding.EncodeToString(buf[:n])

			env, _ := protocol.NewEnvelope(
				protocol.GenerateID(),
				protocol.ChannelFile,
				protocol.TypeStream,
				protocol.ActionFileDownloadChunk,
				protocol.FileDownloadChunkPayload{
					TransferID: transferID,
					Index:      index,
					Data:       encoded,
				},
			)

			m.sendFn(env)
			index++
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			m.logger.Error("download read error", zap.Error(err))
			break
		}
	}

	// Send done
	env, _ := protocol.NewEnvelope(
		protocol.GenerateID(),
		protocol.ChannelFile,
		protocol.TypeEvent,
		protocol.ActionFileDownloadDone,
		map[string]string{
			"transfer_id": transferID,
			"sha256":      hex.EncodeToString(session.Hash.Sum(nil)),
		},
	)
	m.sendFn(env)

	// Cleanup
	m.mu.Lock()
	delete(m.downloads, transferID)
	m.mu.Unlock()
	session.File.Close()
	if session.CleanupPath != "" {
		os.Remove(session.CleanupPath)
	}
}

// Delete deletes a file or directory
func (m *Manager) Delete(path string) error {
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal not allowed")
	}
	cleanPath := normalizePath(path)
	return os.RemoveAll(cleanPath)
}

// Mkdir creates a directory
func (m *Manager) Mkdir(path string) error {
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal not allowed")
	}
	cleanPath := normalizePath(path)
	return os.MkdirAll(cleanPath, 0755)
}

// Move moves/renames a file or directory
func (m *Manager) Move(src, dst string) error {
	if strings.Contains(src, "..") || strings.Contains(dst, "..") {
		return fmt.Errorf("path traversal not allowed")
	}
	cleanSrc := normalizePath(src)
	cleanDst := normalizePath(dst)
	return os.Rename(cleanSrc, cleanDst)
}

// Chmod changes file permissions
func (m *Manager) Chmod(path, mode string) error {
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal not allowed")
	}
	cleanPath := normalizePath(path)

	// Parse octal mode string (e.g. "0755")
	var modeUint uint64
	_, err := fmt.Sscanf(mode, "%o", &modeUint)
	if err != nil {
		return fmt.Errorf("invalid mode %q: %w", mode, err)
	}

	return os.Chmod(cleanPath, os.FileMode(modeUint))
}

// CreateFile creates a new empty file
func (m *Manager) CreateFile(path string) error {
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal not allowed")
	}
	cleanPath := normalizePath(path)
	f, err := os.Create(cleanPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	f.Close()
	return nil
}

// StartDownloadDir creates a ZIP archive of a directory and starts a download session
func (m *Manager) StartDownloadDir(transferID, path string) (*protocol.FileDownloadDirStartResult, error) {
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("path traversal not allowed")
	}
	cleanPath := normalizePath(path)

	info, err := os.Stat(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("stat path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory")
	}

	// Create temp ZIP file
	tmpFile, err := os.CreateTemp("", "qoder-dir-*.zip")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Write ZIP archive
	zipWriter := zip.NewWriter(tmpFile)
	baseDir := filepath.Base(cleanPath)

	err = filepath.WalkDir(cleanPath, func(filePath string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}

		relPath, err := filepath.Rel(cleanPath, filePath)
		if err != nil {
			return nil
		}
		relPath = filepath.ToSlash(relPath)
		if relPath == "." {
			return nil
		}
		zipEntry := baseDir + "/" + relPath

		if d.IsDir() {
			_, err = zipWriter.Create(zipEntry + "/")
			return err
		}

		w, err := zipWriter.Create(zipEntry)
		if err != nil {
			return err
		}

		f, err := os.Open(filePath)
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(w, f)
		return err
	})

	zipWriter.Close()
	tmpFile.Close()

	if err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("create zip: %w", err)
	}

	// Open ZIP for reading
	zipFile, err := os.Open(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("open zip: %w", err)
	}

	zipInfo, err := zipFile.Stat()
	if err != nil {
		zipFile.Close()
		os.Remove(tmpPath)
		return nil, fmt.Errorf("stat zip: %w", err)
	}

	dirName := filepath.Base(cleanPath)
	zipName := dirName + ".zip"

	m.mu.Lock()
	defer m.mu.Unlock()

	m.downloads[transferID] = &DownloadSession{
		ID:          transferID,
		Path:        tmpPath,
		File:        zipFile,
		ChunkSize:   defaultChunkSize,
		TotalSize:   zipInfo.Size(),
		Hash:        sha256.New(),
		CreatedAt:   time.Now(),
		CleanupPath: tmpPath,
	}

	go m.sendDownloadChunks(transferID)

	return &protocol.FileDownloadDirStartResult{
		TransferID: transferID,
		TotalSize:  zipInfo.Size(),
		ChunkSize:  defaultChunkSize,
		Name:       zipName,
	}, nil
}

// Search searches for files matching a pattern in a directory tree
func (m *Manager) Search(rootPath, pattern string, maxDepth int) (*protocol.FileBrowseResult, error) {
	if strings.Contains(rootPath, "..") {
		return nil, fmt.Errorf("path traversal not allowed")
	}
	cleanPath := normalizePath(rootPath)

	if maxDepth <= 0 {
		maxDepth = 5
	}

	var results []protocol.FileEntry
	patternLower := strings.ToLower(pattern)

	err := filepath.WalkDir(cleanPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}

		// Calculate depth
		rel, err := filepath.Rel(cleanPath, path)
		if err != nil {
			return nil
		}
		depth := len(strings.Split(rel, string(filepath.Separator)))
		if depth > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Match pattern against file name
		if strings.Contains(strings.ToLower(d.Name()), patternLower) {
			info, infoErr := d.Info()
			if infoErr != nil {
				return nil
			}
			entryType := "file"
			if d.IsDir() {
				entryType = "dir"
			}
			results = append(results, protocol.FileEntry{
				Name:    d.Name(),
				Path:    path,
				Type:    entryType,
				Size:    info.Size(),
				Mode:    info.Mode().String(),
				ModTime: info.ModTime().Format(time.RFC3339),
			})
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Type != results[j].Type {
			return results[i].Type == "dir"
		}
		return results[i].Name < results[j].Name
	})

	return &protocol.FileBrowseResult{
		Path:    toForwardSlash(cleanPath),
		Entries: results,
	}, nil
}

// HandleRequest handles file protocol requests
func (m *Manager) HandleRequest(ctx context.Context, env *protocol.Envelope) (*protocol.Envelope, error) {
	switch env.Action {
	case protocol.ActionFileBrowse:
		var payload protocol.FileBrowsePayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		result, err := m.Browse(payload.Path)
		if err != nil {
			return nil, err
		}
		return protocol.NewResponse(env, result)

	case protocol.ActionFileRead:
		var payload protocol.FileReadPayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		result, err := m.ReadFile(payload.Path)
		if err != nil {
			return nil, err
		}
		return protocol.NewResponse(env, result)

	case protocol.ActionFileWrite:
		var payload protocol.FileWritePayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		if err := m.WriteFile(payload.Path, payload.Content); err != nil {
			return nil, err
		}
		return protocol.NewResponse(env, map[string]string{})

	case protocol.ActionFileUploadStart:
		var payload protocol.FileUploadStartPayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		result, err := m.StartUpload(payload.TransferID, payload.Path, payload.Size)
		if err != nil {
			return nil, err
		}
		return protocol.NewResponse(env, result)

	case protocol.ActionFileUploadChunk:
		var payload protocol.FileUploadChunkPayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		if err := m.ReceiveChunk(payload.TransferID, payload.Index, payload.Data); err != nil {
			return nil, err
		}
		return protocol.NewResponse(env, map[string]interface{}{
			"transfer_id": payload.TransferID,
			"index":       payload.Index,
		})

	case protocol.ActionFileUploadDone:
		var payload protocol.FileUploadDonePayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		result, err := m.CompleteUpload(payload.TransferID)
		if err != nil {
			return nil, err
		}
		return protocol.NewResponse(env, result)

	case protocol.ActionFileDownloadStart:
		var payload protocol.FileDownloadStartPayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		result, err := m.StartDownload(payload.TransferID, payload.Path)
		if err != nil {
			return nil, err
		}
		return protocol.NewResponse(env, result)

	case protocol.ActionFileDelete:
		var payload protocol.FileDeletePayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		if err := m.Delete(payload.Path); err != nil {
			return nil, err
		}
		return protocol.NewResponse(env, map[string]string{})

	case protocol.ActionFileMkdir:
		var payload protocol.FileMkdirPayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		if err := m.Mkdir(payload.Path); err != nil {
			return nil, err
		}
		return protocol.NewResponse(env, map[string]string{})

	case protocol.ActionFileMove:
		var payload protocol.FileMovePayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		if err := m.Move(payload.Src, payload.Dst); err != nil {
			return nil, err
		}
		return protocol.NewResponse(env, map[string]string{})

	case protocol.ActionFileChmod:
		var payload protocol.FileChmodPayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		if err := m.Chmod(payload.Path, payload.Mode); err != nil {
			return nil, err
		}
		return protocol.NewResponse(env, map[string]string{})

	case protocol.ActionFileSearch:
		var payload protocol.FileSearchPayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		result, err := m.Search(payload.Path, payload.Pattern, payload.MaxDepth)
		if err != nil {
			return nil, err
		}
		return protocol.NewResponse(env, result)

	case protocol.ActionFileDownloadDir:
		var payload protocol.FileDownloadDirPayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		result, err := m.StartDownloadDir(payload.TransferID, payload.Path)
		if err != nil {
			return nil, err
		}
		return protocol.NewResponse(env, result)

	case protocol.ActionFileCreate:
		var payload protocol.FileCreatePayload
		if err := env.DecodePayload(&payload); err != nil {
			return nil, err
		}
		if err := m.CreateFile(payload.Path); err != nil {
			return nil, err
		}
		return protocol.NewResponse(env, map[string]string{})

	default:
		return nil, fmt.Errorf("unknown file action: %s", env.Action)
	}
}

// Name returns the plugin name
func (m *Manager) Name() string { return "file" }

// Init initializes the plugin
func (m *Manager) Init(a interface{}) error { return nil }

// Shutdown shuts down the plugin
func (m *Manager) Shutdown() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, session := range m.uploads {
		session.TempFile.Close()
		os.Remove(session.TempFile.Name())
		delete(m.uploads, id)
	}

	for id, session := range m.downloads {
		session.File.Close()
		if session.CleanupPath != "" {
			os.Remove(session.CleanupPath)
		}
		delete(m.downloads, id)
	}

	return nil
}

// detectLanguage detects the programming language from file extension
func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	langMap := map[string]string{
		".go":   "go",
		".py":   "python",
		".js":   "javascript",
		".ts":   "typescript",
		".jsx":  "javascript",
		".tsx":  "typescript",
		".rs":   "rust",
		".java": "java",
		".c":    "c",
		".cpp":  "cpp",
		".h":    "c",
		".yaml": "yaml",
		".yml":  "yaml",
		".json": "json",
		".xml":  "xml",
		".html": "html",
		".css":  "css",
		".sh":   "shell",
		".bash": "shell",
		".sql":  "sql",
		".md":   "markdown",
		".toml": "toml",
		".ini":  "ini",
	}
	if lang, ok := langMap[ext]; ok {
		return lang
	}
	return "plaintext"
}

// normalizePath cleans a path and handles Windows drive letter edge cases
func normalizePath(path string) string {
	if strings.Contains(path, "..") {
		return path // caller should reject
	}

	// On Windows, fix paths that start with /X: or \X: (front-end sends /C: which Clean treats as UNC)
	if runtime.GOOS == "windows" {
		// Match /C: or \C: at the start and convert to C:\
		if len(path) >= 3 && (path[0] == '/' || path[0] == '\\') && path[2] == ':' &&
			((path[1] >= 'A' && path[1] <= 'Z') || (path[1] >= 'a' && path[1] <= 'z')) {
			// Save drive letter before modifying path
			driveLetter := string(path[1])
			rest := ""
			if len(path) > 3 {
				rest = path[3:]
				// Ensure rest starts with separator
				if rest[0] != '/' && rest[0] != '\\' {
					rest = string(filepath.Separator) + rest
				}
				path = driveLetter + ":" + rest
			} else {
				// /C: or \C: with no sub-path → C:\
				path = driveLetter + ":\\"
			}
		}
	}

	cleanPath := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		// filepath.Clean("C:") gives "C:." which is a relative path; we need "C:\"
		if len(cleanPath) == 2 && cleanPath[1] == ':' {
			cleanPath = cleanPath + string(filepath.Separator)
		} else if len(cleanPath) == 3 && cleanPath[1] == ':' && cleanPath[2] == '.' {
			cleanPath = cleanPath[:2] + string(filepath.Separator)
		}
	}
	return cleanPath
}

// toForwardSlash converts OS-specific path separators to forward slashes for API responses
func toForwardSlash(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}

// syscallStat is a placeholder for platform-specific stat info
type syscallStat struct {
	Uid int
	Gid int
}

// listWindowsDrives returns available drive letters on Windows, or nil on non-Windows
func listWindowsDrives() []protocol.FileEntry {
	if runtime.GOOS != "windows" {
		return nil
	}
	var entries []protocol.FileEntry
	for _, drive := range "ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
		path := string(drive) + ":\\"
		if _, err := os.Stat(path); err == nil {
			entries = append(entries, protocol.FileEntry{
				Name:    string(drive) + ":",
				Type:    "dir",
				Size:    0,
				Mode:    "drwxrwxrwx",
				ModTime: "",
			})
		}
	}
	return entries
}
