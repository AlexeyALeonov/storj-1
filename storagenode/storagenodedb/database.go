// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

package storagenodedb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-sqlite3" // used indirectly
	"github.com/zeebo/errs"
	"go.uber.org/zap"
	monkit "gopkg.in/spacemonkeygo/monkit.v2"

	"storj.io/storj/internal/dbutil"
	"storj.io/storj/internal/dbutil/sqliteutil"
	"storj.io/storj/internal/migrate"
	"storj.io/storj/pkg/kademlia"
	"storj.io/storj/storage"
	"storj.io/storj/storage/boltdb"
	"storj.io/storj/storage/filestore"
	"storj.io/storj/storage/teststore"
	"storj.io/storj/storagenode"
	"storj.io/storj/storagenode/bandwidth"
	"storj.io/storj/storagenode/console"
	"storj.io/storj/storagenode/orders"
	"storj.io/storj/storagenode/pieces"
	"storj.io/storj/storagenode/piecestore"
	"storj.io/storj/storagenode/reputation"
	"storj.io/storj/storagenode/storageusage"
)

var (
	mon = monkit.Package()

	// ErrDatabase represents errors from the databases.
	ErrDatabase = errs.Class("storage node database error")
)

var _ storagenode.DB = (*DB)(nil)

// SQLDB defines interface that matches *sql.DB
// this is such that we can use utccheck.DB for the backend
//
// TODO: wrap the connector instead of *sql.DB
type SQLDB interface {
	Begin() (*sql.Tx, error)
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
	Close() error
	Conn(ctx context.Context) (*sql.Conn, error)
	Driver() driver.Driver
	Exec(query string, args ...interface{}) (sql.Result, error)
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	Ping() error
	PingContext(ctx context.Context) error
	Prepare(query string) (*sql.Stmt, error)
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
	SetConnMaxLifetime(d time.Duration)
	SetMaxIdleConns(n int)
	SetMaxOpenConns(n int)
}

// Config configures storage node database
type Config struct {
	// TODO: figure out better names
	Storage  string
	Info     string
	Info2    string
	Kademlia string

	Pieces string
}

// DB contains access to different database tables
type DB struct {
	log *zap.Logger

	pieces interface {
		storage.Blobs
		Close() error
	}

	versionsDB        *versionsDB
	v0PieceInfoDB     *v0PieceInfoDB
	bandwidthDB       *bandwidthDB
	consoleDB         *consoleDB
	ordersDB          *ordersDB
	pieceExpirationDB *pieceExpirationDB
	pieceSpaceUsedDB  *pieceSpaceUsedDB
	reputationDB      *reputationDB
	storageUsageDB    *storageusageDB
	usedSerialsDB     *usedSerialsDB

	kdb, ndb, adb storage.KeyValueStore

	sqliteDriverInstanceKey string
	registeredSQLite3Hook   bool
	conlock                 sync.Mutex
	connections             map[string]*sqlite3.SQLiteConn
}

// New creates a new master database for storage node
func New(log *zap.Logger, config Config) (*DB, error) {
	piecesDir, err := filestore.NewDir(config.Pieces)
	if err != nil {
		return nil, err
	}
	pieces := filestore.New(log, piecesDir)

	dbs, err := boltdb.NewShared(config.Kademlia, kademlia.KademliaBucket, kademlia.NodeBucket, kademlia.AntechamberBucket)
	if err != nil {
		return nil, err
	}

	db := &DB{
		log:    log,
		pieces: pieces,
		kdb:    dbs[0],
		ndb:    dbs[1],
		adb:    dbs[2],

		conlock:     sync.Mutex{},
		connections: make(map[string]*sqlite3.SQLiteConn),
	}

	// The sqlite driver is needed in order to perform backups. We use a connect hook to intercept it.
	db.sqliteDriverInstanceKey = sqliteutil.Sqlite3DriverName + strconv.FormatInt(rand.Int63(), 10)
	db.conlock.Lock()
	if db.registeredSQLite3Hook == false {
		sql.Register(db.sqliteDriverInstanceKey, &sqlite3.SQLiteDriver{
			ConnectHook: func(conn *sqlite3.SQLiteConn) error {
				fileName := strings.ToLower(filepath.Base(conn.GetFilename("")))
				db.conlock.Lock()
				db.connections[fileName] = conn
				db.conlock.Unlock()
				return nil
			},
		})
		db.registeredSQLite3Hook = true
	}
	db.conlock.Unlock()

	databasesPath := filepath.Dir(config.Info2)
	err = db.openDatabases(databasesPath)
	if err != nil {
		return nil, err
	}
	return db, nil
}

