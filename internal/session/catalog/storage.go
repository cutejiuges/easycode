package catalog

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

var (
	errInvalidSnapshot              = errors.New("session catalog snapshot is invalid")
	errSnapshotPublicationUncertain = errors.New("session catalog publication is uncertain")
)

// Catalog 持有跨进程锁及本次打开看到的已验证快照。
type Catalog struct {
	config      Config
	store       *secureStore
	entries     []Entry
	hasSnapshot bool
	uncertain   bool
}

// New 创建纯内存 Catalog 配置。
func New(config Config) (*Catalog, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	return &Catalog{config: config}, nil
}

// Open 取得 Catalog 锁并读取安全打开且符合当前结构的快照。
func (catalog *Catalog) Open(ctx context.Context) error {
	if catalog == nil || ctx == nil {
		return fmt.Errorf("session catalog and context are required")
	}
	if catalog.store != nil {
		return catalog.ready(ctx)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	store, err := openSecureStore(ctx, catalog.config.SnapshotPath)
	if err != nil {
		return err
	}
	catalog.store = store
	file, err := store.openSnapshot()
	if canceled := ctx.Err(); canceled != nil {
		return errors.Join(canceled, catalog.Close())
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.Join(err, catalog.Close())
	}
	entries, err := decodeSnapshot(ctx, file)
	if err != nil {
		if ctx.Err() != nil {
			return errors.Join(ctx.Err(), catalog.Close())
		}
		var syntaxError *json.SyntaxError
		if errors.Is(err, errInvalidSnapshot) || errors.Is(err, io.ErrUnexpectedEOF) ||
			errors.Is(err, io.EOF) || errors.As(err, &syntaxError) {
			return nil
		}
		return errors.Join(fmt.Errorf("read session catalog snapshot: %w", err), catalog.Close())
	}
	catalog.entries = entries
	catalog.hasSnapshot = true
	return nil
}

func decodeSnapshot(ctx context.Context, reader io.Reader) ([]Entry, error) {
	decoder := json.NewDecoder(reader)
	open, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if open != json.Delim('[') {
		return nil, errInvalidSnapshot
	}

	var entries []Entry
	seen := make(map[domain.ThreadID]struct{})
	for decoder.More() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
		var entry Entry
		if codec.UnmarshalRecord(raw, &entry) != nil || validateEntry(entry) != nil {
			return nil, errInvalidSnapshot
		}
		if _, duplicate := seen[entry.ThreadID]; duplicate ||
			(len(entries) != 0 && compareEntry(entries[len(entries)-1], entry) >= 0) {
			return nil, errInvalidSnapshot
		}
		seen[entry.ThreadID] = struct{}{}
		entries = append(entries, entry)
	}
	close, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if close != json.Delim(']') {
		return nil, errInvalidSnapshot
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, errInvalidSnapshot
	}
	return entries, ctx.Err()
}

func (catalog *Catalog) publish(ctx context.Context, entries []Entry) error {
	if err := catalog.ready(ctx); err != nil {
		return err
	}
	slices.SortFunc(entries, compareEntry)
	if catalog.hasSnapshot && slices.Equal(entries, catalog.entries) {
		return errors.Join(catalog.store.checkSnapshot(), ctx.Err())
	}
	err := catalog.store.publishSnapshot(ctx, func(writer io.Writer) error {
		return writeSnapshot(ctx, writer, entries)
	})
	if err != nil {
		catalog.uncertain = errors.Is(err, errSnapshotPublicationUncertain)
		return err
	}
	catalog.entries = entries
	catalog.hasSnapshot = true
	return nil
}

func writeSnapshot(ctx context.Context, writer io.Writer, entries []Entry) error {
	buffer := bufio.NewWriter(writer)
	if _, err := buffer.WriteString("["); err != nil {
		return err
	}
	for index, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		data, err := codec.MarshalStable(entry)
		if err != nil {
			return fmt.Errorf("encode session catalog entry: %w", err)
		}
		if index > 0 {
			if _, err := buffer.WriteString(","); err != nil {
				return err
			}
		}
		if _, err := buffer.Write(data); err != nil {
			return err
		}
	}
	if _, err := buffer.WriteString("]\n"); err != nil {
		return err
	}
	return buffer.Flush()
}

func compareEntry(left, right Entry) int {
	if order := right.UpdatedAt.Compare(left.UpdatedAt); order != 0 {
		return order
	}
	return cmp.Compare(right.ThreadID, left.ThreadID)
}

// LatestCompatible 返回精确条件下最近的 root thread。
func (catalog *Catalog) LatestCompatible(ctx context.Context, selector Selector) (Entry, bool, error) {
	if err := validateSelector(selector); err != nil {
		return Entry{}, false, err
	}
	if err := catalog.ready(ctx); err != nil {
		return Entry{}, false, err
	}
	for _, entry := range catalog.entries {
		if err := ctx.Err(); err != nil {
			return Entry{}, false, err
		}
		if entry.CreationCWD == selector.CreationCWD && entry.ProviderFamily == selector.ProviderFamily &&
			entry.ProviderWire == selector.ProviderWire && entry.Model == selector.Model {
			return entry, true, nil
		}
	}
	return Entry{}, false, ctx.Err()
}

func (catalog *Catalog) ready(ctx context.Context) error {
	if catalog == nil || catalog.store == nil {
		return fmt.Errorf("session catalog is not open")
	}
	if ctx == nil {
		return fmt.Errorf("catalog context is required")
	}
	if catalog.uncertain {
		return errSnapshotPublicationUncertain
	}
	return ctx.Err()
}

// Close 释放快照、Catalog 锁和所属目录句柄。
func (catalog *Catalog) Close() error {
	if catalog == nil || catalog.store == nil {
		return nil
	}
	err := catalog.store.Close()
	catalog.store = nil
	catalog.entries = nil
	catalog.hasSnapshot = false
	catalog.uncertain = false
	return err
}
