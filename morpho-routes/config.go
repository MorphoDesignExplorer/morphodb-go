package morphoroutes

import (
	"errors"
	"os"
)

type Config struct {
	DB_STRING               string
	AWS_REGION              string
	AWS_STORAGE_BUCKET_NAME string
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

	return result, nil
}
