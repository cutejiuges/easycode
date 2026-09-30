package catalog

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/ncruces/go-sqlite3"
	_ "github.com/ncruces/go-sqlite3/embed"

	"easycode/internal/domain"
)

const schemaVersion = 1

const schemaSQL = `
CREATE TABLE threads (
    thread_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    journal_path TEXT NOT NULL UNIQUE,
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL,
    last_sequence INTEGER NOT NULL,
    last_checksum TEXT NOT NULL,
    creation_cwd TEXT NOT NULL,
    provider_family TEXT NOT NULL,
    provider_wire TEXT NOT NULL,
    model TEXT NOT NULL
) STRICT;
CREATE INDEX threads_continue_lookup ON threads (
    creation_cwd,
    provider_family,
    provider_wire,
    model,
    updated_at_ns DESC,
    thread_id DESC
);
PRAGMA user_version = 1;
`

// Catalog 持有一次显式 Catalog 协调所需的锁和单一 SQLite 连接。
type Catalog struct {
	config Config
	store  *secureStore
	db     *sqlite3.Conn
	open   bool
}

// New 创建纯内存 Catalog 配置，不访问文件系统或数据库。
func New(config Config) (*Catalog, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	if config.BusyTimeout == 0 {
		config.BusyTimeout = 5 * time.Second
	}
	return &Catalog{config: config}, nil
}

// Open 取得跨进程 Catalog 锁，并打开、校验或重建 schema v1。
func (catalog *Catalog) Open(ctx context.Context) error {
	if catalog == nil {
		return fmt.Errorf("session catalog is required")
	}
	if catalog.open {
		return nil
	}
	if ctx == nil {
		return fmt.Errorf("catalog context is required")
	}
	store, err := openSecureStore(ctx, catalog.config.DatabasePath)
	if err != nil {
		return err
	}
	catalog.store = store
	if err := store.ensureDatabase(); err != nil {
		_ = catalog.Close()
		return err
	}
	db, err := catalog.openConnection(ctx, store.path(store.dbName))
	if err == nil {
		err = validateSchema(db)
	}
	if err != nil {
		if db != nil {
			_ = db.Close()
		}
		db, err = catalog.rebuildEmpty(ctx)
	}
	if err != nil {
		_ = catalog.Close()
		return err
	}
	catalog.db = db
	catalog.open = true
	return nil
}