// NewTest creates new test database for storage node.
func NewTest(log *zap.Logger, storageDir string) (*DB, error) {
	piecesDir, err := filestore.NewDir(storageDir)
	if err != nil {
		return nil, err
	}
	pieces := filestore.New(log, piecesDir)

	versionsDB, err := openTestDatabase()
	if err != nil {
		return nil, err
	}

	db := &DB{
		log:    log,
		pieces: pieces,
		kdb:    teststore.New(),
		ndb:    teststore.New(),
		adb:    teststore.New(),

		// Initialize databases. Currently shares one info.db database file but
		// in the future these will initialize their own database connections.
		versionsDB:        newVersionsDB(versionsDB, ""),
		v0PieceInfoDB:     newV0PieceInfoDB(versionsDB),
		bandwidthDB:       newBandwidthDB(versionsDB),
		consoleDB:         newConsoleDB(versionsDB),
		ordersDB:          newOrdersDB(versionsDB),
		pieceExpirationDB: newPieceExpirationDB(versionsDB),
		pieceSpaceUsedDB:  newPieceSpaceUsedDB(versionsDB),
		reputationDB:      newReputationDB(versionsDB),
		storageUsageDB:    newStorageusageDB(versionsDB),
		usedSerialsDB:     newUsedSerialsDB(versionsDB),
		vouchersDB:        newVouchersDB(versionsDB),
	}
	return db, nil
}

// openDatabases opens all the SQLite3 storage node databases and returns if any fails to open successfully.
func (db *DB) openDatabases(databasesPath string) error {
	// We open the versions database first because this one has the DB schema versioning info
	// we need before anything else.
	versionsDB, err := openDatabase(db.sqliteDriverInstanceKey, filepath.Join(databasesPath, VersionsDatabaseFilename))
	if err != nil {
		return err
	}
	if db.versionsDB == nil {
		db.versionsDB = newVersionsDB(versionsDB, filepath.Join(databasesPath, VersionsDatabaseFilename))
	} else {
		db.versionsDB.SQLDB = versionsDB
	}

	bandwidthDB, err := openDatabase(db.sqliteDriverInstanceKey, filepath.Join(databasesPath, BandwidthDatabaseFilename))
	if err != nil {
		return err
	}
	if db.bandwidthDB == nil {
		db.bandwidthDB = newBandwidthDB(bandwidthDB)
	} else {
		db.bandwidthDB.SQLDB = bandwidthDB
	}

	// TODO: console database?

	ordersDB, err := openDatabase(db.sqliteDriverInstanceKey, filepath.Join(databasesPath, OrdersDatabaseFilename))
	if err != nil {
		return err
	}
	if db.ordersDB == nil {
		db.ordersDB = newOrdersDB(ordersDB)
	} else {
		db.ordersDB.SQLDB = ordersDB
	}

	pieceExpirationDB, err := openDatabase(db.sqliteDriverInstanceKey, filepath.Join(databasesPath, PieceExpirationDatabaseFilename))
	if err != nil {
		return err
	}
	if db.pieceExpirationDB == nil {
		db.pieceExpirationDB = newPieceExpirationDB(pieceExpirationDB)
	} else {
		db.pieceExpirationDB.SQLDB = pieceExpirationDB
	}

	v0PieceInfoDB, err := openDatabase(db.sqliteDriverInstanceKey, filepath.Join(databasesPath, v0PieceInfoDatabaseFilename))
	if err != nil {
		return err
	}
	if db.v0PieceInfoDB == nil {
		db.v0PieceInfoDB = newV0PieceInfoDB(v0PieceInfoDB)
	} else {
		db.v0PieceInfoDB.SQLDB = v0PieceInfoDB
	}

	pieceSpaceUsedDB, err := openDatabase(db.sqliteDriverInstanceKey, filepath.Join(databasesPath, PieceSpacedUsedDatabaseFilename))
	if err != nil {
		return err
	}
	if db.pieceSpaceUsedDB == nil {
		db.pieceSpaceUsedDB = newPieceSpaceUsedDB(pieceSpaceUsedDB)
	} else {
		db.pieceSpaceUsedDB.SQLDB = pieceSpaceUsedDB
	}

	reputationDB, err := openDatabase(db.sqliteDriverInstanceKey, filepath.Join(databasesPath, ReputationDatabaseFilename))
	if err != nil {
		return err
	}
	if db.reputationDB == nil {
		db.reputationDB = newReputationDB(reputationDB)
	} else {
		db.reputationDB.SQLDB = reputationDB
	}

	storageUsageDB, err := openDatabase(db.sqliteDriverInstanceKey, filepath.Join(databasesPath, StorageUsageDatabaseFilename))
	if err != nil {
		return err
	}
	if db.storageUsageDB == nil {
		db.storageUsageDB = newStorageusageDB(storageUsageDB)
	} else {
		db.storageUsageDB.SQLDB = storageUsageDB
	}

	usedSerialsDB, err := openDatabase(db.sqliteDriverInstanceKey, filepath.Join(databasesPath, UsedSerialsDatabaseFilename))
	if err != nil {
		return err
	}
	if db.usedSerialsDB == nil {
		db.usedSerialsDB = newUsedSerialsDB(usedSerialsDB)
	} else {
		db.usedSerialsDB.SQLDB = usedSerialsDB
	}

	vouchersDB, err := openDatabase(db.sqliteDriverInstanceKey, filepath.Join(databasesPath, VouchersDatabaseFilename))
	if err != nil {
		return err
	}
	if db.vouchersDB == nil {
		db.vouchersDB = newVouchersDB(vouchersDB)
	} else {
		db.vouchersDB.SQLDB = vouchersDB
	}

	return nil
}

