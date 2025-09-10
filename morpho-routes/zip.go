package morphoroutes

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Uploads two versions of a project's solutions to the filesystem as CSVs; One human-readable and the other for the API.
// If run locally, this is saved to the filesystem. Otherwise, the CSVs are saved to S3.
func UploadCsv(service Service, projectName string) error {
	urlGenerator := func(filename string) string {
		switch service.ENVIRONMENT {
		case "prod":
			return path.Join(service.S3_IMAGES, filename)
		case "dev":
			return fmt.Sprintf("./%s", filename)
		default:
			return ""
		}
	}

	NonArchivalUrlGenerator := func(filename string) string {
		switch service.ENVIRONMENT {
		case "prod":
			return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", service.AWS_STORAGE_BUCKET_NAME, service.AWS_REGION, filename)
		case "dev":
			return fmt.Sprintf("http://localhost:%s/%s", service.PORT, filename)
		default:
			return ""
		}
	}

	archivalUrlGenerator := func(filename string) string {
		return filename
	}

	nonArchivalSolutions, err := GetSolutions(map[string]string{"project": projectName}, service, NonArchivalUrlGenerator)
	if err != nil {
		return err
	}

	archivalSolutions, err := GetSolutions(map[string]string{"project": projectName}, service, archivalUrlGenerator)
	if err != nil {
		return err
	}

	// Create CSV file streams for upload
	nonArchivalCsv := io.NopCloser(bytes.NewBuffer(SolutionSet(nonArchivalSolutions).CsvMarshal(false)))
	archivalCsv := io.NopCloser(bytes.NewBuffer(SolutionSet(archivalSolutions).CsvMarshal(true)))

	fmt.Println(urlGenerator(fmt.Sprintf("assets/%s/data.csv", projectName)))

	// file url should be something like assets/GCGA_10/data.csv
	onlineArchivalCsvHandle, err := os.OpenFile(
		urlGenerator(fmt.Sprintf("assets/%s/data.csv", projectName)),
		os.O_CREATE|os.O_WRONLY,
		0644)
	if err != nil {
		return NewServerError(err)
	}
	defer onlineArchivalCsvHandle.Close()

	onlineNonArchivalCsvHandle, err := os.OpenFile(
		urlGenerator(fmt.Sprintf("assets/%s/data_api.csv", projectName)),
		os.O_CREATE|os.O_WRONLY,
		0644)
	if err != nil {
		return NewServerError(err)
	}
	defer onlineNonArchivalCsvHandle.Close()

	if _, err = io.Copy(onlineNonArchivalCsvHandle, nonArchivalCsv); err != nil {
		return NewServerError(err)
	}

	if _, err := io.Copy(onlineArchivalCsvHandle, archivalCsv); err != nil {
		return NewServerError(err)
	}

	return nil
}

// Uploads an archive of a project to the filesystem as a zipfile.
// If run locally, this is saved to the filesystem. Otherwise, the archive is saved to S3.
func UploadArchive(service Service, projectName string) error {
	urlGenerator := func(filename string) string {
		switch service.ENVIRONMENT {
		case "prod":
			return fmt.Sprintf("%s/%s", service.S3_IMAGES, filename)
		case "dev":
			return fmt.Sprintf("./%s", filename)
		default:
			return ""
		}
	}

	// we create the zip archive in /tmp and then copy it over the s3 after archival is done
	zipFileHandle, err := os.OpenFile("/tmp/temp_archive_"+projectName+".zip", os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return NewServerError(err)
	}
	defer zipFileHandle.Close()

	zipFile := zip.NewWriter(zipFileHandle)

	solutions, err := GetSolutions(map[string]string{"project": projectName}, service, urlGenerator)
	if err != nil {
		return NewServerError(err)
	}

	for _, solution := range solutions {
		for _, asset := range solution.Assets {
			assetBytes, err := os.ReadFile(asset.File)
			if err != nil {
				return NewServerError(err)
			}

			_, filename := path.Split(asset.File)

			zipCounterpart, err := zipFile.Create(fmt.Sprintf("%s/%s", asset.Tag, filename))
			if err != nil {
				return NewServerError(err)
			}
			zipCounterpart.Write(assetBytes)
		}
	}

	csvUrl := urlGenerator(fmt.Sprintf("assets/%s/data.csv", projectName))

	csvBytes, err := os.ReadFile(csvUrl)
	if err != nil {
		return NewServerError(err)
	}

	zipCounterpart, err := zipFile.Create("data.csv")
	if err != nil {
		return NewServerError(err)
	}

	zipCounterpart.Write(csvBytes)
	zipFile.Flush()
	zipFile.Close()

	onlineFileHandle, err := os.OpenFile(
		urlGenerator(fmt.Sprintf("assets/%s/archive.zip", projectName)),
		os.O_CREATE|os.O_WRONLY,
		0644)
	if err != nil {
		return NewServerError(err)
	}
	defer onlineFileHandle.Close()

	zipFileHandle.Seek(0, 0)
	_, err = io.Copy(onlineFileHandle, zipFileHandle)
	if err != nil {
		return NewServerError(err)
	}

	err = os.Remove("/tmp/temp_archive_" + projectName + ".zip")
	if err != nil {
		return NewServerError(err)
	}

	return nil
}

type FileTuple struct {
	name string
	file *zip.File
}

func IsDir(filename string) bool {
	return filename[len(filename)-1] == os.PathSeparator
}

