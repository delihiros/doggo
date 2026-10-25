package vm

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

var builtinsIOPath = []BuiltinDefinition{
	{
		Name:       "io-file*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgMin("io-file*", args, 1); err != nil {
				return err
			}

			parts := make([]string, len(args))
			for i, arg := range args {
				part, ok := arg.(atom.String)
				if !ok {
					return newArgumentError("io-file*: argument %d must be a string, got %s", i+1, arg.Type())
				}
				parts[i] = string(part)
			}

			return atom.NewString(ioHostPath(filepath.Join(parts...)))
		}},
	},
	{
		Name:       "io-basename*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("io-basename*", args, 1); err != nil {
				return err
			}

			path, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("io-basename*: argument must be a string, got %s", args[0].Type())
			}

			return atom.NewString(filepath.Base(ioHostPath(string(path))))
		}},
	},
	{
		Name:       "io-dirname*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("io-dirname*", args, 1); err != nil {
				return err
			}

			path, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("io-dirname*: argument must be a string, got %s", args[0].Type())
			}

			return atom.NewString(ioHostPath(filepath.Dir(string(path))))
		}},
	},
	{
		Name:       "io-extension*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("io-extension*", args, 1); err != nil {
				return err
			}

			path, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("io-extension*: argument must be a string, got %s", args[0].Type())
			}

			ext := filepath.Ext(ioHostPath(string(path)))
			if ext == "" || ext == "." {
				return atom.NilValue
			}
			return atom.NewString(ext[1:])
		}},
	},
	{
		Name:       "io-normalize*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("io-normalize*", args, 1); err != nil {
				return err
			}

			path, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("io-normalize*: argument must be a string, got %s", args[0].Type())
			}

			return atom.NewString(ioHostPath(string(path)))
		}},
	},
	{
		Name:       "io-absolute?*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("io-absolute?*", args, 1); err != nil {
				return err
			}

			path, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("io-absolute?*: argument must be a string, got %s", args[0].Type())
			}

			return atom.NewBoolean(filepath.IsAbs(string(path)))
		}},
	},
	{
		Name:       "io-absolute*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("io-absolute*", args, 1); err != nil {
				return err
			}

			path, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("io-absolute*: argument must be a string, got %s", args[0].Type())
			}

			abs, err := filepath.Abs(string(path))
			if err != nil {
				return RuntimeErrf("io-absolute*", "%v", err)
			}
			return atom.NewString(ioHostPath(abs))
		}},
	},
	{
		Name:       "io-relative-to*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("io-relative-to*", args, 2); err != nil {
				return err
			}

			base, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("io-relative-to*: base must be a string, got %s", args[0].Type())
			}
			path, ok := args[1].(atom.String)
			if !ok {
				return newArgumentError("io-relative-to*: path must be a string, got %s", args[1].Type())
			}

			baseIsAbs := filepath.IsAbs(string(base))
			pathIsAbs := filepath.IsAbs(string(path))
			if baseIsAbs != pathIsAbs {
				return newArgumentError("io-relative-to*: base and path must both be absolute or both be relative")
			}

			rel, err := filepath.Rel(string(base), string(path))
			if err != nil {
				return newArgumentError("io-relative-to*: %v", err)
			}
			return atom.NewString(ioHostPath(rel))
		}},
	},
	{
		Name:       "io-exists?*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("io-exists?*", args, 1); err != nil {
				return err
			}

			path, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("io-exists?*: argument must be a string, got %s", args[0].Type())
			}

			_, err := os.Stat(string(path))
			if err == nil {
				return atom.NewBoolean(true)
			}
			if os.IsNotExist(err) {
				return atom.NewBoolean(false)
			}
			return RuntimeErrf("io-exists?*", "%v", err)
		}},
	},
	{
		Name:       "io-file?*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("io-file?*", args, 1); err != nil {
				return err
			}

			path, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("io-file?*: argument must be a string, got %s", args[0].Type())
			}

			info, err := os.Stat(string(path))
			if err == nil {
				return atom.NewBoolean(!info.IsDir())
			}
			if os.IsNotExist(err) {
				return atom.NewBoolean(false)
			}
			return RuntimeErrf("io-file?*", "%v", err)
		}},
	},
	{
		Name:       "io-directory?*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("io-directory?*", args, 1); err != nil {
				return err
			}

			path, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("io-directory?*: argument must be a string, got %s", args[0].Type())
			}

			info, err := os.Stat(string(path))
			if err == nil {
				return atom.NewBoolean(info.IsDir())
			}
			if os.IsNotExist(err) {
				return atom.NewBoolean(false)
			}
			return RuntimeErrf("io-directory?*", "%v", err)
		}},
	},
	{
		Name:       "io-list-entries*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("io-list-entries*", args, 1); err != nil {
				return err
			}

			path, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("io-list-entries*: argument must be a string, got %s", args[0].Type())
			}

			entries, err := os.ReadDir(string(path))
			if err != nil {
				return RuntimeErrf("io-list-entries*", "%v", err)
			}

			items := make([]types.Value, 0, len(entries))
			for _, entry := range entries {
				items = append(items, atom.NewString(ioHostPath(filepath.Join(string(path), entry.Name()))))
			}
			return coll.NewVector(items...)
		}},
	},
	{
		Name:       "io-walk*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("io-walk*", args, 2); err != nil {
				return err
			}

			root, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("io-walk*: path must be a string, got %s", args[0].Type())
			}

			options, errValue := ioWalkOptionsFromValue(args[1])
			if errValue != nil {
				return errValue
			}

			rootPath := ioHostPath(string(root))
			rootInfo, err := os.Lstat(rootPath)
			if err != nil {
				return RuntimeErrf("io-walk*", "%v", err)
			}
			return newIOWalkSeq(rootPath, rootInfo, options)
		}},
	},
	{
		Name:       "io-glob*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("io-glob*", args, 1); err != nil {
				return err
			}

			pattern, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("io-glob*: pattern must be a string, got %s", args[0].Type())
			}

			matches, err := filepath.Glob(string(pattern))
			if err != nil {
				return newArgumentError("io-glob*: %v", err)
			}

			items := make([]types.Value, 0, len(matches))
			for _, match := range matches {
				items = append(items, atom.NewString(ioHostPath(match)))
			}
			return coll.NewVector(items...)
		}},
	},
	{
		Name:       "io-create-temp-file*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("io-create-temp-file*", args, 3); err != nil {
				return err
			}

			prefix, errValue := ioOptionalStringArg("io-create-temp-file*", "prefix", args[0])
			if errValue != nil {
				return errValue
			}
			suffix, errValue := ioOptionalStringArg("io-create-temp-file*", "suffix", args[1])
			if errValue != nil {
				return errValue
			}
			dir, errValue := ioOptionalStringArg("io-create-temp-file*", "dir", args[2])
			if errValue != nil {
				return errValue
			}
			if prefix == "" {
				prefix = "doggo-"
			}

			file, err := os.CreateTemp(dir, prefix+"*"+suffix)
			if err != nil {
				return RuntimeErrf("io-create-temp-file*", "%v", err)
			}
			if err := file.Close(); err != nil {
				return RuntimeErrf("io-create-temp-file*", "%v", err)
			}
			return atom.NewString(ioHostPath(file.Name()))
		}},
	},
	{
		Name:       "io-create-temp-dir*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("io-create-temp-dir*", args, 2); err != nil {
				return err
			}

			prefix, errValue := ioOptionalStringArg("io-create-temp-dir*", "prefix", args[0])
			if errValue != nil {
				return errValue
			}
			dir, errValue := ioOptionalStringArg("io-create-temp-dir*", "dir", args[1])
			if errValue != nil {
				return errValue
			}
			if prefix == "" {
				prefix = "doggo-"
			}

			value, err := os.MkdirTemp(dir, prefix+"*")
			if err != nil {
				return RuntimeErrf("io-create-temp-dir*", "%v", err)
			}
			return atom.NewString(ioHostPath(value))
		}},
	},
	{
		Name:       "io-normalize-relative-path*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("io-normalize-relative-path*", args, 1); err != nil {
				return err
			}

			path, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("io-normalize-relative-path*: argument must be a string, got %s", args[0].Type())
			}

			cleaned := filepath.Clean(string(path))
			if filepath.IsAbs(cleaned) {
				return newArgumentError("io-normalize-relative-path*: path must be relative, got %s", cleaned)
			}
			return atom.NewString(cleaned)
		}},
	},
	{
		Name:       "io-as-url*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("io-as-url*", args, 1); err != nil {
				return err
			}

			path, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("io-as-url*: argument must be a string, got %s", args[0].Type())
			}

			raw := string(path)
			// A Windows drive prefix is a filesystem volume, even though URL
			// parsing may mistake its letter and colon for a scheme.
			if filepath.VolumeName(raw) == "" {
				if parsed, err := url.Parse(raw); err == nil && parsed.Scheme != "" {
					return atom.NewString(parsed.String())
				}
			}

			abs, err := filepath.Abs(raw)
			if err != nil {
				return newArgumentError("io-as-url*: %v", err)
			}
			u := &url.URL{
				Scheme: "file",
				Path:   filepath.ToSlash(abs),
			}
			volume := filepath.ToSlash(filepath.VolumeName(abs))
			if strings.HasPrefix(volume, "//") {
				// UNC volumes encode the server as the URL host and retain the
				// share name as the first path component.
				server, share, _ := strings.Cut(strings.TrimPrefix(volume, "//"), "/")
				u.Host = server
				u.Path = "/" + share + strings.TrimPrefix(u.Path, volume)
			} else if volume != "" {
				u.Path = "/" + u.Path
			}
			return atom.NewString(u.String())
		}},
	},
	{
		Name:       "io-delete-entry*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgRange("io-delete-entry*", args, 1, 2); err != nil {
				return err
			}

			path, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("io-delete-entry*: first argument must be a string, got %s", args[0].Type())
			}

			silently := false
			if len(args) == 2 {
				silently = isTruthy(args[1])
			}

			if err := os.Remove(string(path)); err != nil {
				if silently && os.IsNotExist(err) {
					return atom.NilValue
				}
				return RuntimeErrf("io-delete-entry*", "%v", err)
			}
			return atom.NilValue
		}},
	},
	{
		Name:       "io-make-parents*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("io-make-parents*", args, 1); err != nil {
				return err
			}

			path, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("io-make-parents*: argument must be a string, got %s", args[0].Type())
			}

			parent := filepath.Dir(string(path))
			if parent == "." || parent == "" || parent == string(os.PathSeparator) {
				return atom.NilValue
			}
			if err := os.MkdirAll(parent, 0o755); err != nil {
				return RuntimeErrf("io-make-parents*", "%v", err)
			}
			return atom.NilValue
		}},
	},
	{
		Name:       "io-mkdirs*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("io-mkdirs*", args, 1); err != nil {
				return err
			}

			path, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("io-mkdirs*: argument must be a string, got %s", args[0].Type())
			}

			normalized := ioHostPath(string(path))
			if err := os.MkdirAll(normalized, 0o755); err != nil {
				return RuntimeErrf("io-mkdirs*", "%v", err)
			}
			return atom.NewString(normalized)
		}},
	},
	{
		Name:       "io-delete-tree*",
		Visibility: BuiltinPrivate,
		Builtin: &Builtin{Fn: func(args ...types.Value) types.Value {
			if err := CheckArgCount("io-delete-tree*", args, 2); err != nil {
				return err
			}

			path, ok := args[0].(atom.String)
			if !ok {
				return newArgumentError("io-delete-tree*: path must be a string, got %s", args[0].Type())
			}
			ignoreMissing, ok := args[1].(atom.Boolean)
			if !ok {
				return newArgumentError("io-delete-tree*: ignore-missing must be boolean, got %s", args[1].Type())
			}

			if err := ioDeleteTree(string(path), bool(ignoreMissing)); err != nil {
				return RuntimeErrf("io-delete-tree*", "%v", err)
			}
			return atom.NilValue
		}},
	},
	{
		Name:       "io-copy-tree*",
		Visibility: BuiltinPrivate,
		Builtin:    ioCopyOrMoveTreeBuiltin("io-copy-tree*", false),
	},
	{
		Name:       "io-move*",
		Visibility: BuiltinPrivate,
		Builtin:    ioCopyOrMoveTreeBuiltin("io-move*", true),
	},
}