// openDatabase opens or creates a database at the specified path.
func openDatabase(sqliteDriverInstanceKey string, path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}

	db, err := sql.Open(sqliteDriverInstanceKey, "file:"+path+"?_journal=WAL&_busy_timeout=10000")
	if err != nil {
		return nil, ErrDatabase.Wrap(err)
	}

	dbutil.Configure(db, mon)
	// TODO: This shouldn't be needed. When a database is first opened it hasn't been created on disk yet.
	// This flushes the newly initialized database to disk.
	db.Ping()
	return db, nil
}

// // openTestDatabase creates an in memory database.
func openTestDatabase() (*sql.DB, error) {
	// create memory DB with a shared cache and a unique name to avoid collisions
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:memdb%d?mode=memory&cache=shared", rand.Int63()))
	if err != nil {
		return nil, ErrDatabase.Wrap(err)
	}

	// Set max idle and max open to 1 to control concurrent access to the memory DB
	// Setting max open higher than 1 results in table locked errors
	db.SetMaxIdleConns(1)
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(-1)

	mon.Chain("db_stats", monkit.StatSourceFunc(
		func(cb func(name string, val float64)) {
			monkit.StatSourceFromStruct(db.Stats()).Stats(cb)
		}))

	return db, nil
}

// CreateTables creates any necessary tables.
func (db *DB) CreateTables() error {
	migration := db.Migration()
	return migration.Run(db.log.Named("migration"), db.versionsDB)
}

// Close closes any resources.
func (db *DB) Close() error {
	return errs.Combine(
		db.kdb.Close(),
		db.ndb.Close(),
		db.adb.Close(),

		db.closeDatabases(),
	)
}

func (db *DB) closeDatabases() error {
	return errs.Combine(
		db.versionsDB.Close(),
		db.bandwidthDB.Close(),
		// db.consoleDB.Close(), TODO: Fix this?
		db.ordersDB.Close(),
		db.pieceExpirationDB.Close(),
		db.v0PieceInfoDB.Close(),
		db.pieceSpaceUsedDB.Close(),
		db.reputationDB.Close(),
		db.storageUsageDB.Close(),
		db.usedSerialsDB.Close(),
	)
}

// VersionsMigration returns the instance of the versions database.
func (db *DB) VersionsMigration() migrate.DB {
	return db.versionsDB
}

