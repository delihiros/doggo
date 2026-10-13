package lsp

import (
	"path/filepath"
	"sort"

	toolindex "doggo/internal/tooling/index"
)

type Document struct {
	URI     string
	Text    string
	Version int
}

type DocumentStore struct {
	docs map[string]Document
}

func NewDocumentStore() *DocumentStore {
	return &DocumentStore{
		docs: make(map[string]Document),
	}
}

func (s *DocumentStore) Upsert(uri, text string, version int) {
	uri = toolindex.NormalizeURI(uri)
	s.docs[uri] = Document{
		URI:     uri,
		Text:    text,
		Version: version,
	}
}

func (s *DocumentStore) Delete(uri string) {
	uri = toolindex.NormalizeURI(uri)
	delete(s.docs, uri)
}

func (s *DocumentStore) Get(uri string) (Document, bool) {
	uri = toolindex.NormalizeURI(uri)
	doc, ok := s.docs[uri]
	return doc, ok
}

func (s *DocumentStore) URIs() []string {
	out := make([]string, 0, len(s.docs))
	for uri := range s.docs {
		out = append(out, uri)
	}
	sort.Strings(out)
	return out
}

func (s *DocumentStore) FilePathOverlay() map[string]string {
	out := make(map[string]string)
	for uri, doc := range s.docs {
		path, err := toolindex.FileURIToPath(uri)
		if err != nil {
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		out[filepath.Clean(abs)] = doc.Text
	}
	return out
}
