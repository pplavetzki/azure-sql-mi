package internal

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
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
	Collation                  string
	AllowSnapshotIsolation     bool
	AllowReadCommittedSnapshot bool
	Parameterization           string
	CompatibilityLevel         int
}

type AlterParams struct {
	AllowSnapshotIsolation     *bool
	AllowReadCommittedSnapshot *bool
	Parameterization           *string
	CompatibilityLevel         *int
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

type DatabaseSync struct {
	Database []struct {
		Name                       string `json:"name"`
		State                      int    `json:"state"`
		IsReadOnly                 bool   `json:"isReadOnly"`
		UserAccess                 int    `json:"userAccess"`
		CreateDate                 string `json:"createDate"`
		CompatibilityLevel         int    `json:"compatibilityLevel"`
		Collation                  string `json:"collation"`
		AllowSnapshotIsolation     string `json:"allowSnapshotIsolation"`
		AllowReadCommittedSnapshot string `json:"allowReadCommittedSnapshot"`
		Parameterization           string `json:"parameterization"`
	} `json:"database"`
}

type DatabaseConfig struct {
	DatabaseName               string
	DatabaseID                 string
	State                      int
	IsReadOnly                 bool
	UserAccess                 int
	CompatibilityLevel         int
	Collation                  string
	AllowSnapshotIsolation     bool
	AllowReadCommittedSnapshot bool
	Parameterization           string
}

type SyncResponse struct {
	CompatibilityLevel         *int
	AllowSnapshotIsolation     *bool
	Parameterization           *string
	AllowReadCommittedSnapshot *bool
}

func Safe(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (db *MSSql) SyncNeeded(ctx context.Context, params *DatabaseConfig) (*SyncResponse, error) {
	_ = log.FromContext(ctx)
	logger := log.Log

	logger.V(1).Info("determine syncing", "sync-params", params)

	syncResponse := &SyncResponse{}

	// Build connection string
	connString := fmt.Sprintf("server=%s;user id=%s;password=%s;port=%d", db.Server, db.User, db.Password, db.Port)

	var err error

	if params.DatabaseID != "" {
		dn, err := db.FindDatabaseName(ctx, params.DatabaseID)
		if err != nil {
			return nil, err
		}
		if Safe(dn) != params.DatabaseName {
			return nil, fmt.Errorf("database name: %s does not match the name does not match the expected name %s", Safe(dn), params.DatabaseName)
		}
	}

	// Create connection pool
	db.DB, err = sql.Open("sqlserver", connString)
	if err != nil {
		return nil, err
	}
	err = db.DB.Ping()
	if err != nil {
		return nil, err
	}
	sqlStmt := "SELECT [name], " +
		"[state], " +
		"[is_read_only] as [isReadOnly], " +
		"[user_access] as [userAccess], " +
		"[create_date] as [createDate], " +
		"[compatibility_level] as [compatibilityLevel], " +
		"[collation_name] as [collation], " +
		"IIF(snapshot_isolation_state = 1 or snapshot_isolation_state = 3, 'true', 'false') as [allowSnapshotIsolation], " +
		"IIF(is_read_committed_snapshot_on = 1, 'true', 'false') as [allowReadCommittedSnapshot], " +
		"IIF(is_parameterization_forced = 0, 'simple', 'forced' ) as [parameterization] " +
		"FROM sys.databases " +
		"WHERE [name] = '%s' " +
		"FOR JSON PATH, ROOT ('database')"

	stmt, err := db.DB.Prepare(fmt.Sprintf(sqlStmt, params.DatabaseName))
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	row := stmt.QueryRow()
	var output string
	err = row.Scan(&output)
	// sql: no rows in result set
	if err != nil {
		if strings.Contains(err.Error(), "sql: no rows in result set") {
			return nil, nil
		}
		return nil, err
	}
	var sync DatabaseSync
	err = json.Unmarshal([]byte(output), &sync)
	if err != nil {
		return nil, err
	}

	/***************************************************************************************************************************
	* Perform the validation for syncing logic
	***************************************************************************************************************************/
	// allowReadCommittedSnapshot, _ := strconv.ParseBool(sync.Database[0].AllowReadCommittedSnapshot)
	allowSnapshotIsolation, _ := strconv.ParseBool(sync.Database[0].AllowSnapshotIsolation)
	requireSync := false

	// if params.AllowReadCommittedSnapshot != allowReadCommittedSnapshot {
	// 	syncResponse.AllowReadCommittedSnapshot = &params.AllowReadCommittedSnapshot
	// 	requireSync = true
	// }
	if params.AllowSnapshotIsolation != allowSnapshotIsolation {
		syncResponse.AllowSnapshotIsolation = &params.AllowSnapshotIsolation
		requireSync = true
	}
	if params.CompatibilityLevel != sync.Database[0].CompatibilityLevel {
		syncResponse.CompatibilityLevel = &params.CompatibilityLevel
		requireSync = true
	}
	if params.Parameterization != sync.Database[0].Parameterization {
		syncResponse.Parameterization = &params.Parameterization
		requireSync = true
	}
	/**************************************************************************************************************************/
	if requireSync {
		return syncResponse, nil
	}

	return nil, nil
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
	if err != nil {
		if strings.Contains(err.Error(), "sql: no rows in result set") {
			return nil, nil
		}
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

func (db *MSSql) AlterDatabase(ctx context.Context, databaseName string, params *AlterParams) error {
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

	altStatements := buildAlterSQL(databaseName, params)
	errors := []error{}
	if len(altStatements) > 0 {
		for _, alter := range altStatements {
			_, err = db.DB.Exec(alter)
			if err != nil {
				logger.V(0).Info(err.Error())
				errors = append(errors, err)
			}
		}
	}
	if len(errors) > 0 {
		return fmt.Errorf("errors while running alter on database: %s", databaseName)
	}

	return nil
}

func onOff(value bool) string {
	if value {
		return "ON"
	} else {
		return "OFF"
	}
}

func buildAlterSQL(databaseName string, params *AlterParams) []string {
	altStatements := []string{}
	altTemplate := fmt.Sprintf("Alter DATABASE %s ", databaseName)

	if params.Parameterization != nil && *params.Parameterization != "" {
		altStatements = append(altStatements, fmt.Sprintf("%s SET PARAMETERIZATION %s;", altTemplate, *params.Parameterization))
	}
	// if params.AllowReadCommittedSnapshot != nil {
	// 	altStatements = append(altStatements, fmt.Sprintf("%s SET READ_COMMITTED_SNAPSHOT %s;", altTemplate, onOff(*params.AllowReadCommittedSnapshot)))
	// }
	if params.AllowSnapshotIsolation != nil {
		altStatements = append(altStatements, fmt.Sprintf("%s SET ALLOW_SNAPSHOT_ISOLATION %s;", altTemplate, onOff(*params.AllowSnapshotIsolation)))
	}
	if params.CompatibilityLevel != nil {
		altStatements = append(altStatements, fmt.Sprintf("%s SET COMPATIBILITY_LEVEL = %d;", altTemplate, *params.CompatibilityLevel))
	}
	return altStatements
}

func buildDatabaseSQL(verb string, databaseName string, params *DatabaseParams) string {
	var b strings.Builder
	var count int8 = 0

	fmt.Fprintf(&b, "%s DATABASE %s ", verb, databaseName)

	if params.Collation != "" {
		fmt.Fprintf(&b, "Collate %s", params.Collation)
		count++
	}
	if verb == "ALTER" && count == 0 {
		return ""
	}
	return b.String()
}
