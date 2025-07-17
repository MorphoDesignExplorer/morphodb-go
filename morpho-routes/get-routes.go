package morphoroutes

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	_ "github.com/glebarez/go-sqlite" // pure go driver for windows platforms
	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3" // go driver for linux platforms
)

// Fetches all the projects or a singular project.
//
// Variables is a map that could contain the key project, which denotes the singular project to select. Do not provide the key if you need to fetch all the projects.
//
// service is the set of environment variables.
//
// Returns the set of projects fetched, or an error.
func GetProjects(variables map[string]string, service Service) ([]Project, error) {
	broadQuery := "select creation_date, project.project_name, variable_metadata, output_metadata, assets, deleted, metadata.captions, metadata.slug, metadata.markdown, metadata.human_name FROM project, metadata where project.project_name = metadata.project_name;"
	constrictedQuery := "select creation_date, project.project_name, variable_metadata, output_metadata, assets, deleted, metadata.captions, metadata.slug, metadata.markdown, metadata.human_name FROM project, metadata WHERE project.project_name = metadata.project_name AND project.project_name = ?;"

	projectName, singularRequest := variables["project"]

	db, err := service.GetDB()
	if err != nil {
		return nil, NewServerError(err)
	}
	defer db.Close()

	var result *sql.Rows
	if singularRequest {
		result, err = db.Query(constrictedQuery, projectName)
	} else {
		result, err = db.Query(broadQuery)
	}

	if err != nil {
		return nil, NewServerError(err)
	}
	defer result.Close()

	projects := make([]Project, 0)
	for result.Next() {
		var temp Project
		var tempMetadata Metadata

		err = result.Scan(&temp.CreationDate, &temp.ProjectName, &temp.VariableMetadata, &temp.OutputMetadata, &temp.Assets, &temp.Deleted, &tempMetadata.Captions, &tempMetadata.Description.Slug, &tempMetadata.Description.Text, &tempMetadata.HumanName)
		if err != nil {
			return nil, NewServerError(err)
		}

		temp.ProjectMetadata = tempMetadata
		projects = append(projects, temp)
	}

	return projects, nil
}

// Fetches all solutions or a single solution associated with a project.
//
// variables is a map that contains the project and solution key, where project should be filled and solution can be ommitted.
// the solution key can be omitted to fetch all solutions associated with a project.
//
// service is the set of environment variables.
//
// urlGenerator generates the file path prefix for the assets fetched.
//
// Returns the set of solutions, or an error.
func GetSolutions(variables map[string]string, service Service, urlGenerator func(string) string) ([]Solution, error) {
	broadQuery := "SELECT solution.id, solution.scoped_id, parameters, output_parameters, tag, file FROM solution, asset WHERE asset.solution_id = solution.id AND solution.project_name = ?"
	constrictedQuery := "SELECT solution.id, solution.scoped_id, parameters, output_parameters, tag, file FROM solution, asset WHERE asset.solution_id = solution.id AND solution.project_name = ? AND solution.id = ?"

	projectName := variables["project"]
	solutionId, singularRequest := variables["solution"]

	db, err := service.GetDB()
	if err != nil {
		return nil, NewServerError(err)
	}
	defer db.Close()

	var result *sql.Rows
	if singularRequest {
		result, err = db.Query(constrictedQuery, projectName, solutionId)
	} else {
		result, err = db.Query(broadQuery, projectName)
	}
	if err != nil {
		return nil, NewServerError(err)
	}
	defer result.Close()

	solutions := make(map[string]Solution)
	for result.Next() {
		var tempSolution Solution
		var fileTag, fileUri string

		err = result.Scan(&tempSolution.Id, &tempSolution.ScopedId, &tempSolution.Parameter, &tempSolution.OutputParameter, &fileTag, &fileUri)
		if err != nil {
			return nil, NewServerError(err)
		}

		fileUri = urlGenerator(fileUri)

		if solution, ok := solutions[tempSolution.Id]; ok {
			solution.Assets = append(solution.Assets, Asset{Tag: fileTag, File: fileUri})
			solutions[tempSolution.Id] = solution
		} else {
			tempSolution.Assets = make([]Asset, 0)
			tempSolution.Assets = append(tempSolution.Assets, Asset{Tag: fileTag, File: fileUri})
			solutions[tempSolution.Id] = tempSolution
		}
	}

	solutionSet := make([]Solution, 0, len(solutions))
	for _, solution := range solutions {
		solutionSet = append(solutionSet, solution)
	}

	return solutionSet, nil
}

