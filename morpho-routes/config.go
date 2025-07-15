package morphoroutes

import (
	"database/sql"
	"errors"
	"os"
)

type Config struct {
	DB_STRING               string // where is the SQLite database?
	AWS_REGION              string // what is the aws region we're running on?
	AWS_STORAGE_BUCKET_NAME string // what is the name of the storage bucket?
	ENVIRONMENT             string // are we running this server on production? (either prod or dev)
}

func (c *Config) GetDB() (*sql.DB, error) {
	return StartConn(*c)
}

func GetConfig() (Config, error) {
	result := Config{}
	result.DB_STRING = os.Getenv("DB_STRING")
	if len(result.DB_STRING) == 0 {
		return result, errors.New("DB_STRING was not set")
	}

	result.AWS_REGION = os.Getenv("AWS_REGION")
	if len(result.AWS_REGION) == 0 {
		return result, errors.New("AWS_REGION was not set")
	}

	result.AWS_STORAGE_BUCKET_NAME = os.Getenv("AWS_STORAGE_BUCKET_NAME")
	if len(result.AWS_STORAGE_BUCKET_NAME) == 0 {
		return result, errors.New("AWS_STORAGE_BUCKET_NAME was not set")
	}

	result.ENVIRONMENT = os.Getenv("ENVIRONMENT")
	if len(result.ENVIRONMENT) == 0 {
		return result, errors.New("ENVIRONMENT was not set")
	} else if result.ENVIRONMENT != "prod" && result.ENVIRONMENT != "dev" {
		return result, errors.New("ENVIRONMENT can only have the following values: prod, dev")
	}

	return result, nil
}