func (catalog *Catalog) openConnection(ctx context.Context, path string) (*sqlite3.Conn, error) {
	db, err := sqlite3.OpenFlags(path, sqlite3.OPEN_READWRITE|sqlite3.OPEN_CREATE|sqlite3.OPEN_NOFOLLOW)
	if err != nil {
		return nil, fmt.Errorf("open session catalog database: %w", err)
	}
	db.SetInterrupt(ctx)
	if err := db.BusyTimeout(catalog.config.BusyTimeout); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure session catalog busy timeout: %w", err)
	}
	if err := db.Exec("PRAGMA journal_mode=DELETE; PRAGMA synchronous=FULL; PRAGMA temp_store=MEMORY; PRAGMA foreign_keys=ON;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure session catalog database: %w", err)
	}
	if err := catalog.store.validateSidecars(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func validateSchema(db *sqlite3.Conn) error {
	check, err := querySingleText(db, "PRAGMA quick_check")
	if err != nil || check != "ok" {
		return fmt.Errorf("session catalog integrity check failed")
	}
	version, err := querySingleInt64(db, "PRAGMA user_version")
	if err != nil || version != schemaVersion {
		return fmt.Errorf("session catalog schema is incompatible")
	}
	wantColumns := []string{
		"thread_id", "session_id", "journal_path", "created_at_ns", "updated_at_ns",
		"last_sequence", "last_checksum", "creation_cwd", "provider_family", "provider_wire", "model",
	}
	statement, _, err := db.Prepare("PRAGMA table_info(threads)")
	if err != nil {
		return fmt.Errorf("inspect session catalog schema: %w", err)
	}
	defer func() { _ = statement.Close() }()
	columns := make([]string, 0, len(wantColumns))
	for statement.Step() {
		columns = append(columns, statement.ColumnText(1))
	}
	if err := statement.Err(); err != nil {
		return fmt.Errorf("inspect session catalog schema: %w", err)
	}
	if len(columns) != len(wantColumns) {
		return fmt.Errorf("session catalog schema is incompatible")
	}
	for index := range columns {
		if columns[index] != wantColumns[index] {
			return fmt.Errorf("session catalog schema is incompatible")
		}
	}
	indexCount, err := querySingleInt64(db, "SELECT count(*) FROM sqlite_schema WHERE type='index' AND name='threads_continue_lookup'")
	if err != nil || indexCount != 1 {
		return fmt.Errorf("session catalog schema is incompatible")
	}
	return nil
}

func (catalog *Catalog) rebuildEmpty(ctx context.Context) (*sqlite3.Conn, error) {
	temporaryName, err := catalog.store.createTemporaryDatabase()
	if err != nil {
		return nil, err
	}
	keepTemporary := true
	defer func() {
		if keepTemporary {
			_ = catalog.store.removeTemporary(temporaryName)
		}
	}()
	temporaryPath := catalog.store.path(temporaryName)
	db, err := catalog.openConnection(ctx, temporaryPath)
	if err != nil {
		return nil, err
	}
	if err := catalog.store.openTemporaryJournal(temporaryName); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := db.Exec("BEGIN IMMEDIATE;" + schemaSQL + "COMMIT;"); err != nil {
		_ = db.Exec("ROLLBACK")
		_ = db.Close()
		return nil, fmt.Errorf("create session catalog schema: %w", err)
	}
	if err := db.Close(); err != nil {
		return nil, fmt.Errorf("close rebuilt session catalog: %w", err)
	}
	file, err := catalog.store.openNamedDatabase(temporaryName)
	if err != nil {
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("sync rebuilt session catalog: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close synced session catalog: %w", err)
	}
	if err := catalog.store.replaceDatabase(temporaryName); err != nil {
		return nil, err
	}
	keepTemporary = false
	return catalog.openConnection(ctx, catalog.store.path(catalog.store.dbName))
}

// LatestCompatible 返回精确 selector 下确定性排序的最近 root thread。
func (catalog *Catalog) LatestCompatible(ctx context.Context, selector Selector) (Entry, bool, error) {
	if err := validateSelector(selector); err != nil {
		return Entry{}, false, err
	}
	if err := catalog.ready(ctx); err != nil {
		return Entry{}, false, err
	}
	statement, _, err := catalog.db.Prepare(`SELECT session_id, thread_id, journal_path, created_at_ns, updated_at_ns,
last_sequence, last_checksum, creation_cwd, provider_family, provider_wire, model
FROM threads WHERE creation_cwd=?1 AND provider_family=?2 AND provider_wire=?3 AND model=?4
ORDER BY updated_at_ns DESC, thread_id DESC LIMIT 1`)
	if err != nil {
		return Entry{}, false, fmt.Errorf("prepare session catalog query: %w", err)
	}
	defer func() { _ = statement.Close() }()
	if err := bindTexts(statement, selector.CreationCWD, string(selector.ProviderFamily), selector.ProviderWire, selector.Model); err != nil {
		return Entry{}, false, err
	}
	if !statement.Step() {
		if err := statement.Err(); err != nil {
			return Entry{}, false, fmt.Errorf("query session catalog: %w", err)
		}
		return Entry{}, false, nil
	}
	entry, err := decodeEntry(statement)
	if err != nil {
		return Entry{}, false, err
	}
	return entry, true, nil
}

func decodeEntry(statement *sqlite3.Stmt) (Entry, error) {
	sessionID, err := domain.ParseSessionID(statement.ColumnText(0))
	if err != nil {
		return Entry{}, fmt.Errorf("session catalog row is invalid")
	}
	threadID, err := domain.ParseThreadID(statement.ColumnText(1))
	if err != nil {
		return Entry{}, fmt.Errorf("session catalog row is invalid")
	}
	sequence := statement.ColumnInt64(5)
	if sequence <= 0 {
		return Entry{}, fmt.Errorf("session catalog row is invalid")
	}
	entry := Entry{
		SessionID: sessionID, ThreadID: threadID, JournalPath: statement.ColumnText(2),
		CreatedAt: time.Unix(0, statement.ColumnInt64(3)).UTC(), UpdatedAt: time.Unix(0, statement.ColumnInt64(4)).UTC(),
		LastSequence: uint64(sequence), LastChecksum: statement.ColumnText(6), CreationCWD: statement.ColumnText(7),
		ProviderFamily: domain.ProviderFamily(statement.ColumnText(8)), ProviderWire: statement.ColumnText(9), Model: statement.ColumnText(10),
	}
	if err := validateEntry(entry); err != nil {
		return Entry{}, fmt.Errorf("session catalog row is invalid")
	}
	return entry, nil
}

func (catalog *Catalog) ready(ctx context.Context) error {
	if catalog == nil || !catalog.open || catalog.db == nil {
		return fmt.Errorf("session catalog is not open")
	}
	if ctx == nil {
		return fmt.Errorf("catalog context is required")
	}
	catalog.db.SetInterrupt(ctx)
	return nil
}

func (catalog *Catalog) beginWrite(ctx context.Context) error {
	if err := catalog.ready(ctx); err != nil {
		return err
	}
	if err := catalog.store.ensureRollbackJournal(); err != nil {
		return err
	}
	if err := catalog.db.Exec("BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin session catalog transaction: %w", err)
	}
	return nil
}

