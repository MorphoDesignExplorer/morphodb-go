
package morphoroutes

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"fmt"
	"io"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gabriel-vasile/mimetype"
	"os"
	"path"
	"strings"
)

type FileTuple struct {
	name     string
	file     *zip.File		
}

func IsDir(filename string) bool {
	return filename[len(filename) - 1] == os.PathSeparator
}

func MakeTree(zippath string) (map[string]FileTuple, error) {
	files := make(map[string]FileTuple)

	arc, err := zip.OpenReader(zippath)
	if err != nil {
		return nil, err
	}

	for _, f := range arc.File {
		files[f.Name] = FileTuple{
			name: f.Name,
			file: f,
		}
	}

	return files, nil
}

func unpackItem(file *zip.File, dirname string) (filename string, err error) {
	filename = "./" + path.Join(dirname, "solutions.db")
	fileHandle, err := os.OpenFile(filename, os.O_CREATE | os.O_RDWR, 0777)
	defer fileHandle.Close()
	if err != nil {
		return "", err
	}

	rc, err := file.Open()
	if err != nil { return "", err}

	contents, err := io.ReadAll(rc)
	if err != nil { return "", err}

	n, err := fileHandle.Write(contents)
	if n < len(contents) || err != nil {
		fmt.Println("could not write all the contents.")
		panic(err)
	}

	return filename, nil
}

type TempAsset struct {
	Tag			string
	File		string
	SolutionId	string
}

func getAssets(tx *sql.Tx) ([]TempAsset, error) {
	assets := make([]TempAsset, 0)

	rows, err := tx.Query("SELECT file, tag, solution_id FROM asset")
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		tempAsset := TempAsset{}
		err = rows.Scan(&tempAsset.File, &tempAsset.Tag, &tempAsset.SolutionId)
		if err != nil {
			return nil, err
		}
		assets = append(assets, tempAsset)
	}

	return assets, nil
}

func unpackAndUploadToLocal(file *zip.File, name string) (string, error) {
	rc, err := file.Open()
	if err != nil {
		return "", err
	}

	contents, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}

	mime := mimetype.Detect(contents)

	if writeHandle, err := os.OpenFile(fmt.Sprintf("assets/%s%s", name, mime.Extension()), os.O_CREATE|os.O_RDWR, 0644); err == nil {
		n, err := writeHandle.Write(contents)
		if n != len(contents) || err != nil {
			return "", fmt.Errorf("could not write complete file: " + err.Error())
		}
		return mime.Extension(), nil
	} else {
		return "", err
	}
}

func unpackAndUploadToS3(file *zip.File, name string) (string, error) {
	rc, err := file.Open()
	if err != nil {
		return "", err
	}

	contents, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}

	mime := mimetype.Detect(contents)

	client, err := CreateS3Client()
	if err != nil {
		return "", nil
	}

	_, err = client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:			aws.String("morpho-images"),
		Key:			aws.String(fmt.Sprintf("assets/%s%s", name, mime.Extension())),
		Body:			bytes.NewReader(contents),
		ContentType:	aws.String(mime.String()),
	})

	return mime.Extension(), err
}

// Local function for interop with uploaded database
func getMetadata(db *sql.DB, projectName string) (m Metadata, err error) {
	row := db.QueryRow("SELECT captions, human_name, slug, text FROM metadata WHERE project_name=?", projectName)
	err = row.Scan(&m.Captions, &m.HumanName, &m.Description.Slug, &m.Description.Text)
	return
}

func ClearWD() error {
	err := os.RemoveAll("./temp")
	if err != nil {
		return err
	}
	return nil
}

func UploadProject(config Config) (err error) {
	tree, err := MakeTree("./test.zip")
	if err != nil {
		LogError(err)
		return err
	}

	// create local directory for importing the db file

	err = ClearWD()
	if err != nil {
		LogError(err)
		return err
	}

	err = os.Mkdir("./temp", 0777)
	if err != nil {
		LogError(err)
		return err
	}

	// extract imported db into local directory

	filename, err := unpackItem(tree["solutions.db"].file, "./temp")
	if err != nil {
		LogError(err)
		return err
	}

	// get handles to temporary and permanent db

	tempdb, err := sql.Open(GetDriver(), fmt.Sprintf("file:%s", filename))
	if err != nil {
		LogError(err)
		return err
	}

	realdb, err := StartConn(config)
	if err != nil {
		LogError(err)
		return err
	}

	// start transactions
	
	temptx, err := tempdb.Begin()
	if err != nil {
		LogError(err)
		return err
	}
	defer temptx.Rollback()

	realtx, err := realdb.Begin()
	if err != nil {
		LogError(err)
		return err
	}
	defer realtx.Rollback()

	// insert project and associated solutions

	projects, err := GetAllProjects(tempdb)
	if err != nil {
		LogError(err)
		return err
	}

	projects[0].Create(realtx)


	solutions, err := GetAllSolutions(temptx, projects[0].ProjectName)
	if err != nil {
		LogError(err)
		return nil
	}

	solutionSet := SolutionSet(solutions)
	err = solutionSet.Create(realtx, projects[0].ProjectName)
	if err != nil {
		LogError(err)
		return err
	}

	assets, err := getAssets(temptx)
	if err != nil {
		LogError(err)
		return err
	}

	// insert assets

	for _, asset := range assets {
		randomName := fmt.Sprintf("%s_%s", asset.SolutionId, randString(7))
		s3Url, err := unpackAndUploadToLocal(tree[strings.ReplaceAll(asset.File, "\\", string(filepath.Separator))].file, randomName)
		if err != nil {
			LogError(err)
			return err
		}

		a := Asset{Tag: asset.Tag, File: s3Url}
		a.Create(realtx, asset.SolutionId)
	}

	// insert metadata

	metadata, err := getMetadata(tempdb, projects[0].ProjectName)
	if err != nil {
		LogError(err)
		return err
	}

	metadata.Create(realtx, projects[0].ProjectName)

	// wrap up

	err = realtx.Commit()
	if err != nil {
		LogError(err)
		return err
	}

	fmt.Println("done")

	err = ClearWD()
	if err != nil {
		LogError(err)
		return
	}

	return nil
}

