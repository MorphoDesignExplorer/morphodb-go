package morphoroutes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"

	"github.com/gorilla/mux"
)

// Every endpoint in this route set must be accessed with authentication only
// Every endpoint in this route set will invalidate the cache.

// Endpoint that takes a zip File with a project's solutions and uploads it to the database.
//
// Returns an Endpoint object.
func (service Service) PostProjectZip() *Endpoint {

	type PostProjectZipResponse struct {
		Message     string `json:"message"`
		ProjectName string `json:"project_name"`
	}

	type PostProjectZipRequest struct {
		S3Uri *string `json:"s3uri"`
	}

	return NewEndpoint(func(writer http.ResponseWriter, request *http.Request) error {
		reqObject := PostProjectZipRequest{}
		decoder := json.NewDecoder(request.Body)
		if err := decoder.Decode(&reqObject); err != nil {
			return err
		}

		if reqObject.S3Uri == nil {
			err := fmt.Errorf("s3Uri not present in request's JSON body.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		fmt.Println(path.Join(service.S3_MOUNTPOINT, *reqObject.S3Uri))

		projectName, err := UploadProject(service, *reqObject.S3Uri)
		if err != nil {
			return err
		}

		if err = SuccessfulResponseJson(writer, request, PostProjectZipResponse{Message: "Uploaded project successfully.", ProjectName: projectName}); err != nil {
			return APIError{http.StatusInternalServerError, JSON_MARSHAL_ERROR, NewServerError(err)}
		}

		GlobalCache.InvalidateAll()

		return nil
	})
}

type PostProjectRequest struct {
	Metadata Metadata    `json:"metadata"`
	Models   SolutionSet `json:"models"`
	Project  Project     `json:"project"`
}

func (service Service) DeleteProjectEndpoint() *Endpoint {
	return NewEndpoint(func(writer http.ResponseWriter, request *http.Request) error {
		variables := mux.Vars(request)

		projectName, ok := variables["project"]
		if !ok {
			err := fmt.Errorf("Could not find project to delete.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		db, err := service.GetDB()
		if err != nil {
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
		}

		tx, err := db.Begin()
		if err != nil {
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
		}
		defer tx.Rollback()

		project, err := GetProject(db, projectName)
		if err != nil {
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
		}

		err = project.Delete(tx)
		if err != nil {
			return APIError{http.StatusServiceUnavailable, WRITE_DB_ERROR, NewServerError(err)}
		}

		if err = tx.Commit(); err != nil {
			return APIError{http.StatusServiceUnavailable, WRITE_DB_ERROR, NewServerError(err)}
		}

		if err = SuccessfulResponseJson(writer, request, APIMessage{"Deleted " + projectName + " successfully."}); err != nil {
			return APIError{http.StatusServiceUnavailable, JSON_MARSHAL_ERROR, NewServerError(err)}
		}

		GlobalCache.InvalidateAll()

		return nil
	})
}

// Endpoint that takes a CSV with a project's solutions and uploads it to the database.
//
// Returns an Endpoint object.
func (service Service) PostProjectEndpoint() *Endpoint {
	return NewEndpoint(func(writer http.ResponseWriter, request *http.Request) error {
		var data PostProjectRequest
		dec := json.NewDecoder(request.Body)
		err := dec.Decode(&data)
		if err != nil {
			return APIError{http.StatusBadRequest, JSON_UNMARSHAL_ERROR, NewServerError(err)}
		}

		db, err := service.GetDB()
		if err != nil {
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
		}

		tx, err := db.Begin()
		if err != nil {
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
		}
		defer tx.Rollback()

		// save the project first
		if err = data.Project.Create(tx); err != nil {
			return APIError{http.StatusServiceUnavailable, WRITE_DB_ERROR, NewServerError(err)}
		}

		// then save the solutions
		if err = data.Models.Create(tx, data.Project.ProjectName); err != nil {
			return APIError{http.StatusServiceUnavailable, WRITE_DB_ERROR, NewServerError(err)}
		}

		// finally save the metadata
		if err = data.Metadata.Create(tx, data.Project.ProjectName); err != nil {
			return APIError{http.StatusServiceUnavailable, WRITE_DB_ERROR, NewServerError(err)}
		}

		if err = tx.Commit(); err != nil {
			return APIError{http.StatusServiceUnavailable, WRITE_DB_ERROR, NewServerError(err)}
		}

		responseBytes := fmt.Appendf([]byte{}, fmt.Sprintf("%s was uploaded successfully.", data.Project.ProjectName))

		SuccessfulResponse(writer, request, responseBytes)

		GlobalCache.InvalidateAll()

		GlobalCache.Invalidate(request.URL.Path) // POST requests to this URI invalidate the cache.
		return nil
	})
}

func (service Service) UpdateProjectMetadataEndpoint() *Endpoint {

	type UpdateProjectMetadataRequest struct {
		VariableMetadataUnits *map[string]string `json:"variable_metadata_units"`
		OutputMetadataUnits   *map[string]string `json:"output_metadata_units"`
		AssetDescriptions     *map[string]string `json:"asset_descriptions"`
		Captions              *[]Caption         `json:"captions"`
		ProjectDescription    *string            `json:"project_description"`
		ProjectName           *string            `json:"project_name"`
		HumanName             *string            `json:"human_name"`
	}

	return NewEndpoint(func(writer http.ResponseWriter, request *http.Request) error {
		db, err := StartConn(service)
		if err != nil {
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
		}

		var upmReq UpdateProjectMetadataRequest
		dec := json.NewDecoder(request.Body)
		err = dec.Decode(&upmReq)
		if err != nil {
			err := fmt.Errorf(JSON_UNMARSHAL_ERROR)
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		if upmReq.ProjectName == nil {
			err := fmt.Errorf("No project name specified.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		if upmReq.HumanName != nil && len(*upmReq.HumanName) == 0 {
			err := fmt.Errorf("No project name specified.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		project, err := GetProject(db, *upmReq.ProjectName)
		if err != nil {
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
		}

		metadata, err := GetMetadata(db, *upmReq.ProjectName)
		if err != nil {
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
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
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
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
			return APIError{http.StatusServiceUnavailable, WRITE_DB_ERROR, NewServerError(err)}
		}

		if err = SuccessfulResponseJson(writer, request, APIMessage{"Updated project successfully."}); err != nil {
			return APIError{http.StatusInternalServerError, JSON_UNMARSHAL_ERROR, NewServerError(err)}
		}

		GlobalCache.InvalidateAll()
		return nil
	})
}

// Endpoint that uploads one or more images with attached tags for a particular solution, to S3.
func (service Service) PostAssetEndpoint() *Endpoint {

	type PostAssetRequest struct {
		Asset Asset `json:"asset"`
	}

	return NewEndpoint(func(writer http.ResponseWriter, request *http.Request) error {
		request.Body = http.MaxBytesReader(writer, request.Body, 31<<20) // Total of 31MB allowed for the whole body
		err := request.ParseMultipartForm(30 << 20)                      // Allow 30MB of uploads
		if err != nil {
			err := fmt.Errorf("Could not read form data.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		db, err := StartConn(service)
		if err != nil {
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
		}

		solutionIds, ok := request.MultipartForm.Value["solution_id"]
		if !ok || len(solutionIds) != 1 {
			err := fmt.Errorf("No solution ID specified.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}
		solutionId := solutionIds[0]

		var projectAssets ProjectAssetFields
		assetRow := db.QueryRow("select assets from project where project_name = (select project_name from solution where id = ?)", solutionId)
		err = assetRow.Scan(&projectAssets)
		if err != nil {
			err := fmt.Errorf("Invalid solution ID.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		var scopedId int
		scopedIdRow := db.QueryRow("select scoped_id from solution where id = ?", solutionId)
		err = scopedIdRow.Scan(&scopedId)
		if err != nil {
			err := fmt.Errorf("Invalid solution ID.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		files := make(map[string]Openable)

		// validate that there's only one file uploaded per tag
		for tag, handle := range request.MultipartForm.File {
			// fmt.Println(tag, handle)
			if len(handle) > 1 {
				err := fmt.Errorf("tag %s had more than one files uploaded to it.", tag)
				return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
			} else if len(handle) == 0 {
				err := fmt.Errorf("tag %s had no files uploaded to it.", tag)
				return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
			}
			files[tag] = (*OpenableMultipartFile)(handle[0])
		}

		tx, err := db.Begin()
		if err != nil {
			return APIError{http.StatusInternalServerError, "Could not start database transaction.", NewServerError(err)}
		}
		defer tx.Rollback()

		err = CreateAssets(tx, projectAssets, solutionId, files)
		if err != nil {
			LogError(err)
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		if err = tx.Commit(); err != nil {
			return APIError{http.StatusInternalServerError, "Could not commit database transaction.", NewServerError(err)}
		}

		SuccessfulResponse(writer, request, []byte("{'message': 'updated assets successfully.'"))

		GlobalCache.Invalidate(request.URL.Path) // POST requests to this endpoint invalidate the cache.
		return nil
	})
}

func (service Service) PutDocumentEndpoint() *Endpoint {

	type PutDocumentRequest struct {
		Text   *string `json:"text"`
		Title  *string `json:"title"`
		Parent *string `json:"parent"`
	}

	return NewEndpoint(func(writer http.ResponseWriter, request *http.Request) error {
		variables := mux.Vars(request)

		var docReq PutDocumentRequest
		dec := json.NewDecoder(request.Body)
		err := dec.Decode(&docReq)
		if err != nil {
			return APIError{http.StatusBadRequest, JSON_UNMARSHAL_ERROR, NewServerError(err)}
		}

		idOrSlug, ok := variables["idOrSlug"]
		if !ok {
			err := fmt.Errorf("Id or Slug was empty.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		if docReq.Text == nil {
			err := fmt.Errorf("No content was provided to update the document with.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		if docReq.Title == nil {
			err := fmt.Errorf("No title was provided for the document.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		if docReq.Parent == nil {
			emptyString := ""
			docReq.Parent = &emptyString
		}

		db, err := StartConn(service)
		if err != nil {
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
		}

		tx, err := db.Begin()
		if err != nil {
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
		}

		doc, err := GetDocument(tx, idOrSlug)
		if err != nil {
			err := fmt.Errorf("Could not find the document %s", idOrSlug)
			return APIError{http.StatusNotFound, err.Error(), NewServerError(err)}
		}

		err = doc.Update(tx, *docReq.Text, *docReq.Title, *docReq.Parent)
		if err != nil {
			return APIError{http.StatusServiceUnavailable, WRITE_DB_ERROR, NewServerError(err)}
		}

		err = tx.Commit()
		if err != nil {
			return APIError{http.StatusServiceUnavailable, WRITE_DB_ERROR, NewServerError(err)}
		}

		if err = SuccessfulResponseJson(writer, request, APIMessage{"Updated document successfully."}); err != nil {
			return APIError{http.StatusInternalServerError, JSON_UNMARSHAL_ERROR, NewServerError(err)}
		}

		return nil
	})
}

func (service Service) PostDocumentEndpoint() *Endpoint {

	type PostDocumentRequest struct {
		Slug   *string `json:"slug"`
		Text   *string `json:"text"`
		Title  *string `json:"title"`
		Parent *string `json:"parent"`
	}

	return NewEndpoint(func(writer http.ResponseWriter, request *http.Request) error {
		var docReq PostDocumentRequest
		dec := json.NewDecoder(request.Body)
		err := dec.Decode(&docReq)
		if err != nil {
			return APIError{http.StatusBadRequest, JSON_UNMARSHAL_ERROR, NewServerError(err)}
		}

		if docReq.Slug == nil {
			err := fmt.Errorf("Slug is emtpy.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		if docReq.Text == nil {
			err := fmt.Errorf("No content was provided to update the document with.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		if docReq.Title == nil {
			err := fmt.Errorf("No title was provided for the document.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		if docReq.Parent == nil {
			emptyString := ""
			docReq.Parent = &emptyString
		}

		db, err := StartConn(service)
		if err != nil {
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
		}

		tx, err := db.Begin()
		if err != nil {
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
		}

		doc := Document{}

		err = doc.Create(tx, *docReq.Slug, *docReq.Text, *docReq.Title, *docReq.Parent)
		if err != nil {
			return APIError{http.StatusServiceUnavailable, WRITE_DB_ERROR, NewServerError(err)}
		}

		err = tx.Commit()
		if err != nil {
			return APIError{http.StatusServiceUnavailable, WRITE_DB_ERROR, NewServerError(err)}
		}

		SuccessfulResponse(writer, request, []byte("{'message': 'document created successfully.'"))
		return nil
	})
}

func (service Service) DeleteDocumentEndpoint() *Endpoint {
	return NewEndpoint(func(writer http.ResponseWriter, request *http.Request) error {
		variables := mux.Vars(request)
		idOrSlug, ok := variables["idOrSlug"]
		if !ok {
			err := fmt.Errorf("Id or Slug was empty.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		if idOrSlug == "Front Matter" {
			err := fmt.Errorf("Cannot delete front matter.")
			return APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
		}

		db, err := service.GetDB()
		if err != nil {
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
		}

		tx, err := db.Begin()
		if err != nil {
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
		}

		doc, err := GetDocument(tx, idOrSlug)
		if err != nil {
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
		}

		err = doc.Delete(tx)
		if err != nil {
			return APIError{http.StatusServiceUnavailable, DELETE_DB_ERROR, NewServerError(err)}
		}

		err = tx.Commit()
		if err != nil {
			return APIError{http.StatusServiceUnavailable, WRITE_DB_ERROR, NewServerError(err)}
		}

		if err = SuccessfulResponseJson(writer, request, APIMessage{"Document delete successfully."}); err != nil {
			return APIError{http.StatusServiceUnavailable, JSON_MARSHAL_ERROR, NewServerError(err)}
		}
		return nil
	})
}
