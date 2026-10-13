package project

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// GenerationSnapshot exposes validated source reads under the writer's lease.
// It is valid only during WithPublishedSnapshot's callback.
type GenerationSnapshot struct {
	tree     *OwnedTree
	manifest generationManifest
}

func (s *GenerationSnapshot) ReadFile(name string) ([]byte, error) {
	if s == nil || s.tree == nil {
		return nil, fmt.Errorf("generation snapshot is closed")
	}
	name, err := canonicalGenerationSourcePath(name)
	if err != nil {
		return nil, err
	}
	entry, ok := s.manifest[name]
	if !ok || entry.directory {
		return nil, fs.ErrNotExist
	}
	content, err := s.tree.ReadFile(name)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(content)
	if int64(len(content)) != entry.size || hex.EncodeToString(sum[:]) != entry.sha256 {
		return nil, fmt.Errorf("generation source integrity error for %s: content changed", name)
	}
	return content, nil
}

// CopyDependencies writes only requested snapshots through the destination
// ledger, verifies their recorded digests, and never reopens source paths.
func (s *GenerationSnapshot) CopyDependencies(destination *OwnedTree, deps []LockDep) error {
	if s == nil || s.tree == nil {
		return fmt.Errorf("generation snapshot is closed")
	}
	selected := make(map[string]LockDep, len(deps))
	for _, dep := range deps {
		if err := validateLockDep(dep); err != nil {
			return err
		}
		selected[dep.InstallDir] = dep
	}
	skip := func(name string, _ fs.FileInfo) bool {
		if name == "deps" {
			return false
		}
		for prefix := range selected {
			if name == prefix || strings.HasPrefix(name, prefix+"/") {
				return false
			}
		}
		return true
	}
	if err := s.tree.CopyTo(destination, skip); err != nil {
		return err
	}
	for _, dep := range deps {
		root, err := destination.OpenRoot(dep.InstallDir)
		if err != nil {
			return err
		}
		digest, hashErr := CanonicalRootDigest(root)
		if err := errors.Join(hashErr, root.Close()); err != nil {
			return err
		}
		if digest != dep.Digest {
			return fmt.Errorf("retained dependency %s digest mismatch", dep.ModuleID)
		}
	}
	return nil
}

// WithPublishedSnapshot retains the previous generation once, captures its
// identity ledger, and validates it before and after reuse. It shares the
// owner's exclusive lease instead of acquiring a reader lease.
func (o *GenerationOwner) WithPublishedSnapshot(mod *ModFile, lock *LockFile, use func(*GenerationSnapshot) error) (retErr error) {
	if lock == nil || !IsLowerHexDigest(lock.Generation) {
		return fmt.Errorf("invalid generation lock")
	}
	if err := o.verify(true); err != nil {
		return err
	}
	root, err := openVerifiedSubroot(o.generations, lock.Generation)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, root.Close()) }()
	tree := NewOwnedTree(root)
	defer func() { retErr = errors.Join(retErr, tree.Close()) }()
	if err := tree.CaptureExisting(); err != nil {
		return err
	}
	var manifest generationManifest
	if err := validateGenerationRootWithManifest(root, mod, lock, &manifest); err != nil {
		return fmt.Errorf("%s is stale or corrupt: %w", LockFileName, err)
	}
	if err := errors.Join(tree.VerifyComplete(), revalidateSubroot(o.generations, lock.Generation, root), o.verify(true)); err != nil {
		return err
	}
	snapshot := &GenerationSnapshot{tree: tree, manifest: manifest}
	defer func() { snapshot.tree = nil }()
	if err := use(snapshot); err != nil {
		return err
	}
	return errors.Join(validateGenerationRoot(root, mod, lock), tree.VerifyComplete(), revalidateSubroot(o.generations, lock.Generation, root), o.verify(true))
}