// Versions returns the instance of the versions database.
func (db *DB) Versions() SQLDB {
	return db.versionsDB
}

// V0PieceInfo returns the instance of the V0PieceInfoDB database.
func (db *DB) V0PieceInfo() pieces.V0PieceInfoDB {
	return db.v0PieceInfoDB
}

// Bandwidth returns the instance of the Bandwidth database.
func (db *DB) Bandwidth() bandwidth.DB {
	return db.bandwidthDB
}

// Console returns the instance of the Console database.
func (db *DB) Console() console.DB {
	return db.consoleDB
}

// Orders returns the instance of the Orders database.
func (db *DB) Orders() orders.DB {
	return db.ordersDB
}

// Pieces returns blob storage for pieces
func (db *DB) Pieces() storage.Blobs {
	return db.pieces
}

// PieceExpirationDB returns the instance of the PieceExpiration database.
func (db *DB) PieceExpirationDB() pieces.PieceExpirationDB {
	return db.pieceExpirationDB
}

// PieceSpaceUsedDB returns the instance of the PieceSpacedUsed database.
func (db *DB) PieceSpaceUsedDB() pieces.PieceSpaceUsedDB {
	return db.pieceSpaceUsedDB
}

// Reputation returns the instance of the Reputation database.
func (db *DB) Reputation() reputation.DB {
	return db.reputationDB
}

// StorageUsage returns the instance of the StorageUsage database.
func (db *DB) StorageUsage() storageusage.DB {
	return db.storageUsageDB
}

// UsedSerials returns the instance of the UsedSerials database.
func (db *DB) UsedSerials() piecestore.UsedSerials {
	return db.usedSerialsDB
}

// RoutingTable returns kademlia routing table
func (db *DB) RoutingTable() (kdb, ndb, adb storage.KeyValueStore) {
	return db.kdb, db.ndb, db.adb
}

