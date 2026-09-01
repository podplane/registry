// Podplane <https://podplane.dev>
// Copyright The Podplane Authors
// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/podplane/registry/pkg/storage"
)

const (
	apiVersion   = "registry/2.0"
	maxIndexSize = 16 << 20
)

var (
	digestPattern = regexp.MustCompile(`^([a-z0-9]+(?:[+._-][a-z0-9]+)*):([a-f0-9]{32,})$`)
	repoPattern   = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)*$`)
	tagPattern    = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)
)

// handler serves registry requests from read-only object storage.
type handler struct{ store storage.Reader }

// distributionError is the OCI Distribution error response envelope.
type distributionError struct {
	Errors []errorItem `json:"errors"`
}

// errorItem describes one OCI Distribution error.
type errorItem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// index contains manifest descriptors from an OCI image layout index.
type index struct {
	Manifests []descriptor `json:"manifests"`
}

// descriptor contains fields needed to resolve a manifest.
type descriptor struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Annotations map[string]string `json:"annotations"`
}

// New returns a complete /v2/ registry handler without accessing storage.
func New(store storage.Reader) (http.Handler, error) {
	if store == nil {
		return nil, errors.New("registry: store is required")
	}
	return &handler{store: store}, nil
}

// ServeHTTP dispatches supported read-only OCI Distribution requests.
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Docker-Distribution-API-Version", apiVersion)
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "UNSUPPORTED", "the operation is unsupported")
		return
	}
	if r.URL.Path == "/v2/" || r.URL.Path == "/v2" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/v2/") {
		writeError(w, http.StatusNotFound, "NAME_UNKNOWN", "repository name not known to registry")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/v2/")
	if repo, ref, ok := splitRoute(rest, "/manifests/"); ok {
		h.serveManifest(w, r, repo, ref)
		return
	}
	if repo, digest, ok := splitRoute(rest, "/blobs/"); ok {
		h.serveBlob(w, r, repo, digest)
		return
	}
	writeError(w, http.StatusNotFound, "NAME_UNKNOWN", "repository name not known to registry")
}

// splitRoute separates a repository name from a terminal route value.
func splitRoute(path, marker string) (string, string, bool) {
	i := strings.LastIndex(path, marker)
	if i <= 0 {
		return "", "", false
	}
	repo, value := path[:i], path[i+len(marker):]
	return repo, value, repoPattern.MatchString(repo) && value != "" && !strings.Contains(value, "/")
}

// parseDigest validates and separates an OCI digest.
func parseDigest(value string) (algorithm, encoded string, ok bool) {
	m := digestPattern.FindStringSubmatch(value)
	if m == nil || (m[1] == "sha256" && len(m[2]) != 64) || (m[1] == "sha512" && len(m[2]) != 128) {
		return "", "", false
	}
	if _, err := hex.DecodeString(m[2]); err != nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// serveManifest resolves and serves a manifest by tag or digest.
func (h *handler) serveManifest(w http.ResponseWriter, r *http.Request, repo, reference string) {
	_, _, isDigest := parseDigest(reference)
	if !isDigest && strings.Contains(reference, ":") {
		writeError(w, http.StatusBadRequest, "DIGEST_INVALID", "manifest digest is invalid")
		return
	}
	if !isDigest && !tagPattern.MatchString(reference) {
		writeError(w, http.StatusBadRequest, "TAG_INVALID", "manifest tag is invalid")
		return
	}
	digest, mediaType, err := h.resolveManifest(r.Context(), repo, reference, isDigest)
	if err != nil {
		storageError(w, err, "MANIFEST_UNKNOWN", "manifest unknown")
		return
	}
	algorithm, encoded, ok := parseDigest(digest)
	if !ok || mediaType == "" {
		writeError(w, http.StatusInternalServerError, "UNKNOWN", "unknown error")
		return
	}
	h.serveObject(w, r, repo+"/blobs/"+algorithm+"/"+encoded, mediaType, digest, false, "MANIFEST_UNKNOWN", "manifest unknown")
}

// resolveManifest resolves a tag or digest through the repository index.
func (h *handler) resolveManifest(ctx context.Context, repo, reference string, isDigest bool) (string, string, error) {
	key := repo + "/index.json"
	body, size, err := h.store.Open(ctx, key, nil)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = body.Close() }()
	if size <= 0 || size > maxIndexSize {
		return "", "", errors.New("invalid index size")
	}
	var idx index
	decoder := json.NewDecoder(io.LimitReader(body, size+1))
	if err := decoder.Decode(&idx); err != nil {
		return "", "", fmt.Errorf("decode index: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return "", "", errors.New("decode index: trailing data")
	}
	for _, manifest := range idx.Manifests {
		if (isDigest && manifest.Digest == reference) || (!isDigest && manifest.Annotations["org.opencontainers.image.ref.name"] == reference) {
			return manifest.Digest, manifest.MediaType, nil
		}
	}
	return "", "", storage.ErrNotFound
}

// serveBlob validates a digest and serves its blob.
func (h *handler) serveBlob(w http.ResponseWriter, r *http.Request, repo, digest string) {
	algorithm, encoded, ok := parseDigest(digest)
	if !ok {
		writeError(w, http.StatusBadRequest, "DIGEST_INVALID", "blob digest is invalid")
		return
	}
	h.serveObject(w, r, repo+"/blobs/"+algorithm+"/"+encoded, "application/octet-stream", digest, true, "BLOB_UNKNOWN", "blob unknown to registry")
}

// serveObject serves object metadata or content with optional range handling.
func (h *handler) serveObject(w http.ResponseWriter, r *http.Request, key, fallbackType, digest string, ranges bool, notFoundCode, notFoundMessage string) {
	w.Header().Set("Content-Type", fallbackType)
	w.Header().Set("Docker-Content-Digest", digest)
	var requested *storage.Range
	status := http.StatusOK
	var length int64
	rangeHeader := ""
	if ranges {
		w.Header().Set("Accept-Ranges", "bytes")
		rangeHeader = r.Header.Get("Range")
	}
	if r.Method == http.MethodHead || rangeHeader != "" {
		size, err := h.store.Size(r.Context(), key)
		if err != nil {
			storageError(w, err, notFoundCode, notFoundMessage)
			return
		}
		length = size
		if rangeHeader != "" {
			requested, err = parseRange(rangeHeader, size)
			if err != nil {
				w.Header().Set("Content-Range", "bytes */"+strconv.FormatInt(size, 10))
				writeError(w, http.StatusRequestedRangeNotSatisfiable, "RANGE_INVALID", "requested range is not satisfiable")
				return
			}
			status, length = http.StatusPartialContent, requested.End-requested.Start+1
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", requested.Start, requested.End, size))
		}
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		w.WriteHeader(status)
		return
	}
	body, opened, err := h.store.Open(r.Context(), key, requested)
	if err != nil {
		storageError(w, err, notFoundCode, notFoundMessage)
		return
	}
	defer func() { _ = body.Close() }()
	if opened < 0 || (requested != nil && opened != length) {
		writeError(w, http.StatusInternalServerError, "UNKNOWN", "unknown error")
		return
	}
	if requested == nil {
		length = opened
	}
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.WriteHeader(status)
	_, _ = io.CopyN(w, body, length)
}

// parseRange parses one HTTP byte range against an object size.
func parseRange(value string, size int64) (*storage.Range, error) {
	if !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") || size <= 0 {
		return nil, errors.New("invalid range")
	}
	parts := strings.Split(strings.TrimPrefix(value, "bytes="), "-")
	if len(parts) != 2 {
		return nil, errors.New("invalid range")
	}
	if parts[0] == "" {
		n, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || n <= 0 {
			return nil, errors.New("invalid range")
		}
		if n > size {
			n = size
		}
		return &storage.Range{Start: size - n, End: size - 1}, nil
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 || start >= size {
		return nil, errors.New("invalid range")
	}
	end := size - 1
	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || end < start {
			return nil, errors.New("invalid range")
		}
		if end >= size {
			end = size - 1
		}
	}
	return &storage.Range{Start: start, End: end}, nil
}

// storageError translates storage errors into OCI Distribution errors.
func storageError(w http.ResponseWriter, err error, code, message string) {
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, code, message)
		return
	}
	writeError(w, http.StatusInternalServerError, "UNKNOWN", "unknown error")
}

// writeError writes one OCI Distribution JSON error response.
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Del("Accept-Ranges")
	w.Header().Del("Content-Length")
	w.Header().Del("Docker-Content-Digest")
	if status != http.StatusRequestedRangeNotSatisfiable {
		w.Header().Del("Content-Range")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(distributionError{Errors: []errorItem{{Code: code, Message: message}}})
}