func ioCopyOrMoveTreeBuiltin(fnName string, move bool) *Builtin {
	return &Builtin{Fn: func(args ...types.Value) types.Value {
		if err := CheckArgCount(fnName, args, 4); err != nil {
			return err
		}

		src, ok := args[0].(atom.String)
		if !ok {
			return newArgumentError("%s: source must be a string, got %s", fnName, args[0].Type())
		}
		dst, ok := args[1].(atom.String)
		if !ok {
			return newArgumentError("%s: destination must be a string, got %s", fnName, args[1].Type())
		}
		overwrite, ok := args[2].(atom.Boolean)
		if !ok {
			return newArgumentError("%s: overwrite must be boolean, got %s", fnName, args[2].Type())
		}
		ignoreMissing, ok := args[3].(atom.Boolean)
		if !ok {
			return newArgumentError("%s: ignore-missing must be boolean, got %s", fnName, args[3].Type())
		}

		if err := ioCopyOrMoveTree(string(src), string(dst), bool(overwrite), bool(ignoreMissing), move); err != nil {
			return RuntimeErrf(fnName, "%v", err)
		}
		return atom.NilValue
	}}
}

func ioOptionalStringArg(fnName, label string, arg types.Value) (string, types.Value) {
	if arg == nil || arg == atom.NilValue {
		return "", nil
	}
	value, ok := arg.(atom.String)
	if !ok {
		return "", newArgumentError("%s: %s must be a string or nil, got %s", fnName, label, arg.Type())
	}
	return string(value), nil
}

