package morphoroutes

import (
	"errors"
	"os"
)

type Config struct {
	DB_STRING               string
	AWS_S3_ENDPOINT_URL     string
	AWS_STORAGE_BUCKET_NAME string
}

func GetConfig() (Config, error) {
	result := Config{}
	result.DB_STRING = os.Getenv("DB_STRING")
	if len(result.DB_STRING) == 0 {
		return result, errors.New("DB_STRING was not set")
	}

	result.AWS_S3_ENDPOINT_URL = os.Getenv("AWS_S3_ENDPOINT_URL")
	if len(result.DB_STRING) == 0 {
		return result, errors.New("AWS_S3_ENDPOINT_URL was not set")
	}

	result.AWS_STORAGE_BUCKET_NAME = os.Getenv("AWS_STORAGE_BUCKET_NAME")
	if len(result.DB_STRING) == 0 {
		return result, errors.New("AWS_STORAGE_BUCKET_NAME was not set")
	}

	return result, nil
}
