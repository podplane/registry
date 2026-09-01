// Podplane <https://podplane.dev>
// Copyright The Podplane Authors
// SPDX-License-Identifier: Apache-2.0

package registry_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/podplane/registry/pkg/registry"
	"github.com/podplane/registry/pkg/storage"
)

// fakeStore records read-only storage calls against in-memory objects.
type fakeStore struct {
	objects   map[string][]byte
	sizes     int
	opens     int
	lastRange *storage.Range
}

// Size returns the size of one in-memory object.
func (s *fakeStore) Size(_ context.Context, key string) (int64, error) {
	s.sizes++
	data, ok := s.objects[key]
	if !ok {
		return 0, storage.ErrNotFound
	}
	return int64(len(data)), nil
}

// Open returns a full or ranged in-memory object body.
func (s *fakeStore) Open(_ context.Context, key string, requested *storage.Range) (io.ReadCloser, int64, error) {
	s.opens++
	s.lastRange = requested
	data, ok := s.objects[key]
	if !ok {
		return nil, 0, storage.ErrNotFound
	}
	if requested != nil {
		data = data[requested.Start : requested.End+1]
	}
	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
}

// digest returns the SHA-256 digest of data.
func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// request executes one request against handler.
func request(t *testing.T, handler http.Handler, method, path, byteRange string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if byteRange != "" {
		req.Header.Set("Range", byteRange)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

// newHandler constructs a registry handler for a test store.
func newHandler(t *testing.T, store storage.Reader) http.Handler {
	t.Helper()
	handler, err := registry.New(store)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

// TestNewAndIdleMakeNoCalls verifies construction has no storage side effects.
func TestNewAndIdleMakeNoCalls(t *testing.T) {
	if _, err := registry.New(nil); err == nil {
		t.Fatal("New(nil) succeeded")
	}
	store := &fakeStore{}
	if _, err := registry.New(store); err != nil {
		t.Fatal(err)
	}
	if store.sizes != 0 || store.opens != 0 {
		t.Fatalf("storage calls = %d size, %d open", store.sizes, store.opens)
	}
}

// TestRoutesAndManifests verifies tag, digest, nested index, and HEAD responses.
func TestRoutesAndManifests(t *testing.T) {
	manifest := []byte(`{"schemaVersion":2}`)
	d := digest(manifest)
	hexDigest := d[len("sha256:"):]
	childIndex := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[]}`)
	childDigest := digest(childIndex)
	index := []byte(fmt.Sprintf(`{"manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":%q,"annotations":{"org.opencontainers.image.ref.name":"latest"}},{"mediaType":"application/vnd.oci.image.index.v1+json","digest":%q}]}`, d, childDigest))
	store := &fakeStore{objects: map[string][]byte{"team/app/index.json": index, "team/app/blobs/sha256/" + hexDigest: manifest, "team/app/blobs/sha256/" + strings.TrimPrefix(childDigest, "sha256:"): childIndex}}
	handler := newHandler(t, store)
	for _, path := range []string{"/v2/team/app/manifests/latest", "/v2/team/app/manifests/" + d} {
		response := request(t, handler, http.MethodGet, path, "")
		if response.Code != http.StatusOK || response.Body.String() != string(manifest) {
			t.Fatalf("%s: status %d body %q", path, response.Code, response.Body.String())
		}
		if response.Header().Get("Docker-Content-Digest") != d {
			t.Fatalf("digest header = %q", response.Header().Get("Docker-Content-Digest"))
		}
	}
	if store.sizes != 0 || store.opens != 4 {
		t.Fatalf("manifest GET storage calls = %d size, %d open", store.sizes, store.opens)
	}
	response := request(t, handler, http.MethodHead, "/v2/team/app/manifests/"+d, "")
	if response.Code != http.StatusOK || response.Body.Len() != 0 {
		t.Fatalf("HEAD status %d body %q", response.Code, response.Body.String())
	}
	response = request(t, handler, http.MethodGet, "/v2/team/app/manifests/"+childDigest, "")
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/vnd.oci.image.index.v1+json" {
		t.Fatalf("index manifest status %d content type %q", response.Code, response.Header().Get("Content-Type"))
	}
	if response.Header().Get("Accept-Ranges") != "" {
		t.Fatalf("manifest advertises byte ranges: %q", response.Header().Get("Accept-Ranges"))
	}
}

// TestBlobRanges verifies full, HEAD, partial, open-ended, and suffix blob reads.
func TestBlobRanges(t *testing.T) {
	data := []byte("0123456789")
	d := digest(data)
	store := &fakeStore{objects: map[string][]byte{"app/blobs/sha256/" + d[len("sha256:"):]: data}}
	handler := newHandler(t, store)
	full := request(t, handler, http.MethodGet, "/v2/app/blobs/"+d, "")
	if full.Code != http.StatusOK || full.Body.String() != string(data) {
		t.Fatalf("full blob: status=%d body=%q", full.Code, full.Body.String())
	}
	if store.sizes != 0 || store.opens != 1 {
		t.Fatalf("full blob storage calls = %d size, %d open", store.sizes, store.opens)
	}
	head := request(t, handler, http.MethodHead, "/v2/app/blobs/"+d, "")
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") != "10" {
		t.Fatalf("HEAD blob: status=%d body=%q headers=%v", head.Code, head.Body.String(), head.Header())
	}
	tests := []struct{ value, body, contentRange string }{{"bytes=2-4", "234", "bytes 2-4/10"}, {"bytes=7-", "789", "bytes 7-9/10"}, {"bytes=-3", "789", "bytes 7-9/10"}}
	for _, test := range tests {
		response := request(t, handler, http.MethodGet, "/v2/app/blobs/"+d, test.value)
		if response.Code != http.StatusPartialContent || response.Body.String() != test.body || response.Header().Get("Content-Range") != test.contentRange {
			t.Fatalf("range %s: status=%d body=%q headers=%v", test.value, response.Code, response.Body.String(), response.Header())
		}
	}
	response := request(t, handler, http.MethodGet, "/v2/app/blobs/"+d, "bytes=99-")
	if response.Code != http.StatusRequestedRangeNotSatisfiable || response.Header().Get("Content-Range") != "bytes */10" {
		t.Fatalf("invalid range response = %d, %q", response.Code, response.Header().Get("Content-Range"))
	}
}

// TestManifestRejectsTrailingIndexData verifies that an index contains exactly one JSON value.
func TestManifestRejectsTrailingIndexData(t *testing.T) {
	store := &fakeStore{objects: map[string][]byte{"app/index.json": []byte(`{"manifests":[]} {}`)}}
	handler := newHandler(t, store)
	response := request(t, handler, http.MethodGet, "/v2/app/manifests/latest", "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}

// TestValidationAndMethodsAvoidStorage verifies rejected requests have no storage side effects.
func TestValidationAndMethodsAvoidStorage(t *testing.T) {
	store := &fakeStore{objects: map[string][]byte{}}
	handler := newHandler(t, store)
	tests := []struct {
		method string
		path   string
		code   string
	}{
		{http.MethodPut, "/v2/app/manifests/latest", "UNSUPPORTED"},
		{http.MethodGet, "/v2/app/manifests/sha256:abcd", "DIGEST_INVALID"},
		{http.MethodGet, "/v2/../app/blobs/sha256:abcd", "NAME_UNKNOWN"},
		{http.MethodGet, "/v2/UPPER/blobs/sha256:abcd", "NAME_UNKNOWN"},
	}
	for _, test := range tests {
		response := request(t, handler, test.method, test.path, "")
		if response.Code < 400 || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
			t.Fatalf("%s %s response = %d %s", test.method, test.path, response.Code, response.Body.String())
		}
	}
	if store.sizes != 0 || store.opens != 0 {
		t.Fatalf("storage calls = %d size, %d open", store.sizes, store.opens)
	}
}

// TestPingUsesNoStorage verifies registry pings never access storage.
func TestPingUsesNoStorage(t *testing.T) {
	store := &fakeStore{}
	handler := newHandler(t, store)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		if response := request(t, handler, method, "/v2/", ""); response.Code != http.StatusOK {
			t.Fatalf("status = %d", response.Code)
		}
	}
	if store.sizes+store.opens != 0 {
		t.Fatal("ping accessed storage")
	}
}
