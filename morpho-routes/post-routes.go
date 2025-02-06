package morphoroutes

import (
	"net/http"

	schema "github.com/gorilla/schema"
)

// Every endpoint in this route set must be accessed with authentication only

// Endpoint that takes a CSV with a project's solutions and uploads it to the database.
func ImportCSV(writer http.ResponseWriter, request *http.Request) {
	// request should have a csv file
	// check file header and extension
	// check file for malware (?)
	// check if file follows format
	// upload file contents to db
}

type ImageForm struct {
	Tag  string
	File []byte
}

// Endpoint that uploads one or more images with attached tags for a particular solution, to S3.
func UploadAsset(writer http.ResponseWriter, request *http.Request) {
	// request should hold a form with tags mapping to image files
	// check file header and extension
	// check file for malware
	// upload file contents to s3
	// update s3 url to the db
	var form ImageForm
	decoder := schema.NewDecoder()
	err := decoder.Decode(&form, request.PostForm)
	if err != nil {
		LogError(err)
		HandleError(writer)
		return
	}
}