// GET method that returns either a singular project or all the projects.
//
// Returns an Endpoint object.
func (service Service) GetProjectEndpoint() *Endpoint {
	return NewEndpoint(func(writer http.ResponseWriter, request *http.Request) error {
		variables := mux.Vars(request) // map that may or may not have the key 'project'

		projectSet, err := GetProjects(variables, service)
		if err != nil {
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
		}

		bytes, err := json.Marshal(projectSet)
		if err != nil {
			return APIError{http.StatusInternalServerError, JSON_MARSHAL_ERROR, NewServerError(err)}
		}

		GlobalCache.Cache(request.URL.Path, bytes)
		SuccessfulResponse(writer, request, bytes)
		return nil
	})
}

// GET method that returns either a singular solution or all the solutions associated with a project.
//
// Returns an Endpoint object.
func (service Service) GetSolutionEndpoint() *Endpoint {
	urlGenerator := func(filename string) string {
		switch service.ENVIRONMENT {
		case "prod":
			return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", service.AWS_STORAGE_BUCKET_NAME, service.AWS_REGION, filename)
		case "dev":
			return fmt.Sprintf("http://localhost:%s/%s", service.PORT, filename)
		default:
			return ""
		}
	}

	return NewEndpoint(func(writer http.ResponseWriter, request *http.Request) error {
		variables := mux.Vars(request) // map that has the key 'project' and may or may not have the key 'solution'
		solutionSet, err := GetSolutions(variables, service, urlGenerator)
		if err != nil {
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
		}

		bytes, err := json.Marshal(solutionSet)
		if err != nil {
			return APIError{http.StatusInternalServerError, JSON_MARSHAL_ERROR, NewServerError(err)}
		}

		GlobalCache.Cache(request.URL.Path, bytes)
		SuccessfulResponse(writer, request, bytes)
		return nil
	})
}

// GET Method that returns a document specified by an ID or slug.
//
// Returns an endpoint object.
func (service Service) GetDocumentEndpoint() *Endpoint {
	method := func(writer http.ResponseWriter, request *http.Request) error {
		variables := mux.Vars(request)
		db, err := service.GetDB()
		if err != nil {
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
		}

		tx, err := db.Begin()
		if err != nil {
			return APIError{http.StatusServiceUnavailable, OPEN_DB_ERROR, NewServerError(err)}
		}

		var docBytes []byte

		if docIdOrSlug, ok := variables["idOrSlug"]; ok {
			doc, err := GetDocument(tx, docIdOrSlug)
			if err != nil {
				return APIError{http.StatusNotFound, "Could not find the document specified.", NewServerError(err)}
			}

			docBytes, err = json.Marshal(doc)
			if err != nil {
				return APIError{http.StatusInternalServerError, JSON_MARSHAL_ERROR, NewServerError(err)}
			}
		} else {
			docs, err := GetAllDocuments(tx)
			if err != nil {
				return APIError{http.StatusInternalServerError, OPEN_DB_ERROR, NewServerError(err)}
			}

			docBytes, err = json.Marshal(docs)
			if err != nil {
				return APIError{http.StatusInternalServerError, JSON_MARSHAL_ERROR, NewServerError(err)}
			}
		}

		SuccessfulResponse(writer, request, docBytes)
		return nil
	}

	return NewEndpoint(method)
}