type ioWalkOrder string

const (
	ioWalkOrderPre  ioWalkOrder = "pre"
	ioWalkOrderPost ioWalkOrder = "post"
)

type ioWalkType string

const (
	ioWalkTypeAll   ioWalkType = "all"
	ioWalkTypeFiles ioWalkType = "files"
	ioWalkTypeDirs  ioWalkType = "dirs"
)

type ioWalkOptions struct {
	order          ioWalkOrder
	entryType      ioWalkType
	includeRoot    bool
	maxDepth       int
	followSymlinks bool
}

func ioWalkOptionsFromValue(value types.Value) (ioWalkOptions, types.Value) {
	options := ioWalkOptions{
		order:          ioWalkOrderPre,
		entryType:      ioWalkTypeAll,
		includeRoot:    true,
		maxDepth:       -1,
		followSymlinks: false,
	}
	if value == nil || value == atom.NilValue {
		return options, nil
	}

	optionMap, ok := value.(types.Map)
	if !ok {
		return options, newArgumentError("io-walk*: options must be a map, got %s", value.Type())
	}

	if raw := optionMap.Get(atom.NewKeyword("order")); raw != nil && raw != atom.NilValue {
		kw, ok := raw.(atom.Keyword)
		if !ok {
			return options, newArgumentError("io-walk*: :order must be a keyword, got %s", raw.Type())
		}
		switch kw {
		case atom.NewKeyword("pre"):
			options.order = ioWalkOrderPre
		case atom.NewKeyword("post"):
			options.order = ioWalkOrderPost
		default:
			return options, newArgumentError("io-walk*: :order must be :pre or :post")
		}
	}

	if raw := optionMap.Get(atom.NewKeyword("type")); raw != nil && raw != atom.NilValue {
		kw, ok := raw.(atom.Keyword)
		if !ok {
			return options, newArgumentError("io-walk*: :type must be a keyword, got %s", raw.Type())
		}
		switch kw {
		case atom.NewKeyword("all"):
			options.entryType = ioWalkTypeAll
		case atom.NewKeyword("files"):
			options.entryType = ioWalkTypeFiles
		case atom.NewKeyword("dirs"):
			options.entryType = ioWalkTypeDirs
		default:
			return options, newArgumentError("io-walk*: :type must be :all, :files, or :dirs")
		}
	}

	if raw := optionMap.Get(atom.NewKeyword("include-root")); raw != nil && raw != atom.NilValue {
		booleanValue, ok := raw.(atom.Boolean)
		if !ok {
			return options, newArgumentError("io-walk*: :include-root must be boolean, got %s", raw.Type())
		}
		options.includeRoot = bool(booleanValue)
	}

	if raw := optionMap.Get(atom.NewKeyword("max-depth")); raw != nil && raw != atom.NilValue {
		intValue, ok := raw.(types.Int64Valuer)
		if !ok {
			return options, newArgumentError("io-walk*: :max-depth must be a non-negative integer or nil, got %s", raw.Type())
		}
		depth, exact := intValue.Int64Value()
		if !exact || depth < 0 {
			return options, newArgumentError("io-walk*: :max-depth must be a non-negative integer or nil")
		}
		options.maxDepth = int(depth)
	}

	if raw := optionMap.Get(atom.NewKeyword("follow-symlinks")); raw != nil && raw != atom.NilValue {
		booleanValue, ok := raw.(atom.Boolean)
		if !ok {
			return options, newArgumentError("io-walk*: :follow-symlinks must be boolean, got %s", raw.Type())
		}
		options.followSymlinks = bool(booleanValue)
	}

	return options, nil
}

