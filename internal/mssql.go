package internal

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/denisenkom/go-mssqldb"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// MSSql interaction with sql server
type MSSql struct {
	Server   string `json:"server"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`

	DB *sql.DB
}

type DatabaseParams struct {
	CollationName string
}

// NewMSSql contructor pattern
func NewMSSql(server, user, password string, port int) *MSSql {
	return &MSSql{
		Server:   server,
		Port:     port,
		User:     user,
		Password: password,
	}
}

// FindDatabaseID finds the db id
func (db *MSSql) FindDatabaseID(ctx context.Context, databaseName string) (*string, error) {
	_ = log.FromContext(ctx)
	logger := log.Log

	logger.Info("finding the database if it exists by Name", "name", databaseName)
	// Build connection string
	connString := fmt.Sprintf("server=%s;user id=%s;password=%s;port=%d", db.Server, db.User, db.Password, db.Port)

	var err error

	// Create connection pool
	db.DB, err = sql.Open("sqlserver", connString)
	if err != nil {
		return nil, err
	}
	err = db.DB.Ping()
	if err != nil {
		return nil, err
	}
	sqlStmt := "SELECT CAST(recovery_fork_guid AS char(36)) as recovery_fork_guid FROM sys.database_recovery_status drs JOIN sys.databases dbs ON drs.database_id = dbs.database_id WHERE dbs.[name] = '%s'"

	stmt, err := db.DB.Prepare(fmt.Sprintf(sqlStmt, databaseName))
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	row := stmt.QueryRow()
	var id string
	err = row.Scan(&id)
	// sql: no rows in result set
	if err != nil {
		if strings.Contains(err.Error(), "sql: no rows in result set") {
			return nil, nil
		}
		return nil, err
	}

	return &id, nil
}

func (db *MSSql) FindDatabaseName(ctx context.Context, id string) (*string, error) {
	_ = log.FromContext(ctx)
	logger := log.Log

	logger.Info("finding the database if it exists by ID", "id", id)
	// Build connection string
	connString := fmt.Sprintf("server=%s;user id=%s;password=%s;port=%d", db.Server, db.User, db.Password, db.Port)

	var err error

	// Create connection pool
	db.DB, err = sql.Open("sqlserver", connString)
	if err != nil {
		return nil, err
	}
	err = db.DB.Ping()
	if err != nil {
		return nil, err
	}
	sqlStmt := "select dbs.[name] FROM sys.database_recovery_status drs JOIN sys.databases dbs ON drs.database_id = dbs.database_id where drs.recovery_fork_guid = '%s'"

	stmt, err := db.DB.Prepare(fmt.Sprintf(sqlStmt, id))
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	row := stmt.QueryRow()
	var name string
	err = row.Scan(&name)
	if err != nil && !strings.Contains(err.Error(), "zero") {
		return nil, err
	}
	return &name, nil
}

func (db *MSSql) DeleteDatabase(ctx context.Context, databaseName string) error {
	_ = log.FromContext(ctx)
	logger := log.Log

	logger.Info("deleting the database", "name", databaseName)
	// Build connection string
	connString := fmt.Sprintf("server=%s;user id=%s;password=%s;port=%d", db.Server, db.User, db.Password, db.Port)

	var err error

	// Create connection pool
	db.DB, err = sql.Open("sqlserver", connString)
	if err != nil {
		return err
	}
	defer db.DB.Close()
	err = db.DB.Ping()
	if err != nil {
		return err
	}
	var dbID int64
	result, err := db.DB.Query(fmt.Sprintf("SELECT DB_ID(N'%s') AS [ID];", databaseName))
	if err != nil {
		return err
	}
	defer result.Close()
	result.Next()

	if err = result.Scan(&dbID); err == nil {
		rows, err := db.DB.Query(fmt.Sprintf("DROP DATABASE %s;", databaseName))
		if err != nil {
			return err
		}
		defer rows.Close()
	} else {
		logger.Info("database doesn't exist returning nil")
	}

	return nil
}

func (db *MSSql) CreateDatabase(ctx context.Context, databaseName string, params *DatabaseParams) (*string, error) {
	_ = log.FromContext(ctx)
	logger := log.Log

	logger.Info("creating the database", "name", databaseName)
	// Build connection string
	connString := fmt.Sprintf("server=%s;user id=%s;password=%s;port=%d", db.Server, db.User, db.Password, db.Port)

	var err error

	// Create connection pool
	db.DB, err = sql.Open("sqlserver", connString)
	if err != nil {
		return nil, err
	}
	defer db.DB.Close()
	err = db.DB.Ping()
	if err != nil {
		return nil, err
	}
	_, err = db.DB.Exec(buildDatabaseSQL("CREATE", databaseName, params))
	if err != nil {
		return nil, err
	}
	return db.FindDatabaseID(ctx, databaseName)
}

func (db *MSSql) AlterDatabase(ctx context.Context, databaseName string, params *DatabaseParams) error {
	_ = log.FromContext(ctx)
	logger := log.Log

	logger.Info("altering the database", "name", databaseName)
	// Build connection string
	connString := fmt.Sprintf("server=%s;user id=%s;password=%s;port=%d", db.Server, db.User, db.Password, db.Port)

	var err error

	// Create connection pool
	db.DB, err = sql.Open("sqlserver", connString)
	if err != nil {
		return err
	}
	defer db.DB.Close()
	err = db.DB.Ping()
	if err != nil {
		return err
	}

	sql := buildDatabaseSQL("ALTER", databaseName, params)
	if len(sql) > 0 {
		_, err = db.DB.Exec(sql)
		if err != nil {
			return err
		}
	} else {
		logger.Info("nothing to change, not altering database", "name", databaseName)
	}

	return nil
}

func buildDatabaseSQL(verb string, databaseName string, params *DatabaseParams) string {
	var b strings.Builder
	var count int8 = 0

	fmt.Fprintf(&b, "%s DATABASE %s ", verb, databaseName)

	if params.CollationName != "" {
		fmt.Fprintf(&b, "Collate %s", params.CollationName)
		count++
	}
	if verb == "ALTER" && count == 0 {
		return ""
	}
	return b.String()
}
