package internal

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/denisenkom/go-mssqldb"
	actionsv1alpha1 "github.com/pplavetzki/azure-sql-mi/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type MSSql struct {
	Server   string `json:"server"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`

	DB *sql.DB
}

func NewMSSql(server, user, password string, port int) *MSSql {
	return &MSSql{
		Server:   server,
		Port:     port,
		User:     user,
		Password: password,
	}
}

func (db *MSSql) FindDatabaseID(ctx context.Context, spec *actionsv1alpha1.Database) (*string, error) {
	_ = log.FromContext(ctx)
	logger := log.Log

	logger.Info("finding the database if it exists by Name", "name", spec.Spec.Name)
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
	stmt, err := db.DB.Prepare(fmt.Sprintf("SELECT DB_ID(N'%s') AS [ID];", spec.Spec.Name))
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	row := stmt.QueryRow()
	var id *string
	err = row.Scan(&id)
	if err != nil {
		return nil, err
	}
	return id, nil
}

func (db *MSSql) FindDatabaseName(ctx context.Context, id int) (*string, error) {
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
	defer db.DB.Close()
	err = db.DB.Ping()
	if err != nil {
		return nil, err
	}
	stmt, err := db.DB.Prepare(fmt.Sprintf("SELECT DB_NAME(%d) AS [Name];", id))
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	row := stmt.QueryRow()
	var name *string
	err = row.Scan(&name)
	if err != nil {
		return nil, err
	}
	return name, nil
}

func (db *MSSql) DeleteDatabase(ctx context.Context, spec *actionsv1alpha1.Database) error {
	_ = log.FromContext(ctx)
	logger := log.Log

	logger.Info("deleting the database", "name", spec.Spec.Name)
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
	result, err := db.DB.Query(fmt.Sprintf("SELECT DB_ID(N'%s') AS [ID];", spec.Spec.Name))
	if err != nil {
		return err
	}
	defer result.Close()
	result.Next()

	if err = result.Scan(&dbID); err == nil {
		rows, err := db.DB.Query(fmt.Sprintf("DROP DATABASE %s;", spec.Spec.Name))
		if err != nil {
			return err
		}
		defer rows.Close()
	} else {
		logger.Info("database doesn't exist returning nil")
	}

	return nil
}

func (db *MSSql) CreateDatabase(ctx context.Context, spec *actionsv1alpha1.Database) (*string, error) {
	_ = log.FromContext(ctx)
	logger := log.Log

	logger.Info("creating the database", "name", spec.Spec.Name)
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
	rows, err := db.DB.Query(fmt.Sprintf("CREATE DATABASE %s;", spec.Spec.Name))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	if cols == nil {
		return nil, nil
	}
	return db.FindDatabaseID(ctx, spec)

	// stmt, err := db.DB.Prepare(fmt.Sprintf("CREATE DATABASE %s;", spec.Name))
	// if err != nil {
	// 	return err
	// }
	// defer stmt.Close()

	// row := stmt.QueryRow()
	// var catalog string
	// var tableName string
	// err = row.Scan(&catalog, &tableName)
	// if err != nil {
	// 	return err
	// }

	// logger.Info("queried", "catalog", catalog, "tableName", tableName)
}