type ioWalkFrame struct {
	path           string
	info           os.FileInfo
	depth          int
	children       []string
	childIndex     int
	childrenLoaded bool
	preYielded     bool
	postYielded    bool
}

type ioWalkSeq struct {
	options  ioWalkOptions
	frames   []ioWalkFrame
	once     sync.Once
	current  types.Value
	next     []ioWalkFrame
	err      error
	realized atomic.Bool
}

func newIOWalkSeq(root string, info os.FileInfo, options ioWalkOptions) *ioWalkSeq {
	return &ioWalkSeq{
		options: options,
		frames: []ioWalkFrame{{
			path:  root,
			info:  info,
			depth: 0,
		}},
	}
}

func (seq *ioWalkSeq) String() string {
	items, err := materializeSeq(seq)
	if err != nil {
		return "(#<LazySeqError: " + err.Error() + ">)"
	}
	return coll.NewList(items...).String()
}

func (seq *ioWalkSeq) Type() string {
	return "lazy-seq"
}

func (seq *ioWalkSeq) Hash() uint32 {
	return coll.SeqHash(seq)
}

func (seq *ioWalkSeq) Equals(other types.Value) bool {
	return coll.SequentialEquals(seq, other)
}

func (seq *ioWalkSeq) IsSelfEvaluating() bool {
	return false
}