func (catalog *Catalog) upsert(entry Entry) error {
	if err := validateEntry(entry); err != nil {
		return err
	}
	if entry.LastSequence > math.MaxInt64 {
		return fmt.Errorf("catalog entry sequence is invalid")
	}
	statement, _, err := catalog.db.Prepare(`INSERT INTO threads (
thread_id, session_id, journal_path, created_at_ns, updated_at_ns, last_sequence, last_checksum,
creation_cwd, provider_family, provider_wire, model
) VALUES (?1,?2,?3,?4,?5,?6,?7,?8,?9,?10,?11)
ON CONFLICT(thread_id) DO UPDATE SET session_id=excluded.session_id, journal_path=excluded.journal_path,
created_at_ns=excluded.created_at_ns, updated_at_ns=excluded.updated_at_ns,
last_sequence=excluded.last_sequence, last_checksum=excluded.last_checksum,
creation_cwd=excluded.creation_cwd, provider_family=excluded.provider_family,
provider_wire=excluded.provider_wire, model=excluded.model`)
	if err != nil {
		return fmt.Errorf("prepare session catalog upsert: %w", err)
	}
	defer func() { _ = statement.Close() }()
	values := []string{string(entry.ThreadID), string(entry.SessionID), entry.JournalPath}
	if err := bindTexts(statement, values...); err != nil {
		return err
	}
	if err := statement.BindInt64(4, entry.CreatedAt.UnixNano()); err != nil {
		return err
	}
	if err := statement.BindInt64(5, entry.UpdatedAt.UnixNano()); err != nil {
		return err
	}
	if err := statement.BindInt64(6, int64(entry.LastSequence)); err != nil {
		return err
	}
	for index, value := range []string{entry.LastChecksum, entry.CreationCWD, string(entry.ProviderFamily), entry.ProviderWire, entry.Model} {
		if err := statement.BindText(index+7, value); err != nil {
			return err
		}
	}
	if err := statement.Exec(); err != nil {
		return fmt.Errorf("upsert session catalog entry: %w", err)
	}
	return nil
}

func bindTexts(statement *sqlite3.Stmt, values ...string) error {
	for index, value := range values {
		if err := statement.BindText(index+1, value); err != nil {
			return fmt.Errorf("bind session catalog value: %w", err)
		}
	}
	return nil
}

func querySingleText(db *sqlite3.Conn, query string) (string, error) {
	statement, _, err := db.Prepare(query)
	if err != nil {
		return "", err
	}
	defer func() { _ = statement.Close() }()
	if !statement.Step() {
		if err := statement.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("query returned no rows")
	}
	return statement.ColumnText(0), nil
}

func querySingleInt64(db *sqlite3.Conn, query string) (int64, error) {
	statement, _, err := db.Prepare(query)
	if err != nil {
		return 0, err
	}
	defer func() { _ = statement.Close() }()
	if !statement.Step() {
		if err := statement.Err(); err != nil {
			return 0, err
		}
		return 0, fmt.Errorf("query returned no rows")
	}
	return statement.ColumnInt64(0), nil
}

// Close 幂等关闭 SQLite、释放 Catalog 锁和 data-home 句柄。
func (catalog *Catalog) Close() error {
	if catalog == nil {
		return nil
	}
	var result error
	if catalog.db != nil {
		result = errors.Join(result, catalog.db.Close())
		catalog.db = nil
	}
	if catalog.store != nil {
		result = errors.Join(result, catalog.store.Close())
		catalog.store = nil
	}
	catalog.open = false
	return result
}