func MakeTree(file *os.File) (map[string]FileTuple, error) {
	files := make(map[string]FileTuple)

	finfo, err := file.Stat()
	if err != nil {
		return nil, NewServerError(err)
	}

	archive, err := zip.NewReader(file, finfo.Size())
	if err != nil {
		return nil, NewServerError(err)
	}

	for _, f := range archive.File {
		files[f.Name] = FileTuple{
			name: f.Name,
			file: f,
		}
	}

	return files, nil
}

func unpackItem(file *zip.File, dirname string) (filename string, err error) {
	filename = "./" + path.Join(dirname, "solutions.db")
	fileHandle, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0777)
	defer func() {
		err = fileHandle.Close()
	}()

	if err != nil {
		return "", err
	}

	rc, err := file.Open()
	if err != nil {
		return "", err
	}

	contents, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}

	n, err := fileHandle.Write(contents)
	if n < len(contents) || err != nil {
		fmt.Println("could not write all the contents.")
		panic(err)
	}

	return filename, nil
}

type TempAsset struct {
	Tag        string
	File       string
	SolutionId string
}

// Local function for interop with uploaded database
func getMetadata(db *sql.DB, projectName string) (m Metadata, err error) {
	row := db.QueryRow("SELECT captions, human_name, slug, text FROM metadata WHERE project_name=?", projectName)
	err = row.Scan(&m.Captions, &m.HumanName, &m.Description.Slug, &m.Description.Text)
	return
}

func UploadProject(service Service, s3Uri string) (projectName string, err error) {

	file, err := os.Open(path.Join(service.S3_TEMP, s3Uri))
	if err != nil {
		return "", APIError{http.StatusBadRequest, "Could not find uploaded zip in S3 bucket.", NewServerError(err)}
	}

	tree, err := MakeTree(file)
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not open zip file for reading.", NewServerError(err)}
	}

	//
	// create local directory for importing the db file
	//

	randDir := "./temp" + randString(8)

	err = os.Mkdir(randDir, 0777)
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not create temporary working directory.", NewServerError(err)}
	}

	defer func() {
		removeErr := os.RemoveAll(randDir)
		if removeErr != nil {
			removeErr = APIError{http.StatusInternalServerError, "Could not clear temporary working directory.", NewServerError(err)}
		}

		if err == nil {
			err = removeErr
		}
	}()

	//
	// extract imported db into local directory
	//

	var solutionDbPath string = ""
	var prefix string = ""
	for key := range tree {
		if strings.Contains(key, ".db") {
			solutionDbPath = key
			prefix = path.Dir(key)
		}
	}

	if solutionDbPath == "" {
		err = fmt.Errorf("Could not find solutions.db within zip. Please include it when compressing the targeted folder.")
		return "", APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
	}

	filename, err := unpackItem(tree[solutionDbPath].file, randDir)
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not extract solutions.db from zip file.", NewServerError(err)}
	}

	//
	// get handles to temporary and permanent db and start transactions
	//

	tempdb, err := sql.Open(GetDriver(), fmt.Sprintf("file:%s", filename))
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not open solutions.db.", NewServerError(err)}
	}

	realdb, err := StartConn(service)
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not open internal database.", NewServerError(err)}
	}

	temptx, err := tempdb.Begin()
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not start a transaction within solutions.db.", NewServerError(err)}
	}
	defer temptx.Rollback()

	realtx, err := realdb.Begin()
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not start a transaction within internal database.", NewServerError(err)}
	}
	defer realtx.Rollback()

	//
	// insert project and associated solutions
	//

	projects, err := GetAllProjects(tempdb)
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Querying solutions.db failed.", NewServerError(err)}
	}

	if err = projects[0].Create(realtx); err != nil {
		return "", APIError{http.StatusServiceUnavailable, "Could not insert project into internal database.", NewServerError(err)}
	}

	solutions, err := GetAllSolutions(temptx, projects[0].ProjectName, nil)
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Querying solutions.db failed.", NewServerError(err)}
	}

	solutionSet := SolutionSet(solutions)
	err = solutionSet.Create(realtx, projects[0].ProjectName)
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not insert solutions into internal database.", NewServerError(err)}
	}

	//
	// insert assets
	//

	for _, solution := range solutionSet {
		fileMap := make(map[string]Openable)
		for _, asset := range solution.Assets {
			fileMap[asset.Tag] = (*OpenableZipFile)(tree[path.Join(prefix, strings.ReplaceAll(asset.File, "\\", string(filepath.Separator)))].file)
		}

		// fmt.Println(projects[0].Assets, fileMap)

		err = CreateAssets(realtx, projects[0].Assets, solution, projects[0].ProjectName, fileMap)
		if err != nil {
			return "", APIError{http.StatusInternalServerError, "Could not upload assets to internal database and storage bucket.", NewServerError(err)}
		}
	}

	//
	// insert metadata
	//

	metadata, err := getMetadata(tempdb, projects[0].ProjectName)
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not extract metadata from solutions.db.", NewServerError(err)}
	}

	if err = metadata.Create(realtx, projects[0].ProjectName); err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not insert imported metadata into internal database.", NewServerError(err)}
	}

	//
	// wrap up
	//

	if err = realtx.Commit(); err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not commit transaction on internal database.", NewServerError(err)}
	}

	if err = UploadCsv(service, projects[0].ProjectName); err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not upload CSV data to internal database.", NewServerError(err)}
	}

	return projects[0].ProjectName, nil
}