func (seq *ioWalkSeq) Seq() types.Value {
	if seq == nil {
		return nil
	}
	seq.ensureRealized()
	if seq.err != nil {
		return &coll.LazySeqError{Err: seq.err}
	}
	if seq.current == nil {
		return nil
	}
	return seq
}

func (seq *ioWalkSeq) First() types.Value {
	if seq == nil {
		return nil
	}
	seq.ensureRealized()
	if seq.err != nil {
		return nil
	}
	return seq.current
}

func (seq *ioWalkSeq) Next() types.Seq {
	if seq == nil {
		return nil
	}
	seq.ensureRealized()
	if seq.err != nil || seq.current == nil || len(seq.next) == 0 {
		return nil
	}
	return &ioWalkSeq{
		options: seq.options,
		frames:  ioWalkCloneFrames(seq.next),
	}
}

func (seq *ioWalkSeq) More() types.Seq {
	next := seq.Next()
	if next == nil {
		return coll.NewList()
	}
	return next
}

func (seq *ioWalkSeq) Realized() bool {
	if seq == nil {
		return false
	}
	return seq.realized.Load()
}

func (seq *ioWalkSeq) ensureRealized() {
	seq.once.Do(func() {
		defer seq.realized.Store(true)
		seq.current, seq.next, seq.err = ioWalkAdvance(ioWalkCloneFrames(seq.frames), seq.options)
	})
}

