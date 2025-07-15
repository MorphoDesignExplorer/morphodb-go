package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	morphoroutes "github.com/MorphoDesignExplorer/morphodb-go/morpho-routes"
	"github.com/gorilla/mux"
)

func init() {
	morphoroutes.GlobalCache = &morphoroutes.Cacher{}
	morphoroutes.GlobalCache.InitCache()
}

func mockConfig() morphoroutes.Config {
	return morphoroutes.Config{
		DB_STRING:               "./tests/morpho.sqlite",
		AWS_REGION:              "us-east-1",
		AWS_STORAGE_BUCKET_NAME: "morpho-images",
	}
}

func getFileBytes(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	bytes, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	return bytes, nil
}

func TestProjectEndpoint(t *testing.T) {
	t.Run("TestProjectEndpoint", func(t *testing.T) {
		config := mockConfig()

		getProjects := config.GetProjectEndpoint().Finalize()

		request, _ := http.NewRequest(http.MethodGet, "/project/", nil)
		response := httptest.NewRecorder()

		getProjects(response, request)

		endpointProjects := make([]morphoroutes.Project, 0)
		bytes := response.Body.Bytes()
		err := json.Unmarshal(bytes, &endpointProjects)
		if err != nil {
			t.Error(err)
		}

		fileBytes, err := getFileBytes("./tests/project.json")
		if err != nil {
			t.Errorf("error while opening file: %s", err)
		}

		referenceProjects := make([]morphoroutes.Project, 0)
		err = json.Unmarshal(fileBytes, &referenceProjects)
		if err != nil {
			t.Error(err)
		}

		existence := make(map[string]morphoroutes.Project)
		missingCount := 0

		for _, val := range referenceProjects {
			existence[val.ProjectName] = val
		}
		for _, val := range endpointProjects {
			// fmt.Println(val, existence[val.ProjectName])
			// fmt.Println("********************************")
			if !reflect.DeepEqual(existence[val.ProjectName], val) {
				missingCount += 1
			}
		}

		if missingCount > 0 || len(referenceProjects) != len(endpointProjects) {
			t.Errorf("endpoint has %d projects, reference has %d projects, %d missing", len(endpointProjects), len(referenceProjects), missingCount)
		}
	})
}

func TestSolutionEndpoint(t *testing.T) {
	t.Run("TestProjectModelEndpoint", func(t *testing.T) {
		config := mockConfig()
		endpoints := []string{"GCGA_10", "GCGA_39"}

		deserializeArray := func(b []byte) (slice []any) {
			slice = make([]any, 0)
			json.Unmarshal(b, &slice)
			return
		}

		for _, endpoint := range endpoints {
			request, _ := http.NewRequest(http.MethodGet, "project/{}/model/", nil)
			response := httptest.NewRecorder()
			variables := map[string]string{
				"project": endpoint,
			}

			request = mux.SetURLVars(request, variables)

			getSolutions := config.GetSolutionEndpoint().Finalize()
			getSolutions(response, request)

			fileBytes, err := getFileBytes(fmt.Sprintf("./tests/model_%s.json", endpoint))
			if err != nil {
				t.Errorf("error while opening file: %s", err)
			}

			endpointArray := deserializeArray(response.Body.Bytes())
			referenceArray := deserializeArray(fileBytes)

			// check if the records in one array matches the other
			existence := make(map[string]bool)
			missingCount := 0
			for _, val := range referenceArray {
				existence[fmt.Sprintf("%q", val)] = true
			}

			for _, val := range endpointArray {
				if !existence[fmt.Sprintf("%q", val)] {
					missingCount += 1
				}
			}

			if missingCount > 0 || len(referenceArray) != len(endpointArray) {
				t.Errorf("reference has %d records, endpoint got %d records. %d records did not match", len(referenceArray), len(endpointArray), missingCount)
			}
		}
	})
}