// Migration returns table migrations.
func (db *DB) Migration() *migrate.Migration {
	return &migrate.Migration{
		Table: "versions",
		Steps: []*migrate.Step{
			{
				Description: "Initial setup",
				Version:     0,
				Action: migrate.SQL{
					// table for keeping serials that need to be verified against
					`CREATE TABLE used_serial (
						satellite_id  BLOB NOT NULL,
						serial_number BLOB NOT NULL,
						expiration    TIMESTAMP NOT NULL
					)`,
					// primary key on satellite id and serial number
					`CREATE UNIQUE INDEX pk_used_serial ON used_serial(satellite_id, serial_number)`,
					// expiration index to allow fast deletion
					`CREATE INDEX idx_used_serial ON used_serial(expiration)`,

					// certificate table for storing uplink/satellite certificates
					`CREATE TABLE certificate (
						cert_id       INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL,
						node_id       BLOB        NOT NULL, -- same NodeID can have multiple valid leaf certificates
						peer_identity BLOB UNIQUE NOT NULL  -- PEM encoded
					)`,

					// table for storing piece meta info
					`CREATE TABLE pieceinfo (
						satellite_id     BLOB      NOT NULL,
						piece_id         BLOB      NOT NULL,
						piece_size       BIGINT    NOT NULL,
						piece_expiration TIMESTAMP, -- date when it can be deleted

						uplink_piece_hash BLOB    NOT NULL, -- serialized pb.PieceHash signed by uplink
						uplink_cert_id    INTEGER NOT NULL,

						FOREIGN KEY(uplink_cert_id) REFERENCES certificate(cert_id)
					)`,
					// primary key by satellite id and piece id
					`CREATE UNIQUE INDEX pk_pieceinfo ON pieceinfo(satellite_id, piece_id)`,

					// table for storing bandwidth usage
					`CREATE TABLE bandwidth_usage (
						satellite_id  BLOB    NOT NULL,
						action        INTEGER NOT NULL,
						amount        BIGINT  NOT NULL,
						created_at    TIMESTAMP NOT NULL
					)`,
					`CREATE INDEX idx_bandwidth_usage_satellite ON bandwidth_usage(satellite_id)`,
					`CREATE INDEX idx_bandwidth_usage_created   ON bandwidth_usage(created_at)`,

					// table for storing all unsent orders
					`CREATE TABLE unsent_order (
						satellite_id  BLOB NOT NULL,
						serial_number BLOB NOT NULL,

						order_limit_serialized BLOB      NOT NULL, -- serialized pb.OrderLimit
						order_serialized       BLOB      NOT NULL, -- serialized pb.Order
						order_limit_expiration TIMESTAMP NOT NULL, -- when is the deadline for sending it

						uplink_cert_id INTEGER NOT NULL,

						FOREIGN KEY(uplink_cert_id) REFERENCES certificate(cert_id)
					)`,
					`CREATE UNIQUE INDEX idx_orders ON unsent_order(satellite_id, serial_number)`,

					// table for storing all sent orders
					`CREATE TABLE order_archive (
						satellite_id  BLOB NOT NULL,
						serial_number BLOB NOT NULL,

						order_limit_serialized BLOB NOT NULL, -- serialized pb.OrderLimit
						order_serialized       BLOB NOT NULL, -- serialized pb.Order

						uplink_cert_id INTEGER NOT NULL,

						status      INTEGER   NOT NULL, -- accepted, rejected, confirmed
						archived_at TIMESTAMP NOT NULL, -- when was it rejected

						FOREIGN KEY(uplink_cert_id) REFERENCES certificate(cert_id)
					)`,
					`CREATE INDEX idx_order_archive_satellite ON order_archive(satellite_id)`,
					`CREATE INDEX idx_order_archive_status ON order_archive(status)`,
				},
			},
			{
				Description: "Network Wipe #2",
				Version:     1,
				Action: migrate.SQL{
					`UPDATE pieceinfo SET piece_expiration = '2019-05-09 00:00:00.000000+00:00'`,
				},
			},
			{
				Description: "Add tracking of deletion failures.",
				Version:     2,
				Action: migrate.SQL{
					`ALTER TABLE pieceinfo ADD COLUMN deletion_failed_at TIMESTAMP`,
				},
			},
			{
				Description: "Add vouchersDB for storing and retrieving vouchers.",
				Version:     3,
				Action: migrate.SQL{
					`CREATE TABLE vouchers (
						satellite_id BLOB PRIMARY KEY NOT NULL,
						voucher_serialized BLOB NOT NULL,
						expiration TIMESTAMP NOT NULL
					)`,
				},
			},
			{
				Description: "Add index on pieceinfo expireation",
				Version:     4,
				Action: migrate.SQL{
					`CREATE INDEX idx_pieceinfo_expiration ON pieceinfo(piece_expiration)`,
					`CREATE INDEX idx_pieceinfo_deletion_failed ON pieceinfo(deletion_failed_at)`,
				},
			},
			{
				Description: "Partial Network Wipe - Tardigrade Satellites",
				Version:     5,
				Action: migrate.SQL{
					`UPDATE pieceinfo SET piece_expiration = '2019-06-25 00:00:00.000000+00:00' WHERE satellite_id
						IN (x'84A74C2CD43C5BA76535E1F42F5DF7C287ED68D33522782F4AFABFDB40000000',
							x'A28B4F04E10BAE85D67F4C6CB82BF8D4C0F0F47A8EA72627524DEB6EC0000000',
							x'AF2C42003EFC826AB4361F73F9D890942146FE0EBE806786F8E7190800000000'
					)`,
				},
			},
			{
				Description: "Add creation date.",
				Version:     6,
				Action: migrate.SQL{
					`ALTER TABLE pieceinfo ADD COLUMN piece_creation TIMESTAMP NOT NULL DEFAULT 'epoch'`,
				},
			},
			{
				Description: "Drop certificate table.",
				Version:     7,
				Action: migrate.SQL{
					`DROP TABLE certificate`,
					`CREATE TABLE certificate (cert_id INTEGER)`,
				},
			},
			{
				Description: "Drop old used serials and remove pieceinfo_deletion_failed index.",
				Version:     8,
				Action: migrate.SQL{
					`DELETE FROM used_serial`,
					`DROP INDEX idx_pieceinfo_deletion_failed`,
				},
			},
			{
				Description: "Add order limit table.",
				Version:     9,
				Action: migrate.SQL{
					`ALTER TABLE pieceinfo ADD COLUMN order_limit BLOB NOT NULL DEFAULT X''`,
				},
			},
			{
				Description: "Optimize index usage.",
				Version:     10,
				Action: migrate.SQL{
					`DROP INDEX idx_pieceinfo_expiration`,
					`DROP INDEX idx_order_archive_satellite`,
					`DROP INDEX idx_order_archive_status`,
					`CREATE INDEX idx_pieceinfo_expiration ON pieceinfo(piece_expiration) WHERE piece_expiration IS NOT NULL`,
				},
			},
			{
				Description: "Create bandwidth_usage_rollup table.",
				Version:     11,
				Action: migrate.SQL{
					`CREATE TABLE bandwidth_usage_rollups (
										interval_start	TIMESTAMP NOT NULL,
										satellite_id  	BLOB    NOT NULL,
										action        	INTEGER NOT NULL,
										amount        	BIGINT  NOT NULL,
										PRIMARY KEY ( interval_start, satellite_id, action )
									)`,
				},
			},
			{
				Description: "Clear Tables from Alpha data",
				Version:     12,
				Action: migrate.SQL{
					`DROP TABLE pieceinfo`,
					`DROP TABLE used_serial`,
					`DROP TABLE order_archive`,
					`CREATE TABLE pieceinfo_ (
						satellite_id     BLOB      NOT NULL,
						piece_id         BLOB      NOT NULL,
						piece_size       BIGINT    NOT NULL,
						piece_expiration TIMESTAMP,

						order_limit       BLOB    NOT NULL,
						uplink_piece_hash BLOB    NOT NULL,
						uplink_cert_id    INTEGER NOT NULL,

						deletion_failed_at TIMESTAMP,
						piece_creation TIMESTAMP NOT NULL,

						FOREIGN KEY(uplink_cert_id) REFERENCES certificate(cert_id)
					)`,
					`CREATE UNIQUE INDEX pk_pieceinfo_ ON pieceinfo_(satellite_id, piece_id)`,
					`CREATE INDEX idx_pieceinfo__expiration ON pieceinfo_(piece_expiration) WHERE piece_expiration IS NOT NULL`,
					`CREATE TABLE used_serial_ (
						satellite_id  BLOB NOT NULL,
						serial_number BLOB NOT NULL,
						expiration    TIMESTAMP NOT NULL
					)`,
					`CREATE UNIQUE INDEX pk_used_serial_ ON used_serial_(satellite_id, serial_number)`,
					`CREATE INDEX idx_used_serial_ ON used_serial_(expiration)`,
					`CREATE TABLE order_archive_ (
						satellite_id  BLOB NOT NULL,
						serial_number BLOB NOT NULL,

						order_limit_serialized BLOB NOT NULL,
						order_serialized       BLOB NOT NULL,

						uplink_cert_id INTEGER NOT NULL,

						status      INTEGER   NOT NULL,
						archived_at TIMESTAMP NOT NULL,

						FOREIGN KEY(uplink_cert_id) REFERENCES certificate(cert_id)
					)`,
				},
			},
			{
				Description: "Free Storagenodes from trash data",
				Version:     13,
				Action: migrate.Func(func(log *zap.Logger, mgdb migrate.DB, tx *sql.Tx) error {
					// When using inmemory DB, skip deletion process
					if db.versionsDB.location == "" {
						return nil
					}

					err := os.RemoveAll(filepath.Join(filepath.Dir(db.versionsDB.location), "blob/ukfu6bhbboxilvt7jrwlqk7y2tapb5d2r2tsmj2sjxvw5qaaaaaa")) // us-central1
					if err != nil {
						log.Sugar().Debug(err)
					}
					err = os.RemoveAll(filepath.Join(filepath.Dir(db.versionsDB.location), "blob/v4weeab67sbgvnbwd5z7tweqsqqun7qox2agpbxy44mqqaaaaaaa")) // europe-west1
					if err != nil {
						log.Sugar().Debug(err)
					}
					err = os.RemoveAll(filepath.Join(filepath.Dir(db.versionsDB.location), "blob/qstuylguhrn2ozjv4h2c6xpxykd622gtgurhql2k7k75wqaaaaaa")) // asia-east1
					if err != nil {
						log.Sugar().Debug(err)
					}
					err = os.RemoveAll(filepath.Join(filepath.Dir(db.versionsDB.location), "blob/abforhuxbzyd35blusvrifvdwmfx4hmocsva4vmpp3rgqaaaaaaa")) // "tothemoon (stefan)"
					if err != nil {
						log.Sugar().Debug(err)
					}
					// To prevent the node from starting up, we just log errors and return nil
					return nil
				}),
			},
			{
				Description: "Free Storagenodes from orphaned tmp data",
				Version:     14,
				Action: migrate.Func(func(log *zap.Logger, mgdb migrate.DB, tx *sql.Tx) error {
					// When using inmemory DB, skip deletion process
					if db.versionsDB.location == "" {
						return nil
					}

					err := os.RemoveAll(filepath.Join(filepath.Dir(db.versionsDB.location), "tmp"))
					if err != nil {
						log.Sugar().Debug(err)
					}
					// To prevent the node from starting up, we just log errors and return nil
					return nil
				}),
			},
			{
				Description: "Start piece_expirations table, deprecate pieceinfo table",
				Version:     15,
				Action: migrate.SQL{
					// new table to hold expiration data (and only expirations. no other pieceinfo)
					`CREATE TABLE piece_expirations (
						satellite_id       BLOB      NOT NULL,
						piece_id           BLOB      NOT NULL,
						piece_expiration   TIMESTAMP NOT NULL, -- date when it can be deleted
						deletion_failed_at TIMESTAMP,
						PRIMARY KEY (satellite_id, piece_id)
					)`,
					`CREATE INDEX idx_piece_expirations_piece_expiration ON piece_expirations(piece_expiration)`,
					`CREATE INDEX idx_piece_expirations_deletion_failed_at ON piece_expirations(deletion_failed_at)`,
				},
			},
			{
				Description: "Add reputation and storage usage cache tables",
				Version:     16,
				Action: migrate.SQL{
					`CREATE TABLE reputation (
						satellite_id BLOB NOT NULL,
						uptime_success_count INTEGER NOT NULL,
						uptime_total_count INTEGER NOT NULL,
						uptime_reputation_alpha REAL NOT NULL,
						uptime_reputation_beta REAL NOT NULL,
						uptime_reputation_score REAL NOT NULL,
						audit_success_count INTEGER NOT NULL,
						audit_total_count INTEGER NOT NULL,
						audit_reputation_alpha REAL NOT NULL,
						audit_reputation_beta REAL NOT NULL,
						audit_reputation_score REAL NOT NULL,
						updated_at TIMESTAMP NOT NULL,
						PRIMARY KEY (satellite_id)
					)`,
					`CREATE TABLE storage_usage (
						satellite_id BLOB NOT NULL,
						at_rest_total REAL NOT NUll,
						timestamp TIMESTAMP NOT NULL,
						PRIMARY KEY (satellite_id, timestamp)
					)`,
				},
			},
			{
				Description: "Create piece_space_used table",
				Version:     17,
				Action: migrate.SQL{
					// new table to hold the most recent totals from the piece space used cache
					`CREATE TABLE piece_space_used (
						total INTEGER NOT NULL,
						satellite_id BLOB
					)`,
					`CREATE UNIQUE INDEX idx_piece_space_used_satellite_id ON piece_space_used(satellite_id)`,
					`INSERT INTO piece_space_used (total) select ifnull(sum(piece_size), 0) from pieceinfo_`,
				},
			},
			{
				Description: "Drop vouchers table",
				Version:     18,
				Action: migrate.SQL{
					`DROP TABLE vouchers`,
				},
			},
			{
				Description: "Add disqualified field to reputation",
				Version:     19,
				Action: migrate.SQL{
					`DROP TABLE reputation;`,
					`CREATE TABLE reputation (
						satellite_id BLOB NOT NULL,
						uptime_success_count INTEGER NOT NULL,
						uptime_total_count INTEGER NOT NULL,
						uptime_reputation_alpha REAL NOT NULL,
						uptime_reputation_beta REAL NOT NULL,
						uptime_reputation_score REAL NOT NULL,
						audit_success_count INTEGER NOT NULL,
						audit_total_count INTEGER NOT NULL,
						audit_reputation_alpha REAL NOT NULL,
						audit_reputation_beta REAL NOT NULL,
						audit_reputation_score REAL NOT NULL,
						disqualified TIMESTAMP,
						updated_at TIMESTAMP NOT NULL,
						PRIMARY KEY (satellite_id)
					);`,
				},
			},
			{
				Description: "Split into multiple sqlite databases",
				Version:     18,
				Action: migrate.Func(func(log *zap.Logger, _ migrate.DB, tx *sql.Tx) error {
					// We keep database version information in the info.db but we migrate
					// the other tables into their own individual SQLite3 databases
					// and we drop them from the info.db.
					ctx := context.TODO()

					if err := sqliteutil.MigrateToDatabase(ctx, db.connections, db.sqliteDriverInstanceKey, VersionsDatabaseFilename, BandwidthDatabaseFilename, "bandwidth_usage", "bandwidth_usage_rollups"); err != nil {
						return ErrDatabase.Wrap(err)
					}
					if err := sqliteutil.MigrateToDatabase(ctx, db.connections, db.sqliteDriverInstanceKey, VersionsDatabaseFilename, OrdersDatabaseFilename, "unsent_order", "order_archive_"); err != nil {
						return ErrDatabase.Wrap(err)
					}
					if err := sqliteutil.MigrateToDatabase(ctx, db.connections, db.sqliteDriverInstanceKey, VersionsDatabaseFilename, PieceExpirationDatabaseFilename, "piece_expirations"); err != nil {
						return ErrDatabase.Wrap(err)
					}
					if err := sqliteutil.MigrateToDatabase(ctx, db.connections, db.sqliteDriverInstanceKey, VersionsDatabaseFilename, v0PieceInfoDatabaseFilename, "pieceinfo_"); err != nil {
						return ErrDatabase.Wrap(err)
					}
					if err := sqliteutil.MigrateToDatabase(ctx, db.connections, db.sqliteDriverInstanceKey, VersionsDatabaseFilename, PieceSpacedUsedDatabaseFilename, "piece_space_used"); err != nil {
						return ErrDatabase.Wrap(err)
					}
					if err := sqliteutil.MigrateToDatabase(ctx, db.connections, db.sqliteDriverInstanceKey, VersionsDatabaseFilename, ReputationDatabaseFilename, "reputation"); err != nil {
						return ErrDatabase.Wrap(err)
					}
					if err := sqliteutil.MigrateToDatabase(ctx, db.connections, db.sqliteDriverInstanceKey, VersionsDatabaseFilename, StorageUsageDatabaseFilename, "storage_usage"); err != nil {
						return ErrDatabase.Wrap(err)
					}
					if err := sqliteutil.MigrateToDatabase(ctx, db.connections, db.sqliteDriverInstanceKey, VersionsDatabaseFilename, UsedSerialsDatabaseFilename, "used_serial_"); err != nil {
						return ErrDatabase.Wrap(err)
					}

					// Create a list of tables we have migrated to new databases
					// that we can delete from the original database.
					tablesToDrop := []string{
						"bandwidth_usage",
						"bandwidth_usage_rollups",
						"certificate",
						"unsent_order",
						"order_archive_",
						"piece_expirations",
						"pieceinfo_",
						"piece_space_used",
						"reputation",
						"storage_usage",
						"used_serial_",
					}

					// Delete tables we have migrated from the original database.
					for _, tableName := range tablesToDrop {
						_, err := db.versionsDB.Exec("DROP TABLE "+tableName+";", nil)
						if err != nil {
							return ErrDatabase.Wrap(err)
						}
					}

					// VACUUM the versions database to reclaim the space used by the migrated dropped tables.
					_, err := db.versionsDB.Exec("VACUUM;")
					if err != nil {
						return ErrDatabase.Wrap(err)
					}

					// Closing the databases completes the reclaiming of the space used above in the vacuum call.
					err = db.closeDatabases()
					if err != nil {
						return ErrDatabase.Wrap(err)
					}

					// Re-open all the SQLite3 connections after executing the migration, VACUUM and close to reclaim space.
					err = db.openDatabases(filepath.Dir(db.versionsDB.location))
					if err != nil {
						return ErrDatabase.Wrap(err)
					}

					return nil
				}),
			},
		},
	}
}