func ioWalkAdvance(frames []ioWalkFrame, options ioWalkOptions) (types.Value, []ioWalkFrame, error) {
	for len(frames) > 0 {
		top := &frames[len(frames)-1]
		canDescend, err := ioWalkCanDescend(frames, options)
		if err != nil {
			return nil, nil, err
		}

		if !top.preYielded {
			top.preYielded = true
			shouldInclude, err := ioWalkShouldInclude(top.depth, top.path, top.info, options)
			if err != nil {
				return nil, nil, err
			}
			if options.order == ioWalkOrderPre && shouldInclude {
				return atom.NewString(top.path), frames, nil
			}
		}

		if canDescend {
			if !top.childrenLoaded {
				entries, err := os.ReadDir(top.path)
				if err != nil {
					return nil, nil, err
				}
				top.childrenLoaded = true
				top.children = make([]string, 0, len(entries))
				for _, entry := range entries {
					top.children = append(top.children, ioHostPath(filepath.Join(top.path, entry.Name())))
				}
			}
			if top.childIndex < len(top.children) {
				childPath := top.children[top.childIndex]
				top.childIndex++
				childInfo, err := os.Lstat(childPath)
				if err != nil {
					return nil, nil, err
				}
				frames = append(frames, ioWalkFrame{
					path:  childPath,
					info:  childInfo,
					depth: top.depth + 1,
				})
				continue
			}
		}

		if options.order == ioWalkOrderPost && !top.postYielded {
			top.postYielded = true
			shouldInclude, err := ioWalkShouldInclude(top.depth, top.path, top.info, options)
			if err != nil {
				return nil, nil, err
			}
			if shouldInclude {
				return atom.NewString(top.path), frames, nil
			}
		}

		frames = frames[:len(frames)-1]
	}

	return nil, nil, nil
}

func ioWalkCloneFrames(frames []ioWalkFrame) []ioWalkFrame {
	if len(frames) == 0 {
		return nil
	}
	cloned := make([]ioWalkFrame, len(frames))
	copy(cloned, frames)
	for i := range cloned {
		if len(frames[i].children) > 0 {
			cloned[i].children = append([]string(nil), frames[i].children...)
		}
	}
	return cloned
}

func ioWalkCanDescend(frames []ioWalkFrame, options ioWalkOptions) (bool, error) {
	if len(frames) == 0 {
		return false, nil
	}
	frame := &frames[len(frames)-1]
	if err := ioWalkProbeSymlinkTarget(frame.path, frame.info, options); err != nil {
		return false, err
	}
	if options.maxDepth >= 0 && frame.depth >= options.maxDepth {
		return false, nil
	}

	target, isDir, err := ioWalkDirectoryTarget(frame.path, frame.info, options)
	if err != nil || !isDir {
		return false, err
	}

	if options.followSymlinks {
		for i := 0; i < len(frames)-1; i++ {
			ancestorTarget, ancestorIsDir, err := ioWalkDirectoryTarget(frames[i].path, frames[i].info, options)
			if err != nil {
				return false, err
			}
			if ancestorIsDir && ancestorTarget == target {
				return false, nil
			}
		}
	}

	return true, nil
}

func ioWalkShouldInclude(depth int, path string, info os.FileInfo, options ioWalkOptions) (bool, error) {
	if depth == 0 && !options.includeRoot {
		return false, nil
	}
	if options.entryType == ioWalkTypeAll {
		return true, nil
	}

	isDir, err := ioWalkIsDir(path, info, options)
	if err != nil {
		return false, err
	}
	switch options.entryType {
	case ioWalkTypeFiles:
		return !isDir, nil
	case ioWalkTypeDirs:
		return isDir, nil
	default:
		return true, nil
	}
}

