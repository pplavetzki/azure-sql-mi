package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/go-logr/logr"
	ms "github.com/pplavetzki/azure-sql-mi/internal"
)

var (
	logger logr.Logger
)

type DBResult struct {
	Result *string
	Error  error
}

func getEnvOrFail(key string) string {
	var val string
	if val = os.Getenv(key); val == "" {
		panic(fmt.Errorf("failed to get required env variable: %s", key))
	}
	return val
}

func getDatabaseID(ctx context.Context, msSQL *ms.MSSql, name string, result chan *DBResult) {
	dbID, err := msSQL.FindDatabaseID(ctx, name)
	result <- &DBResult{
		Result: dbID,
		Error:  err,
	}
}

func getDatabaseName(ctx context.Context, msSQL *ms.MSSql, id string, result chan *DBResult) {
	if id != "" {
		dbName, err := msSQL.FindDatabaseName(ctx, id)
		result <- &DBResult{
			Result: dbName,
			Error:  err,
		}
	} else {
		result <- &DBResult{
			Result: nil,
			Error:  nil,
		}
	}
}

func performSync(msSQL *ms.MSSql, databaseID, databaseName string) {
	dbNameResult := make(chan *DBResult)
	dbIDResult := make(chan *DBResult)

	defer func() {
		if msSQL.DB != nil {
			msSQL.DB.Close()
		}
	}()

	go getDatabaseID(context.TODO(), msSQL, databaseName, dbIDResult)
	go getDatabaseName(context.TODO(), msSQL, databaseID, dbNameResult)

	dbNameR := <-dbNameResult
	dbIDR := <-dbIDResult

	if dbNameR.Error != nil {
		panic(fmt.Errorf("failed to query name: %v", dbNameR.Error))
	}
	if dbIDR.Error != nil {
		panic(fmt.Errorf("failed to query db id: %v", dbIDR.Error))
	}
	// if dbIDR.Result != nil {
	// 	logger.V(1).Info("found database ID", "database-id", *dbIDR.Result)
	// }
	// if dbNameR.Result != nil {
	// 	logger.V(1).Info("database name", "database-name", *dbNameR.Result)
	// }
	// Need to do some serious logic here
	if databaseID == "" && dbIDR.Result == nil {
		_, err := msSQL.CreateDatabase(context.TODO(), databaseName, &ms.DatabaseParams{})
		if err != nil {
			panic(err)
		}
		// if dbID != nil {
		// 	logger.Info("successfully created a new database", "database-id", *dbID)
		// } else {
		// 	logger.Info("successfully created a new database, but no database-id")
		// }
	} else if databaseID == "" && dbIDR.Result != nil {
		panic(fmt.Errorf("out of sync -- database name: %s, database guid: %s found, exists on server but not managed by the controller", databaseName, *dbIDR.Result))
	} else if databaseID != "" && (dbIDR.Result != nil && *dbIDR.Result != databaseID) {
		panic(fmt.Errorf("out of sync -- database guid: %s does not match what controller database guid is expecting: %s", *dbIDR.Result, databaseID))
	}

	// dbID, err = msSQL.CreateDatabase(context.TODO(), databaseName, &ms.DatabaseParams{})
	// if err != nil {
	// 	panic(err)
	// }
	// if dbID != nil {
	// 	fmt.Println(*dbID)
	// }
}

func main() {
	// zapLog, err := zap.NewDevelopment()
	// if err != nil {
	// 	panic(fmt.Sprintf("failed starting logger (%v)?", err))
	// }
	// logger = zapr.NewLogger(zapLog)

	server := getEnvOrFail("MS_SERVER")
	user := getEnvOrFail("DB_USER")
	password := getEnvOrFail("DB_PASSWORD")
	portVal := getEnvOrFail("DB_PORT")
	databaseName := getEnvOrFail("DB_NAME")
	databaseID := os.Getenv("DB_ID")

	port, err := strconv.Atoi(portVal)
	if err != nil {
		panic(fmt.Errorf("failed to convert port value to int: %s", portVal))
	}

	mqSQL := ms.NewMSSql(server, user, password, port)
	performSync(mqSQL, databaseID, databaseName)
}
