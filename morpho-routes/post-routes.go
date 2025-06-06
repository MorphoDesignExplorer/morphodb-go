package morphoroutes

import (
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
)

// Every endpoint in this route set must be accessed with authentication only
// Every endpoint in this route set will invalidate the cache.

type PostProjectRequest struct {
	Metadata Metadata    `json:"metadata"`
	Models   SolutionSet `json:"models"`
	Project  Project     `json:"project"`
}

// Endpoint that takes a CSV with a project's solutions and uploads it to the database.
func PostProject(config Config) func(http.ResponseWriter, *http.Request) {
	reportError := func(err error, writer http.ResponseWriter, communicate bool) {
		if communicate {
			HandleErrorWithMessage(writer, err)
		} else {
			HandleError(writer)
		}
	}
	return func(writer http.ResponseWriter, request *http.Request) {
		var data PostProjectRequest
		dec := json.NewDecoder(request.Body)
		err := dec.Decode(&data)
		if err != nil {
			LogError(err)
			reportError(err, writer, false)
			return
		}

		db, err := StartConn(config)
		if err != nil {
			LogError(err)
			reportError(err, writer, false)
			return
		}

		tx, err := db.Begin()
		if err != nil {
			LogError(err)
			reportError(err, writer, false)
			return
		}
		defer tx.Rollback()

		// save the project first
		if err = data.Project.Create(tx); err != nil {
			LogError(err)
			reportError(err, writer, true)
			return
		}

		// then save the solutions
		if err = data.Models.Create(tx, data.Project.ProjectName); err != nil {
			LogError(err)
			reportError(err, writer, true)
			return
		}

		// finally save the metadata
		if err = data.Metadata.Create(tx, data.Project.ProjectName); err != nil {
			LogError(err)
			reportError(err, writer, true)
			return
		}

		if err = tx.Commit(); err != nil {
			LogError(err)
			reportError(err, writer, true)
		}

		responseBytes := fmt.Appendf([]byte{}, fmt.Sprintf("%s was uploaded successfully.", data.Project.ProjectName))

		SuccessfulResponse(writer, request, &responseBytes)

		GlobalCache.Invalidate(request.URL.Path) // POST requests to this URI invalidate the cache.
	}
}

type PostAssetRequest struct {
	Asset Asset `json:"asset"`
}

// Endpoint that uploads one or more images with attached tags for a particular solution, to S3.
func PostAsset(config Config) func(writer http.ResponseWriter, request *http.Request) {
	return func(writer http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(writer, request.Body, 31<<20) // Total of 31MB allowed for the whole body
		err := request.ParseMultipartForm(30 << 20)                      // Allow 30MB of uploads
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}

		db, err := StartConn(config)
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}

		solutionIds, ok := request.MultipartForm.Value["solution_id"]
		if !ok || len(solutionIds) != 1 {
			err := fmt.Errorf("No solution ID specified.")
			LogError(err)
			HandleErrorWithMessage(writer, err)
			return
		}
		solutionId := solutionIds[0]

		var projectAssets ProjectAssetFields
		assetRow := db.QueryRow("select assets from project where project_name = (select project_name from solution where id = ?)", solutionId)
		err = assetRow.Scan(&projectAssets)
		if err != nil {
			err := fmt.Errorf("Invalid solution ID.")
			LogError(err)
			HandleErrorWithMessage(writer, err)
			return
		}

		var scopedId int
		scopedIdRow := db.QueryRow("select scoped_id from solution where id = ?", solutionId)
		err = scopedIdRow.Scan(&scopedId)
		if err != nil {
			err := fmt.Errorf("Invalid solution ID.")
			LogError(err)
			HandleErrorWithMessage(writer, err)
			return
		}

		files := make(map[string]*multipart.FileHeader)

		// validate that there's only one file uploaded per tag
		for tag, handle := range request.MultipartForm.File {
			// fmt.Println(tag, handle)
			if len(handle) > 1 {
				err := fmt.Errorf("tag %s had more than one files uploaded to it.", tag)
				LogError(err)
				HandleErrorWithMessage(writer, err)
				return
			} else if len(handle) == 0 {
				err := fmt.Errorf("tag %s had no files uploaded to it.", tag)
				LogError(err)
				HandleErrorWithMessage(writer, err)
				return
			}
			files[tag] = handle[0]
		}

		err = CreateAssets(db, projectAssets, solutionId, scopedId, files)
		if err != nil {
			LogError(err)
			HandleErrorWithMessage(writer, err)
			return
		}

		SuccessfulResponse(writer, request, &[]byte{})

		GlobalCache.Invalidate(request.URL.Path) // POST requests to this endpoint invalidate the cache.
	}
}