func ioWalkDirectoryTarget(path string, info os.FileInfo, options ioWalkOptions) (string, bool, error) {
	isDir, err := ioWalkIsDir(path, info, options)
	if err != nil || !isDir {
		return "", false, err
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", false, err
	}
	if options.followSymlinks {
		absPath, err = filepath.EvalSymlinks(absPath)
		if err != nil {
			if info.Mode()&os.ModeSymlink != 0 && ioWalkNonDescendableSymlinkTargetError(err) {
				return "", false, nil
			}
			return "", false, err
		}
	}
	return ioHostPath(absPath), true, nil
}

func ioWalkIsDir(path string, info os.FileInfo, options ioWalkOptions) (bool, error) {
	if info.IsDir() {
		return true, nil
	}
	if !options.followSymlinks || info.Mode()&os.ModeSymlink == 0 {
		return false, nil
	}

	targetInfo, err := os.Stat(path)
	if err != nil {
		if ioWalkNonDescendableSymlinkTargetError(err) {
			return false, nil
		}
		return false, err
	}
	return targetInfo.IsDir(), nil
}

func ioWalkProbeSymlinkTarget(path string, info os.FileInfo, options ioWalkOptions) error {
	if !options.followSymlinks || info.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	_, err := os.Stat(path)
	if err == nil || ioWalkNonDescendableSymlinkTargetError(err) {
		return nil
	}
	return err
}

func ioWalkNonDescendableSymlinkTargetError(err error) bool {
	return os.IsNotExist(err) || ioWalkSymlinkLoopError(err) || errors.Is(err, syscall.ENOTDIR)
}

func ioDeleteTree(path string, ignoreMissing bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		if ignoreMissing && os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return os.Remove(path)
	}
	return os.RemoveAll(path)
}

func ioCopyOrMoveTree(src, dst string, overwrite, ignoreMissing, move bool) error {
	src = ioHostPath(src)
	dst = ioHostPath(dst)

	srcInfo, err := os.Lstat(src)
	if err != nil {
		if ignoreMissing && os.IsNotExist(err) {
			return nil
		}
		if os.IsNotExist(err) {
			return fmt.Errorf("source does not exist: %s", src)
		}
		return err
	}

	dstInfo, dstExists, err := ioLstatOptional(dst)
	if err != nil {
		return err
	}

	if srcInfo.Mode()&os.ModeSymlink != 0 {
		return ioUnsupportedSymlinkError(src)
	}

	if srcInfo.IsDir() {
		recursiveDestination, err := ioPathIsSameOrDescendant(src, dst)
		if err != nil {
			return err
		}
		if recursiveDestination {
			return ioRecursiveDestinationError(src, dst)
		}
		return ioCopyOrMoveDir(src, srcInfo, dst, dstExists, dstInfo, overwrite, move)
	}
	return ioCopyOrMoveFile(src, srcInfo, dst, dstExists, dstInfo, overwrite, move)
}

