package morphoroutes

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
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

func PostProjectZip(config Config) func(http.ResponseWriter, *http.Request) {
	reportError := func(err error, writer http.ResponseWriter, communicate bool) {
		if communicate {
			HandleErrorWithMessage(writer, err)
		} else {
			HandleError(writer)
		}
	}
	return func(writer http.ResponseWriter, request *http.Request) {
		err := UploadProject(config)
		if err != nil {
			LogError(err)
			reportError(err, writer, true)
		}

		response := []byte("ok")
		SuccessfulResponse(writer, request, &response)
		GlobalCache.InvalidateAll()
	}
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

		GlobalCache.InvalidateAll()

		// GlobalCache.Invalidate(request.URL.Path) // POST requests to this URI invalidate the cache.
	}
}

type UpdateProjectMetadataRequest struct {
	VariableMetadataUnits *map[string]string `json:"variable_metadata_units"`
	OutputMetadataUnits   *map[string]string `json:"output_metadata_units"`
	AssetDescriptions     *map[string]string `json:"asset_descriptions"`
	Captions              *[]Caption         `json:"captions"`
	ProjectDescription    *string            `json:"project_description"`
	ProjectName           *string            `json:"project_name"`
	HumanName             *string            `json:"human_name"`
}

func UpdateProjectMetadata(config Config) func(writer http.ResponseWriter, request *http.Request) {
	return func(writer http.ResponseWriter, request *http.Request) {
		db, err := StartConn(config)
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}

		var upmReq UpdateProjectMetadataRequest
		dec := json.NewDecoder(request.Body)
		err = dec.Decode(&upmReq)
		if err != nil {
			LogError(err)
			HandleErrorWithMessage(writer, fmt.Errorf("Invalid JSON. Recheck request and JSON format."))
			return
		}

		if upmReq.ProjectName == nil {
			HandleErrorWithMessage(writer, fmt.Errorf("No project_name specified."))
			return
		}

		project, err := GetProject(db, *upmReq.ProjectName)
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}

		metadata, err := GetMetadata(db, *upmReq.ProjectName)
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}

		changed := false
		changedMetadata := false

		if upmReq.VariableMetadataUnits != nil {
			for idx, field := range project.VariableMetadata {
				if unit, ok := (*upmReq.VariableMetadataUnits)[field.FieldName]; ok {
					project.VariableMetadata[idx].FieldUnit = unit
					changed = true
				}
			}
		}

		if upmReq.OutputMetadataUnits != nil {
			for idx, field := range project.OutputMetadata {
				if unit, ok := (*upmReq.OutputMetadataUnits)[field.FieldName]; ok {
					project.OutputMetadata[idx].FieldUnit = unit
					changed = true
				}
			}
		}

		if upmReq.AssetDescriptions != nil {
			for idx, field := range project.Assets {
				if description, ok := (*upmReq.AssetDescriptions)[field.Tag]; ok {
					project.Assets[idx].Description = description
					changed = true
				}
			}
		}

		if upmReq.HumanName != nil {
			metadata.HumanName = *upmReq.HumanName
			changedMetadata = true
		}

		if upmReq.ProjectDescription != nil {
			metadata.Description.Text = *upmReq.ProjectDescription
			changedMetadata = true
		}

		if upmReq.Captions != nil {
			metadata.Captions = *upmReq.Captions
			changedMetadata = true
		}

		tx, err := db.Begin()
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}
		defer tx.Rollback()

		if changed {
			project.Update(tx)
		}

		if changedMetadata {
			metadata.Update(tx, project.ProjectName)
		}

		err = tx.Commit()
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}

		msg := []byte("updated project successfully.")
		SuccessfulResponse(writer, request, &msg)

		GlobalCache.InvalidateAll()
		// GlobalCache.Invalidate(request.URL.Path)
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

type PutDocumentRequest struct {
	Text   *string `json:"text"`
	Title  *string `json:"title"`
	Parent *string `json:"parent"`
}

func PutDocument(config Config) func(writer http.ResponseWriter, request *http.Request) {
	return func(writer http.ResponseWriter, request *http.Request) {
		variables := mux.Vars(request)

		var docReq PutDocumentRequest
		dec := json.NewDecoder(request.Body)
		err := dec.Decode(&docReq)
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}

		idOrSlug, ok := variables["idOrSlug"]
		if !ok {
			HandleErrorWithMessage(writer, fmt.Errorf("Id or Slug was empty."))
			return
		}

		if docReq.Text == nil {
			HandleErrorWithMessage(writer, fmt.Errorf("No content was provided to update the document with."))
			return
		}

		if docReq.Title == nil {
			HandleErrorWithMessage(writer, fmt.Errorf("No title was provided for the document."))
			return
		}

		if docReq.Parent == nil {
			emptyString := ""
			docReq.Parent = &emptyString
		}

		db, err := StartConn(config)
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}

		tx, err := db.Begin()
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}

		doc, err := GetDocument(tx, idOrSlug)
		if err != nil {
			LogError(err)
			HandleErrorWithMessage(writer, fmt.Errorf("Could not find the document %s", idOrSlug))
		}

		err = doc.Update(tx, *docReq.Text, *docReq.Title, *docReq.Parent)
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}

		err = tx.Commit()
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}
	}
}

type PostDocumentRequest struct {
	Slug   *string `json:"slug"`
	Text   *string `json:"text"`
	Title  *string `json:"title"`
	Parent *string `json:"parent"`
}

func PostDocument(config Config) func(writer http.ResponseWriter, request *http.Request) {
	return func(writer http.ResponseWriter, request *http.Request) {
		var docReq PostDocumentRequest
		dec := json.NewDecoder(request.Body)
		err := dec.Decode(&docReq)
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}

		if docReq.Slug == nil {
			HandleErrorWithMessage(writer, fmt.Errorf("Slug is empty."))
			return
		}

		if docReq.Text == nil {
			HandleErrorWithMessage(writer, fmt.Errorf("No content was provided to update the document with."))
			return
		}

		if docReq.Title == nil {
			HandleErrorWithMessage(writer, fmt.Errorf("No title was provided for the document."))
			return
		}

		if docReq.Parent == nil {
			emptyString := ""
			docReq.Parent = &emptyString
		}

		db, err := StartConn(config)
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}

		tx, err := db.Begin()
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}

		doc := Document{}

		err = doc.Create(tx, *docReq.Slug, *docReq.Text, *docReq.Title, *docReq.Parent)
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}

		err = tx.Commit()
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}
	}
}

func DeleteDocument(config Config) func(writer http.ResponseWriter, request *http.Request) {
	return func(writer http.ResponseWriter, request *http.Request) {
		variables := mux.Vars(request)
		idOrSlug, ok := variables["idOrSlug"]
		if !ok {
			HandleErrorWithMessage(writer, fmt.Errorf("Id or Slug was empty."))
			return
		}

		if idOrSlug == "Front Matter" {
			HandleErrorWithMessage(writer, fmt.Errorf("Cannot delete front matter."))
			return
		}

		db, err := StartConn(config)
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}

		tx, err := db.Begin()
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}

		doc, err := GetDocument(tx, idOrSlug)
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}

		err = doc.Delete(tx)
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}

		err = tx.Commit()
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}
	}
}