func ioCopyOrMoveFile(src string, srcInfo os.FileInfo, dst string, dstExists bool, dstInfo os.FileInfo, overwrite, move bool) error {
	finalDst := dst
	finalInfo := dstInfo
	finalExists := dstExists

	if dstExists && dstInfo.IsDir() {
		finalDst = filepath.Join(dst, filepath.Base(src))
		info, exists, err := ioLstatOptional(finalDst)
		if err != nil {
			return err
		}
		finalInfo = info
		finalExists = exists
	}

	samePath, err := ioPathsEquivalent(src, finalDst)
	if err != nil {
		return err
	}
	// Lstat identity also recognizes hardlinks and host path aliases without
	// following a terminal destination symlink that overwrite would replace.
	if samePath || (finalExists && os.SameFile(srcInfo, finalInfo)) {
		return ioSamePathError(src)
	}

	if finalExists {
		if finalInfo.IsDir() {
			return ioTreeShapeError(src, finalDst)
		}
		if !overwrite {
			return ioDestinationExistsError(finalDst)
		}
		if err := os.Remove(finalDst); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(filepath.Dir(finalDst), 0o755); err != nil {
		return err
	}
	if err := ioCopyFileContents(src, finalDst, srcInfo.Mode().Perm()); err != nil {
		return err
	}
	if move {
		return os.Remove(src)
	}
	return nil
}

func ioCopyOrMoveDir(src string, srcInfo os.FileInfo, dst string, dstExists bool, dstInfo os.FileInfo, overwrite, move bool) error {
	if dstExists && !dstInfo.IsDir() {
		return ioTreeShapeError(src, dst)
	}

	if !dstExists {
		if err := os.MkdirAll(dst, srcInfo.Mode().Perm()); err != nil {
			return err
		}
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		childSrc := filepath.Join(src, entry.Name())
		childDst := filepath.Join(dst, entry.Name())
		childInfo, err := os.Lstat(childSrc)
		if err != nil {
			return err
		}
		if childInfo.Mode()&os.ModeSymlink != 0 {
			return ioUnsupportedSymlinkError(childSrc)
		}

		dstChildInfo, dstChildExists, err := ioLstatOptional(childDst)
		if err != nil {
			return err
		}
		if childInfo.IsDir() {
			if dstChildExists && !dstChildInfo.IsDir() {
				return ioTreeShapeError(childSrc, childDst)
			}
			if err := ioCopyOrMoveDir(childSrc, childInfo, childDst, dstChildExists, dstChildInfo, overwrite, move); err != nil {
				return err
			}
			continue
		}
		if err := ioCopyOrMoveFile(childSrc, childInfo, childDst, dstChildExists, dstChildInfo, overwrite, move); err != nil {
			return err
		}
	}

	if move {
		return os.Remove(src)
	}
	return nil
}

func ioCopyFileContents(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func ioLstatOptional(path string) (os.FileInfo, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return info, true, nil
}

func ioPathsEquivalent(a, b string) (bool, error) {
	absA, err := filepath.Abs(a)
	if err != nil {
		return false, err
	}
	absB, err := filepath.Abs(b)
	if err != nil {
		return false, err
	}
	return absA == absB, nil
}

func ioPathIsSameOrDescendant(root, candidate string) (bool, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return false, err
	}
	if rel, err := filepath.Rel(absRoot, absCandidate); err == nil &&
		rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return true, nil
	}
	rootInfo, err := os.Lstat(absRoot)
	if err != nil {
		return false, err
	}
	candidateInfo, exists, err := ioLstatOptional(absCandidate)
	if err != nil {
		return false, err
	}
	if exists && os.SameFile(rootInfo, candidateInfo) {
		return true, nil
	}
	// Resolve only the nearest existing parent, preserving the terminal
	// destination symlink policy. Its physical ancestry can include root
	// even when its lexical ancestry does not, as with an external alias
	// pointing to a proper descendant of root.
	parent := filepath.Dir(absCandidate)
	for {
		_, err := os.Lstat(parent)
		if err == nil {
			parent, err = filepath.EvalSymlinks(parent)
			if err != nil {
				return false, err
			}
			break
		}
		if !os.IsNotExist(err) || filepath.Dir(parent) == parent {
			return false, err
		}
		parent = filepath.Dir(parent)
	}
	for ; ; parent = filepath.Dir(parent) {
		info, err := os.Stat(parent)
		if err != nil {
			return false, err
		}
		if os.SameFile(rootInfo, info) {
			return true, nil
		}
		if filepath.Dir(parent) == parent {
			return false, nil
		}
	}
}

func ioSamePathError(path string) error {
	return fmt.Errorf("source and destination must be different: %s", path)
}

func ioRecursiveDestinationError(src, dst string) error {
	return fmt.Errorf("destination must not be the source or a descendant of the source: %s -> %s", src, dst)
}

func ioDestinationExistsError(path string) error {
	return fmt.Errorf("destination exists and :overwrite is false: %s", path)
}

func ioTreeShapeError(src, dst string) error {
	return fmt.Errorf("incompatible source/destination shapes: %s -> %s", src, dst)
}

func ioUnsupportedSymlinkError(path string) error {
	return fmt.Errorf("symlink handling is not supported for tree mutation: %s", path)
}

func ioHostPath(path string) string {
	return filepath.Clean(path)
}
